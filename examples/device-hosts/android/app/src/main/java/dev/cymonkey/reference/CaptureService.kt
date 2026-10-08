package dev.cymonkey.reference

import android.app.*
import android.content.Intent
import android.content.pm.ServiceInfo
import android.media.projection.MediaProjection
import android.media.projection.MediaProjectionManager
import android.os.*
import dev.cymonkey.androidhelper.*
import okhttp3.OkHttpClient
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.atomic.AtomicBoolean

class CaptureService : Service() {
    companion object { @Volatile var running = false; private set }
    private val main = Handler(Looper.getMainLooper())
    private val closed = AtomicBoolean(false)
    private val client = OkHttpClient()
    private val packets = ArrayBlockingQueue<ByteArray>(12)
    private var capture: DeviceTargetCapture? = null
    private var transport: DeviceTargetTransport? = null
    private var projection: MediaProjection? = null
    override fun onBind(intent: Intent?) = null
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == "stop") { stopSelf(); return START_NOT_STICKY }
        if (running || intent == null) { if (!running) stopSelf(); return START_NOT_STICKY }
        running = true
        try {
            val mic = intent.getBooleanExtra("mic", false)
            val audio = intent.getBooleanExtra("audio", false)
            val playback = intent.getBooleanExtra("playback", false)
            val notifications = getSystemService(NotificationManager::class.java)
            notifications.createNotificationChannel(NotificationChannel("capture", "Board capture", NotificationManager.IMPORTANCE_LOW))
            val stop = PendingIntent.getService(this, 0, Intent(this, CaptureService::class.java).setAction("stop"), PendingIntent.FLAG_IMMUTABLE)
            val notice = Notification.Builder(this, "capture").setSmallIcon(android.R.drawable.ic_menu_view)
                .setContentTitle("Board reference capture active").setContentText("Tap Stop to end sharing")
                .setOngoing(true).addAction(Notification.Action.Builder(null, "Stop", stop).build()).build()
            var types = ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PROJECTION
            if (mic && Build.VERSION.SDK_INT >= 30) types = types or ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE
            startForeground(1, notice, types)
            val consent = intent.getParcelableExtra<Intent>("consent") ?: error("Fresh consent required")
            val approved = getSystemService(MediaProjectionManager::class.java).getMediaProjection(intent.getIntExtra("result", 0), consent) ?: error("Projection unavailable")
            projection = approved
            approved.registerCallback(object : MediaProjection.Callback() { override fun onStop() { main.post { stopSelf() } } }, main)
            if (playback) startPlayback()
            transport = DeviceTargetTransport(client, intent.getStringExtra("url")!!, intent.getStringExtra("token")!!,
                true, audio, mic, if (playback) ({ bytes -> check(packets.offer(bytes)) { "Playback queue full" } }) else null,
                onReady = { main.post {
                    if (!closed.get()) try {
                        val metrics = resources.displayMetrics
                        capture = DeviceTargetCapture(approved, transport!!, metrics.widthPixels, metrics.heightPixels,
                            metrics.densityDpi, true, audio, mic) { stopSelf() }
                    } catch (_: Exception) { stopSelf() }
                } }, onStop = { main.post { stopSelf() } })
            main.postDelayed({ stopSelf() }, 3_600_000) // Local upper bound even if remote disappears silently.
        } catch (_: Exception) { stopSelf() }
        finally { intent.removeExtra("token"); intent.removeExtra("consent") }
        return START_NOT_STICKY // Never replay a projection token after process death.
    }
    private fun startPlayback() {
        Thread({
            var output: DevicePlayback? = null
            try {
                output = DevicePlayback()
                while (!closed.get()) {
                    val bytes = packets.poll(100, java.util.concurrent.TimeUnit.MILLISECONDS) ?: continue
                    var offset = 0
                    val deadline = SystemClock.elapsedRealtime() + 250
                    while (offset < bytes.size && !closed.get()) {
                        val count = output.write(bytes, offset)
                        if (count == 0) { check(SystemClock.elapsedRealtime() < deadline); Thread.sleep(5) } else offset += count
                    }
                }
            } catch (_: Exception) { main.post { stopSelf() } }
            finally { output?.close() }
        }, "Board playback").apply { isDaemon = true; start() }
    }
    override fun onDestroy() {
        closed.set(true); main.removeCallbacksAndMessages(null)
        capture?.close(); transport?.close(); projection?.stop(); packets.clear()
        client.dispatcher.executorService.shutdown(); client.connectionPool.evictAll()
        running = false; stopForeground(STOP_FOREGROUND_REMOVE); super.onDestroy()
    }
}
