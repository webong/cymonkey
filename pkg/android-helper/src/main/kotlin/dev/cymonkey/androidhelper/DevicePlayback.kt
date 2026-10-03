package dev.cymonkey.androidhelper

import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioTrack
import java.io.Closeable

/** PCM16 stereo 48 kHz. Host grants audio.playback before construction and
 * serializes write/close calls. Nonblocking writes expose partial acceptance. */
class DevicePlayback : Closeable {
    private val track: AudioTrack
    private var closed = false
    init {
        val minimum = AudioTrack.getMinBufferSize(48000, AudioFormat.CHANNEL_OUT_STEREO, AudioFormat.ENCODING_PCM_16BIT)
        check(minimum > 0 && minimum <= 192000) { "Playback format or bounded buffer unavailable" }
        track = AudioTrack.Builder()
            .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA)
                .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
            .setAudioFormat(AudioFormat.Builder().setSampleRate(48000).setChannelMask(AudioFormat.CHANNEL_OUT_STEREO)
                .setEncoding(AudioFormat.ENCODING_PCM_16BIT).build())
            .setBufferSizeInBytes(maxOf(minimum, 19200)).setTransferMode(AudioTrack.MODE_STREAM).build()
        try { check(track.state == AudioTrack.STATE_INITIALIZED); track.play() }
        catch (error: Exception) { track.release(); throw error }
    }
    fun write(pcm: ByteArray, offset: Int = 0, count: Int = pcm.size - offset): Int {
        check(!closed)
        require(offset >= 0 && count >= 0 && offset <= pcm.size - count && count % 4 == 0 && count <= 3840)
        val written = track.write(pcm, offset, count, AudioTrack.WRITE_NON_BLOCKING)
        check(written >= 0) { "Native playback failed: $written" }
        return written
    }
    override fun close() {
        if (closed) return
        closed = true
        try { track.pause(); track.flush(); track.stop() } finally { track.release() }
    }
}
