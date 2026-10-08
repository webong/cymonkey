package dev.cymonkey.androidhelper

import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import okio.ByteString.Companion.toByteString
import java.io.Closeable
import java.net.URI
import java.util.concurrent.atomic.AtomicBoolean

/** A host-issued target credential, never the operator credential. TLS is
 * required except loopback. Caller owns the HTTP client and its lifecycle. */
class DeviceTargetTransport(
    client: OkHttpClient, url: String, token: String,
    private val video: Boolean, private val audio: Boolean, private val microphone: Boolean,
    private val playback: ((ByteArray) -> Unit)? = null,
    private val onReady: () -> Unit,
    private val onStop: (Throwable?) -> Unit
) : Closeable {
    private val closed = AtomicBoolean(false)
    private val socket: WebSocket
    init {
        val uri = URI(url)
        require(uri.scheme == "wss" || (uri.scheme == "ws" && uri.host in listOf("localhost", "127.0.0.1", "[::1]", "::1")))
        require(uri.userInfo == null && uri.query == null && uri.fragment == null && token.length == 64)
        socket = client.newWebSocket(Request.Builder().url(url).header("Authorization", "Bearer $token").build(), object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) { if (!closed.get()) try { onReady() } catch (error: Exception) { stop(error, webSocket) } }
            override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                if (closed.get()) return
                try {
                    require(bytes.size in 2..3841 && bytes[0].toInt() == 4 && (bytes.size - 1) % 4 == 0)
                    check(playback != null) { "Playback was not granted" }
                    playback.invoke(bytes.substring(1).toByteArray())
                } catch (error: Exception) { stop(error, webSocket) }
            }
            override fun onMessage(webSocket: WebSocket, text: String) { stop(IllegalArgumentException("Unexpected target message"), webSocket) }
            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) { stop(null, webSocket) }
            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) { stop(t, webSocket) }
        })
    }
    private fun stop(error: Throwable?, connection: WebSocket) { if (closed.compareAndSet(false, true)) { connection.cancel(); onStop(error) } }
    fun publish(kind: Int, bytes: ByteArray) {
        check(!closed.get())
        require((kind == 1 && video) || (kind == 2 && audio) || (kind == 3 && microphone))
        require(bytes.isNotEmpty() && (if (kind == 1) bytes.size <= 4 * 1024 * 1024 else bytes.size <= 3840 && bytes.size % 4 == 0))
        if (socket.queueSize() > 256 * 1024 || !socket.send((byteArrayOf(kind.toByte()) + bytes).toByteString())) {
            val error = IllegalStateException("Mobile transport backpressure")
            stop(error, socket); throw error
        }
    }
    override fun close() { stop(null, socket) }
}
