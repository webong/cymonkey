package dev.cymonkey.androidhelper

import io.github.webong.ext.system.android.SystemAudioPlayback
import java.io.Closeable

/** Host-facing playback handle; ext owns the bounded native audio track. */
class DevicePlayback : Closeable {
    private val native = SystemAudioPlayback()

    fun write(pcm: ByteArray, offset: Int = 0, count: Int = pcm.size - offset): Int =
        native.write(pcm, offset, count)

    override fun close() = native.close()
}
