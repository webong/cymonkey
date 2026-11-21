package dev.cymonkey.androidhelper

import android.media.AudioRecord
import android.media.projection.MediaProjection
import android.view.Surface
import io.github.webong.ext.system.android.SystemMediaProjection
import java.io.Closeable

/** Cymonkey's host-facing projection handle. The consented native projection
 * and its lifetime belong to ext; Cymonkey retains capture transport/policy. */
class DeviceProjection(projection: MediaProjection, onStopped: () -> Unit) : Closeable {
    private val native = SystemMediaProjection(projection, onStopped)

    fun start(surface: Surface, width: Int, height: Int, densityDpi: Int) {
        native.start(surface, width, height, densityDpi, SystemMediaProjection.MAX_DURATION_MILLIS)
    }

    fun resize(surface: Surface, width: Int, height: Int, densityDpi: Int) {
        native.resize(surface, width, height, densityDpi)
    }

    fun openAudio(microphone: Boolean): AudioRecord = native.openAudio(microphone)

    override fun close() = native.close()
}
