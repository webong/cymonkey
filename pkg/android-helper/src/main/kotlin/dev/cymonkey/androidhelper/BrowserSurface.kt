package dev.cymonkey.androidhelper

import android.webkit.CookieManager
import android.webkit.WebView
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import java.lang.ref.WeakReference

/** A browser surface explicitly registered by its owning app. Calls run on the UI thread. */
interface BrowserSurface {
    val currentUrl: String?
    val pageTitle: String?
    fun evaluateApprovedScript(source: String, completion: (Boolean) -> Unit)
    fun cookieHeader(): String?
}

fun interface DocumentScriptHandle { fun remove() }

/** Optional per-frame injection, available only on WebViews with DOCUMENT_START_SCRIPT support. */
interface FrameScriptSurface : BrowserSurface {
    val supportsFrameScripts: Boolean
    fun addFrameScript(source: String, allowedOriginRules: Set<String>): DocumentScriptHandle
}

class WebViewSurface(webView: WebView) : FrameScriptSurface {
    private val reference = WeakReference(webView)

    override val currentUrl: String? get() = reference.get()?.url
    override val pageTitle: String? get() = reference.get()?.title
    override val supportsFrameScripts: Boolean
        get() = reference.get() != null && WebViewFeature.isFeatureSupported(WebViewFeature.DOCUMENT_START_SCRIPT)

    override fun addFrameScript(source: String, allowedOriginRules: Set<String>): DocumentScriptHandle {
        val view = reference.get() ?: throw IllegalStateException("WebView is unavailable")
        check(supportsFrameScripts) { "this WebView does not support document-start script registration" }
        val handler = WebViewCompat.addDocumentStartJavaScript(view, source, allowedOriginRules)
        return DocumentScriptHandle { handler.remove() }
    }

    override fun evaluateApprovedScript(source: String, completion: (Boolean) -> Unit) {
        val view = reference.get()
        if (view == null) {
            completion(false)
            return
        }
        view.evaluateJavascript(source) { completion(it == "true") }
    }

    override fun cookieHeader(): String? {
        val url = currentUrl ?: return null
        return CookieManager.getInstance().getCookie(url)
    }
}
