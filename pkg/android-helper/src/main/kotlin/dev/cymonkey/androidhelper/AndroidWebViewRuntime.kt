package dev.cymonkey.androidhelper

import android.os.Looper
import org.json.JSONArray
import org.json.JSONObject
import java.net.URI
import java.time.Instant

private const val PROTOCOL_VERSION = "cymonkey/v1alpha1"

private class HelperError(val code: String, override val message: String) : RuntimeException(message)

/**
 * In-process Cymonkey binding. The owning app supplies transport, authentication,
 * WebViews, reviewed script source, and cookie authorization.
 */
class AndroidWebViewRuntime(
    private val authorizeCookieRead: ((surfaceId: String, url: String, names: List<String>) -> Boolean)? = null,
    userScriptStore: UserScriptStore = InMemoryUserScriptStore(),
) {
    private data class ApprovedScript(val source: String, val origins: Set<String>)

    private val surfaces = mutableMapOf<String, BrowserSurface>()
    private val scripts = mutableMapOf<String, ApprovedScript>()
    private val eventLog = mutableListOf<JSONObject>()
    private var sequence = 0L
    val userScripts = AndroidUserScriptManager(userScriptStore, { surfaces[it] }, { surfaces.keys.toSet() },
        { surfaceId, scriptId -> emit("userscript.applied", surfaceId, scriptId) }, { scripts.containsKey(it) })

    fun registerSurface(id: String, surface: BrowserSurface) {
        requireMainThread()
        if (id.isBlank() || surfaces.containsKey(id)) {
            throw IllegalArgumentException("surface ID is empty or already registered")
        }
        userScripts.assertSurfaceSupported(surface)
        surfaces[id] = surface
        try { userScripts.onSurfaceRegistered(id) } catch (error: Exception) {
            surfaces.remove(id)
            userScripts.forgetSurface(id)
            throw error
        }
    }

    fun unregisterSurface(id: String) {
        requireMainThread()
        surfaces.remove(id)
        userScripts.forgetSurface(id)
    }

    /** The trusted host provides script source; control requests can only name its ID. */
    fun registerApprovedScript(id: String, source: String, allowedOrigins: List<String>) {
        requireMainThread()
        val origins = allowedOrigins.mapNotNull(::approvedOrigin).toSet()
        if (id.isBlank() || scripts.containsKey(id) || userScripts.contains(id) || source.isBlank() || origins.isEmpty() || origins.size != allowedOrigins.size) {
            throw IllegalArgumentException("script ID must be new and have source and valid HTTPS origins")
        }
        scripts[id] = ApprovedScript(source, origins)
    }

    fun removeApprovedScript(id: String) {
        requireMainThread()
        scripts.remove(id)
    }

    /** Call from the host's WebViewClient.onPageStarted for main-frame navigations. */
    fun onNavigationStarted(surfaceId: String) {
        requireMainThread()
        userScripts.onNavigationStarted(surfaceId)
    }

    /** Call from the host's WebViewClient.onPageFinished to reapply registered scripts. */
    fun onNavigationFinished(surfaceId: String) {
        requireMainThread()
        for (id in scripts.keys.sorted()) {
            try { apply(id, surfaceId) { } } catch (_: HelperError) { }
        }
        userScripts.onNavigationFinished(surfaceId)
    }

    fun handle(request: JSONObject, completion: (JSONObject) -> Unit) {
        requireMainThread()
        val id = request.optLong("id", 0)
        try {
            when (request.optString("method")) {
                "hello" -> completion(success(id, hello()))
                "capabilities" -> completion(success(id, capabilities()))
                "describe" -> completion(success(id, describe()))
                "events" -> completion(success(id, events(request.optJSONObject("params"))))
                "act" -> act(id, request.optJSONObject("params"), completion)
                else -> throw HelperError("invalid_request", "unsupported Cymonkey method")
            }
        } catch (error: HelperError) {
            completion(failure(id, error.code, error.message))
        } catch (_: Exception) {
            completion(failure(id, "native_failure", "web view operation failed"))
        }
    }

    private fun hello() = JSONObject()
        .put("protocolVersion", PROTOCOL_VERSION)
        .put("implementation", JSONObject().put("name", "cymonkey-android-helper").put("version", "0.1.0"))
        .put("domains", JSONArray().put("render"))
        .put("runtimes", JSONArray().put("browser-dom"))
        .put("drivers", JSONArray().put("android-webview"))
        .put("features", JSONArray().put("events.cursor").put("host.reviewed-scripts").put("host.userscript-manager"))

    private fun capabilities(): JSONArray {
        val result = JSONArray().put(capability("userscript.apply", "write", "surfaceId", "scriptId"))
        if (authorizeCookieRead != null) {
            result.put(capability("cookie.read", "read", "surfaceId", "names"))
        }
        return result
    }

    private fun capability(name: String, effect: String, vararg required: String) = JSONObject()
        .put("name", name)
        .put("description", "Host-approved operation on a registered Android WebView.")
        .put("domain", "render")
        .put("runtime", "browser-dom")
        .put("driver", "android-webview")
        .put("support", "mapped")
        .put("lifetime", "attachment")
        .put("persistence", "session")
        .put("effect", effect)
        .put("inputSchema", JSONObject()
            .put("type", "object")
            .put("required", JSONArray(required.toList()))
            .put("additionalProperties", false))

    private fun describe(): JSONObject {
        val listed = JSONArray()
        for (id in surfaces.keys.sorted()) {
            val surface = surfaces.getValue(id)
            listed.put(JSONObject()
                .put("id", id).put("domain", "render").put("runtime", "browser-dom")
                .put("kind", "webview").put("label", surface.pageTitle ?: JSONObject.NULL)
                .put("properties", JSONObject().put("url", surface.currentUrl ?: JSONObject.NULL)))
        }
        return JSONObject()
            .put("revision", surfaces.keys.sorted().joinToString("|"))
            .put("surfaces", listed)
            .put("augmentations", JSONArray())
    }

    private fun act(id: Long, params: JSONObject?, completion: (JSONObject) -> Unit) {
        val name = params?.optString("name")
        val input = params?.optJSONObject("input")
            ?: throw HelperError("invalid_request", "action requires name and input")
        val surfaceId = input.optString("surfaceId")
        if (surfaceId.isBlank()) throw HelperError("invalid_request", "surfaceId is required")
        when (name) {
            "userscript.apply" -> {
                val scriptId = input.optString("scriptId")
                if (scriptId.isBlank()) throw HelperError("invalid_request", "scriptId is required")
                apply(scriptId, surfaceId) { applied ->
                    if (applied) completion(success(id, JSONObject().put("applied", true)))
                    else completion(failure(id, "stale_reference", "registered web view is unavailable"))
                }
            }
            "cookie.read" -> {
                val authorize = authorizeCookieRead
                    ?: throw HelperError("denied", "cookie reading is unavailable")
                val namesArray = input.optJSONArray("names")
                    ?: throw HelperError("invalid_request", "explicit cookie names are required")
                val names = (0 until namesArray.length()).map { namesArray.optString(it) }
                if (names.isEmpty() || names.any { it.isBlank() }) {
                    throw HelperError("invalid_request", "explicit cookie names are required")
                }
                val surface = surfaces[surfaceId]
                    ?: throw HelperError("stale_reference", "registered web view is unavailable")
                val url = surface.currentUrl
                    ?: throw HelperError("stale_reference", "registered web view has no HTTPS page")
                if (origin(url) == null) {
                    throw HelperError("stale_reference", "registered web view has no HTTPS page")
                }
                if (!authorize(surfaceId, url, names)) throw HelperError("denied", "cookie read was not approved")
                val selected = names.toSet()
                val cookies = JSONArray()
                for (part in (surface.cookieHeader() ?: "").split(';')) {
                    val split = part.indexOf('=')
                    if (split < 1) continue
                    val cookieName = part.substring(0, split).trim()
                    if (cookieName in selected) {
                        cookies.put(JSONObject().put("name", cookieName).put("value", part.substring(split + 1).trim()))
                    }
                }
                emit("cookie.read", surfaceId)
                completion(success(id, JSONObject().put("cookies", cookies)))
            }
            else -> throw HelperError("denied", "capability was not advertised")
        }
    }

    private fun apply(scriptId: String, surfaceId: String, completion: (Boolean) -> Unit) {
        val surface = surfaces[surfaceId]
            ?: throw HelperError("stale_reference", "registered web view is unavailable")
        val pageOrigin = surface.currentUrl?.let(::origin)
            ?: throw HelperError("stale_reference", "registered web view has no HTTPS page")
        val script = scripts[scriptId]
        if (script == null) {
            if (!userScripts.contains(scriptId)) throw HelperError("denied", "script is not approved for this page")
            try {
                userScripts.apply(scriptId, surfaceId, completion)
            } catch (_: IllegalArgumentException) {
                throw HelperError("denied", "script is not approved for this page")
            }
            return
        }
        if (pageOrigin !in script.origins) {
            throw HelperError("denied", "script is not approved for this page")
        }
        val guarded = "(() => { if (location.origin !== ${JSONObject.quote(pageOrigin)}) return false;\n${script.source}\nreturn true; })()"
        surface.evaluateApprovedScript(guarded) { applied ->
            if (applied) emit("userscript.applied", surfaceId, scriptId)
            completion(applied)
        }
    }

    private fun events(params: JSONObject?): JSONObject {
        val after = params?.optString("after", "0")?.toLongOrNull() ?: 0L
        val limit = (params?.optInt("limit", 100) ?: 100).coerceIn(1, 500)
        val listed = eventLog.filter { it.getLong("id") > after }.take(limit)
        val result = JSONArray()
        listed.forEach { result.put(it) }
        return JSONObject().put("events", result).put("cursor", listed.lastOrNull()?.getString("id") ?: after.toString())
    }

    private fun emit(type: String, surfaceId: String, scriptId: String? = null) {
        sequence++
        val data = JSONObject().put("surfaceId", surfaceId)
        if (scriptId != null) data.put("scriptId", scriptId)
        eventLog.add(JSONObject()
            .put("id", sequence.toString()).put("type", type).put("occurredAt", Instant.now().toString())
            .put("domain", "render").put("runtime", "browser-dom")
            .put("driver", "android-webview").put("data", data))
        if (eventLog.size > 1_000) eventLog.removeAt(0)
    }

    private fun origin(url: String): String? = try {
        val parsed = URI(url)
        if (!parsed.scheme.equals("https", ignoreCase = true) || parsed.host.isNullOrBlank()) null
        else {
            val host = parsed.host.lowercase().let { if (it.contains(':') && !it.startsWith('[')) "[$it]" else it }
            "https://$host${if (parsed.port < 0 || parsed.port == 443) "" else ":${parsed.port}"}"
        }
    } catch (_: Exception) { null }

    private fun approvedOrigin(url: String): String? = try {
        val parsed = URI(url)
        if (parsed.userInfo != null || parsed.rawPath !in listOf(null, "", "/") ||
            parsed.rawQuery != null || parsed.rawFragment != null) null
        else origin(url)
    } catch (_: Exception) { null }

    private fun success(id: Long, result: Any) = JSONObject().put("id", id).put("result", result)
    private fun failure(id: Long, code: String, message: String) = JSONObject()
        .put("id", id).put("error", JSONObject().put("code", code).put("message", message))

    private fun requireMainThread() {
        check(Looper.myLooper() == Looper.getMainLooper()) { "AndroidWebViewRuntime must run on the UI thread" }
    }
}
