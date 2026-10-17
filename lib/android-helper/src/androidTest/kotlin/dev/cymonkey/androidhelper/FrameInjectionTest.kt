package dev.cymonkey.androidhelper

import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertTrue
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.io.ByteArrayInputStream
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

@RunWith(AndroidJUnit4::class)
class FrameInjectionTest {
    @Test fun unsupportedSurfaceBlocksAllFrameApproval() {
        InstrumentationRegistry.getInstrumentation().runOnMainSync {
            val runtime = AndroidWebViewRuntime()
            runtime.registerSurface("mock", object : BrowserSurface {
                override val currentUrl = "https://app.example.test/"
                override val pageTitle: String? = null
                override fun evaluateApprovedScript(source: String, completion: (Boolean) -> Unit) = completion(false)
                override fun cookieHeader(): String? = null
            })
            val source = """
                // ==UserScript==
                // @name Frame test
                // @match https://app.example.test/*
                // @grant none
                // ==/UserScript==
                document.body.dataset.frameTest = 'yes';
            """.trimIndent()
            assertFalse(runtime.userScripts.inspect(source, "mock").runnable)
            val candidate = runtime.userScripts.prepare("frame-test", source)
            assertThrows(IllegalArgumentException::class.java) { runtime.userScripts.installApproved(candidate) }
        }
    }

    @Test fun approvedScriptRunsInTopDocumentAndMatchingIframe() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val finished = CountDownLatch(1)
        var view: WebView? = null
        instrumentation.runOnMainSync {
            val webView = WebView(instrumentation.targetContext)
            view = webView
            webView.settings.javaScriptEnabled = true
            val surface = WebViewSurface(webView)
            assumeTrue("WebView lacks document-start registration", surface.supportsFrameScripts)
            webView.webViewClient = object : WebViewClient() {
                override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                    if (request.url.host != "app.example.test") return null
                    val html = "<html><body>child</body></html>"
                    return WebResourceResponse("text/html", "UTF-8", ByteArrayInputStream(html.toByteArray()))
                }

                override fun onPageFinished(view: WebView, url: String) { finished.countDown() }
            }
            val runtime = AndroidWebViewRuntime()
            runtime.registerSurface("main", surface)
            val source = """
                // ==UserScript==
                // @name Frame test
                // @match https://app.example.test/*
                // @exclude-match https://app.example.test/private/*
                // @grant none
                // @run-at document-end
                // ==/UserScript==
                document.body.dataset.cymonkeyFrameTest = 'yes';
            """.trimIndent()
            runtime.userScripts.installApproved(runtime.userScripts.prepare("frame-test", source))
            webView.loadDataWithBaseURL("https://app.example.test/", """
                <html><body>top
                  <iframe id="child" src="https://app.example.test/child"></iframe>
                  <iframe id="excluded" src="https://app.example.test/private/child"></iframe>
                </body></html>
            """.trimIndent(), "text/html", "UTF-8", null)
        }
        try {
            assertTrue("top document did not finish", finished.await(15, TimeUnit.SECONDS))
            var executed = false
            repeat(30) {
                val result = CountDownLatch(1)
                instrumentation.runOnMainSync {
                    view!!.evaluateJavascript("""
                        (() => document.body.dataset.cymonkeyFrameTest === 'yes' &&
                          document.getElementById('child')?.contentDocument?.body?.dataset.cymonkeyFrameTest === 'yes' &&
                          document.getElementById('excluded')?.contentDocument?.body?.dataset.cymonkeyFrameTest !== 'yes')()
                    """.trimIndent()) {
                        executed = it == "true"
                        result.countDown()
                    }
                }
                assertTrue("JavaScript callback did not return", result.await(5, TimeUnit.SECONDS))
                if (executed) return@repeat
                Thread.sleep(100)
            }
            assertTrue("script did not execute in both documents", executed)
        } finally {
            instrumentation.runOnMainSync { view?.destroy() }
        }
    }
}
