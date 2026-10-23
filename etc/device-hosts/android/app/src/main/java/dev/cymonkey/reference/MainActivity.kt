package dev.cymonkey.reference

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.media.projection.MediaProjectionManager
import android.os.Bundle
import android.widget.*

/** Credential entry is a lab fixture, never an operator-token login. */
class MainActivity : Activity() {
    private lateinit var endpoint: EditText
    private lateinit var token: EditText
    private lateinit var audio: CheckBox
    private lateinit var mic: CheckBox
    private lateinit var playback: CheckBox
    private var pending: Intent? = null
    override fun onCreate(state: Bundle?) {
        super.onCreate(state)
        val layout = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(32, 32, 32, 32) }
        fun label(text: String) { layout.addView(TextView(this).apply { this.text = text }) }
        label("Board reference • video target\nUse a fresh scoped target token; never an operator token.")
        endpoint = EditText(this).apply { hint = "wss://host/v1/device-targets/mobile-…"; isSingleLine = true; isSaveEnabled = false }; layout.addView(endpoint)
        token = EditText(this).apply { hint = "Scoped target token"; inputType = 129; isSaveEnabled = false }; layout.addView(token)
        audio = CheckBox(this).apply { text = "System audio (must be granted)" }; layout.addView(audio)
        mic = CheckBox(this).apply { text = "Microphone (must be granted)" }; layout.addView(mic)
        playback = CheckBox(this).apply { text = "Playback (must be granted)" }; layout.addView(playback)
        layout.addView(Button(this).apply { text = "Consent and start"; setOnClickListener { begin() } })
        layout.addView(Button(this).apply { text = "Stop"; setOnClickListener { pending = null; stopService(Intent(this@MainActivity, CaptureService::class.java)) } })
        label("Use the browser viewer on another device. Stop from here, the notification, or the OS capture indicator. Rotation may letterbox the stream.")
        setContentView(layout)
    }
    private fun begin() {
        if (pending != null || CaptureService.running) { toast("Stop the current attempt first"); return }
        if (!endpoint.text.toString().startsWith("wss://") || token.text.length != 64) { toast("Enter a WSS endpoint and scoped token"); return }
        pending = Intent(this, CaptureService::class.java).putExtra("url", endpoint.text.toString()).putExtra("token", token.text.toString())
            .putExtra("audio", audio.isChecked).putExtra("mic", mic.isChecked).putExtra("playback", playback.isChecked)
        if ((audio.isChecked || mic.isChecked) && checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(Manifest.permission.RECORD_AUDIO), 10)
        } else consent()
    }
    private fun consent() { startActivityForResult(getSystemService(MediaProjectionManager::class.java).createScreenCaptureIntent(), 11) }
    override fun onRequestPermissionsResult(code: Int, permissions: Array<out String>, results: IntArray) {
        super.onRequestPermissionsResult(code, permissions, results)
        if (code == 10 && pending != null) { if (results.firstOrNull() == PackageManager.PERMISSION_GRANTED) consent() else { pending = null; toast("Audio permission denied") } }
    }
    override fun onActivityResult(code: Int, result: Int, data: Intent?) {
        super.onActivityResult(code, result, data)
        if (code != 11) return
        val request = pending; pending = null
        if (result != RESULT_OK || data == null || request == null) { toast("Capture cancelled"); return }
        request.putExtra("consent", data).putExtra("result", result)
        token.text.clear()
        try { startForegroundService(request) } catch (_: Exception) { toast("Could not start capture service") }
    }
    private fun toast(message: String) { Toast.makeText(this, message, Toast.LENGTH_LONG).show() }
}
