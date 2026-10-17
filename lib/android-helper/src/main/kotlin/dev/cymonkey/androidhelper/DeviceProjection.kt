package dev.cymonkey.androidhelper

import android.hardware.display.DisplayManager
import android.hardware.display.VirtualDisplay
import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioPlaybackCaptureConfiguration
import android.media.AudioRecord
import android.media.MediaRecorder
import android.media.projection.MediaProjection
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.view.Surface
import java.io.Closeable

/** Owns a fresh, consented projection. The host must first start its visible
 * mediaProjection foreground service. Surface belongs to the host's encoder;
 * it is never released here. All lifecycle calls run on the main thread. */
class DeviceProjection(
    private val projection: MediaProjection,
    private val onStopped: () -> Unit
) : Closeable {
    private var display: VirtualDisplay? = null
    private var closed = false
    private var started = false
    private val audio = mutableListOf<AudioRecord>()
    private val callback = object : MediaProjection.Callback() {
        override fun onStop() { close() }
    }
    init {
        require(Looper.myLooper() == Looper.getMainLooper())
        projection.registerCallback(callback, Handler(Looper.getMainLooper()))
    }
    fun start(surface: Surface, width: Int, height: Int, densityDpi: Int) {
        checkMain(); check(!closed && !started)
        require(width in 1..8192 && height in 1..8192 && densityDpi > 0)
        started = true // MediaProjection tokens are single-use, including failed attempts.
        try {
            display = projection.createVirtualDisplay("Cymonkey device", width, height,
                densityDpi, DisplayManager.VIRTUAL_DISPLAY_FLAG_AUTO_MIRROR, surface, null, null)
        } catch (error: Exception) { close(); throw error }
    }
    fun resize(surface: Surface, width: Int, height: Int, densityDpi: Int) {
        checkMain(); check(!closed && display != null)
        require(width in 1..8192 && height in 1..8192 && densityDpi > 0)
        display!!.resize(width, height, densityDpi); display!!.surface = surface
    }
    /** PCM16 mono 48 kHz. Host has already obtained RECORD_AUDIO permission;
     * microphone also requires its foreground service type on current Android.
     * Read on a worker thread; close() stops/unblocks all owned recorders. */
    fun openAudio(microphone: Boolean): AudioRecord {
        checkMain(); check(!closed && started)
        require(audio.size < 2) { "At most two audio sources per projection" }
        val format = AudioFormat.Builder().setEncoding(AudioFormat.ENCODING_PCM_16BIT)
            .setSampleRate(48000).setChannelMask(AudioFormat.CHANNEL_IN_MONO).build()
        val minimum = AudioRecord.getMinBufferSize(48000, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
        check(minimum > 0) { "Audio format unavailable" }
        val builder = AudioRecord.Builder().setAudioFormat(format).setBufferSizeInBytes(maxOf(minimum, 9600))
        if (microphone) builder.setAudioSource(MediaRecorder.AudioSource.MIC)
        else {
            check(Build.VERSION.SDK_INT >= 29) { "Playback capture requires Android 10" }
            builder.setAudioPlaybackCaptureConfig(AudioPlaybackCaptureConfiguration.Builder(projection)
                .addMatchingUsage(AudioAttributes.USAGE_MEDIA)
                .addMatchingUsage(AudioAttributes.USAGE_GAME).build())
        }
        val recorder = builder.build()
        try {
            check(recorder.state == AudioRecord.STATE_INITIALIZED)
            recorder.startRecording(); audio.add(recorder); return recorder
        } catch (error: Exception) { recorder.release(); throw error }
    }
    override fun close() {
        checkMain()
        if (closed) return
        closed = true
        for (recorder in audio) { runCatching { recorder.stop() }; recorder.release() }
        audio.clear(); display?.release(); display = null
        projection.unregisterCallback(callback); projection.stop(); onStopped()
    }
    private fun checkMain() { check(Looper.myLooper() == Looper.getMainLooper()) }
}
