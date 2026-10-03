package dev.cymonkey.androidhelper

import android.webkit.CookieManager
import android.webkit.WebView
import java.lang.ref.WeakReference

/** A browser surface explicitly registered by its owning app. Calls run on the UI thread. */
interface BrowserSurface {
    val currentUrl: String?
    val pageTitle: String?
    fun evaluateApprovedScript(source: String, completion: (Boolean) -> Unit)
    fun cookieHeader(): String?
}

class WebViewSurface(webView: WebView) : BrowserSurface {
    private val reference = WeakReference(webView)

    override val currentUrl: String? get() = reference.get()?.url
    override val pageTitle: String? get() = reference.get()?.title

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
