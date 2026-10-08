package dev.cymonkey.androidhelper

import android.graphics.Bitmap
import android.graphics.PixelFormat
import android.media.AudioRecord
import android.media.ImageReader
import android.media.projection.MediaProjection
import android.os.Handler
import android.os.HandlerThread
import android.os.Looper
import android.os.SystemClock
import java.io.ByteArrayOutputStream
import java.io.Closeable
import java.util.concurrent.atomic.AtomicBoolean

/** Start on the main thread after fresh consent and a visible foreground service.
 * The transport must be connected. The host calls close on WebSocket shutdown. */
class DeviceTargetCapture(
    projection: MediaProjection,
    private val transport: DeviceTargetTransport,
    width: Int, height: Int, densityDpi: Int,
    video: Boolean, systemAudio: Boolean, microphone: Boolean,
    private val onStop: () -> Unit
) : Closeable {
    private val validation = require(Looper.myLooper() == Looper.getMainLooper() && width in 1..4096 && height in 1..4096)
    private val closed = AtomicBoolean(false)
    private val worker = HandlerThread("Cymonkey frames").apply { start() }
    private val handler = Handler(worker.looper)
    private val native = DeviceProjection(projection) { close() }
    private val reader: ImageReader
    private var lastFrame = 0L
    init {
        check(Looper.myLooper() == Looper.getMainLooper())
        require(width in 1..4096 && height in 1..4096)
        val scale = minOf(1.0, 1280.0 / maxOf(width, height))
        val captureWidth = maxOf(1, (width * scale).toInt())
        val captureHeight = maxOf(1, (height * scale).toInt())
        reader = ImageReader.newInstance(captureWidth, captureHeight, PixelFormat.RGBA_8888, 2)
        reader.setOnImageAvailableListener({ source ->
            runCatching { source.acquireLatestImage() }.getOrNull()?.use { image ->
                if (closed.get() || !video || SystemClock.elapsedRealtime() - lastFrame < 100) return@use
                lastFrame = SystemClock.elapsedRealtime()
                try {
                    val plane = image.planes[0]
                    check(plane.pixelStride == 4)
                    val pixels = java.nio.ByteBuffer.allocate(image.width * image.height * 4)
                    val row = ByteArray(image.width * 4)
                    for (y in 0 until image.height) { plane.buffer.position(y * plane.rowStride); plane.buffer.get(row); pixels.put(row) }
                    pixels.flip()
                    val bitmap = Bitmap.createBitmap(image.width, image.height, Bitmap.Config.ARGB_8888)
                    try { bitmap.copyPixelsFromBuffer(pixels); val output = ByteArrayOutputStream(); check(bitmap.compress(Bitmap.CompressFormat.JPEG, 75, output)); transport.publish(1, output.toByteArray()) }
                    finally { bitmap.recycle() }
                } catch (_: Exception) { close() }
            }
        }, handler)
        try {
            native.start(reader.surface, captureWidth, captureHeight, densityDpi)
            if (systemAudio) startAudio(native.openAudio(false), 2)
            if (microphone) startAudio(native.openAudio(true), 3)
        } catch (error: Exception) { close(); throw error }
    }
    private fun startAudio(recorder: AudioRecord, kind: Int) {
        Thread({
            val mono = ByteArray(1920)
            try {
                while (!closed.get()) {
                    val n = recorder.read(mono, 0, mono.size, AudioRecord.READ_BLOCKING)
                    check(n > 0 && n % 2 == 0)
                    val stereo = ByteArray(n * 2)
                    for (i in 0 until n step 2) { stereo[i*2] = mono[i]; stereo[i*2+1] = mono[i+1]; stereo[i*2+2] = mono[i]; stereo[i*2+3] = mono[i+1] }
                    transport.publish(kind, stereo)
                }
            } catch (_: Exception) { close() }
        }, "Cymonkey audio $kind").apply { isDaemon = true; start() }
    }
    override fun close() {
        if (!closed.compareAndSet(false, true)) return
        Handler(Looper.getMainLooper()).post {
            native.close(); reader.close(); worker.quitSafely(); transport.close(); onStop()
        }
    }
}
