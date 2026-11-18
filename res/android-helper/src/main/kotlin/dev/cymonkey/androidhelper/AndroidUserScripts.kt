package dev.cymonkey.androidhelper

import android.content.Context
import android.os.Handler
import android.os.Looper
import org.json.JSONObject
import java.net.URI
import java.security.MessageDigest

/** A reviewed userscript. The source and its metadata are inseparable. */
data class UserScriptCandidate(
    val id: String,
    val name: String,
    val revision: String,
    val source: String,
    val matches: List<String>,
    val excludeMatches: List<String>,
    val runAt: String,
    val sourceOrigin: String,
    val includes: List<String> = emptyList(),
    val excludes: List<String> = emptyList(),
    val allFrames: Boolean = true,
)

/** Import-time assessment. A script can be inspected even when this runtime cannot execute it. */
data class UserScriptCompatibilityReport(
    val name: String?,
    val grants: List<String>,
    val matches: List<String>,
    val includes: List<String>,
    val blockers: List<String>,
    val notes: List<String>,
) {
    val runnable: Boolean get() = blockers.isEmpty()
}

data class ApprovedUserScript(val candidate: UserScriptCandidate, val enabled: Boolean = true)

/** Supply a private persistent implementation, or use [SharedPreferencesUserScriptStore]. */
interface UserScriptStore {
    fun list(): List<ApprovedUserScript>
    fun put(script: ApprovedUserScript)
    fun remove(id: String)
}

class InMemoryUserScriptStore : UserScriptStore {
    private val scripts = linkedMapOf<String, ApprovedUserScript>()
    override fun list(): List<ApprovedUserScript> = scripts.values.toList()
    override fun put(script: ApprovedUserScript) { scripts[script.candidate.id] = script }
    override fun remove(id: String) { scripts.remove(id) }
}

/** Private app storage. The embedding app is responsible for approval and backup policy. */
class SharedPreferencesUserScriptStore(context: Context, name: String = "cymonkey-userscripts") : UserScriptStore {
    private val preferences = context.applicationContext.getSharedPreferences(name, Context.MODE_PRIVATE)

    override fun list(): List<ApprovedUserScript> = preferences.all.keys.sorted().mapNotNull { id ->
        val value = preferences.getString(id, null) ?: return@mapNotNull null
        try {
            val objectValue = JSONObject(value)
            val source = objectValue.getString("source")
            val candidate = UserScriptMetadata.prepare(id, source, objectValue.getString("sourceOrigin"))
            if (candidate.revision != objectValue.getString("revision")) return@mapNotNull null
            ApprovedUserScript(candidate, objectValue.optBoolean("enabled", true))
        } catch (_: Exception) { null } // Corrupt or unsupported records fail closed.
    }

    override fun put(script: ApprovedUserScript) {
        val candidate = script.candidate
        val canonical = UserScriptMetadata.prepare(candidate.id, candidate.source, candidate.sourceOrigin)
        require(candidate == canonical) { "userscript candidate changed after review" }
        val value = JSONObject().put("source", candidate.source)
            .put("sourceOrigin", candidate.sourceOrigin)
            .put("revision", candidate.revision)
            .put("enabled", script.enabled).toString()
        check(preferences.edit().putString(candidate.id, value).commit()) { "failed to save userscript" }
    }

    override fun remove(id: String) {
        check(preferences.edit().remove(id).commit()) { "failed to remove userscript" }
    }
}

/** Metadata and URL rules supported by the Android WebView runtime. */
object UserScriptMetadata {
    private val idPattern = Regex("[A-Za-z0-9][A-Za-z0-9._-]{0,127}")
    private val directivePattern = Regex("\\s*//\\s*@([A-Za-z][A-Za-z0-9_:-]*)(?:\\s+(.+?))?\\s*")
    private const val MAX_SOURCE_BYTES = 1024 * 1024
    private const val MAX_HEADER_CHARS = 64 * 1024
    private const val START_MARKER = "// ==UserScript=="
    private const val END_MARKER = "// ==/UserScript=="
    private val descriptiveDirectives = setOf(
        "name", "namespace", "version", "description", "author", "copyright", "license", "icon", "iconurl",
        "icon64", "icon64url", "defaulticon", "homepage", "homepageurl", "website", "source", "supporturl",
        "tag", "antifeature", "downloadurl", "updateurl",
    )
    private val executionDirectives = setOf("match", "exclude-match", "include", "exclude", "grant", "run-at", "noframes", "inject-into")
    private val unsupportedDirectives = setOf("require", "resource", "connect", "run-in", "sandbox", "unwrap", "top-level-await", "webrequest")

    /** Reports the declared requirements of a VM, TM, or GM userscript without claiming a vendor identity. */
    fun inspect(source: String): UserScriptCompatibilityReport {
        val blockers = mutableListOf<String>()
        val notes = mutableListOf<String>()
        if (source.isBlank() || source.toByteArray(Charsets.UTF_8).size > MAX_SOURCE_BYTES) {
            blockers += "source is empty or exceeds 1 MiB"
        }
        val directives = readDirectives(source)
        if (directives == null) {
            return UserScriptCompatibilityReport(null, emptyList(), emptyList(), emptyList(),
                blockers + "userscript metadata block is required", notes)
        }
        val name = directives["name"]?.firstOrNull()
        if (name.isNullOrBlank() || name.length > 128) blockers += "@name is required and must be at most 128 characters"
        val matches = directives["match"].orEmpty()
        val excludeMatches = directives["exclude-match"].orEmpty()
        val includes = directives["include"].orEmpty()
        val excludes = directives["exclude"].orEmpty()
        if (matches.isEmpty() && includes.isEmpty()) blockers += "@match or @include is required"
        if (matches.size + includes.size > 128 || excludeMatches.size + excludes.size > 128) blockers += "too many URL rules"
        for (pattern in matches + excludeMatches) {
            try { validatePattern(pattern) } catch (_: IllegalArgumentException) { blockers += "unsupported @match pattern: $pattern" }
        }
        for (pattern in includes + excludes) {
            try { validateInclude(pattern) } catch (_: IllegalArgumentException) { blockers += "unsupported @include pattern: $pattern" }
        }
        val grants = directives["grant"].orEmpty()
        when {
            grants.isEmpty() -> blockers += "missing @grant: sandboxed grantless scripts need a userscript API backend"
            grants != listOf("none") -> blockers += "privileged @grant APIs need a userscript API backend: ${grants.joinToString()}"
        }
        val runAt = directives["run-at"].orEmpty()
        if (runAt.size > 1 || (runAt.isNotEmpty() && runAt.singleOrNull() !in setOf("document-end", "document-idle"))) {
            blockers += "@run-at requires a lifecycle stage unavailable in this WebView backend"
        }
        val injectInto = directives["inject-into"].orEmpty()
        if (injectInto.size > 1 || injectInto.any { it !in setOf("page", "auto") }) {
            blockers += "@inject-into requires an execution world unavailable in this WebView backend"
        }
        for (key in directives.keys) {
            when {
                key in unsupportedDirectives -> blockers += "@$key requires a userscript API backend"
                key !in descriptiveDirectives && key !in executionDirectives && !key.startsWith("name:") &&
                    !key.startsWith("description:") -> blockers += "unrecognized @$key directive"
            }
        }
        if (!directives.containsKey("noframes")) {
            if ((matches + includes).any { it.removePrefix("https://").substringBefore('/') == "*" }) {
                blockers += "frame injection requires explicit HTTPS hosts; host-wide * rules are unsupported"
            }
            notes += "all-frame execution requires WebView DOCUMENT_START_SCRIPT support and registration before navigation"
            notes += "all-frame document-end and document-idle run at or after DOMContentLoaded"
        } else {
            notes += "main-frame document-end and document-idle run after WebView.onPageFinished"
        }
        if (directives.containsKey("downloadurl") || directives.containsKey("updateurl")) {
            notes += "update URLs are retained in source but updates require a new host approval"
        }
        if (Regex("\\b(?:GM_[A-Za-z]|GM\\.[A-Za-z])").containsMatchIn(source) && grants == listOf("none")) {
            notes += "source mentions GM APIs, which are unavailable with @grant none"
        }
        return UserScriptCompatibilityReport(name, grants, matches, includes, blockers.distinct(), notes)
    }

    fun prepare(id: String, source: String, sourceOrigin: String = "user"): UserScriptCandidate {
        require(idPattern.matches(id)) { "invalid userscript ID" }
        require(sourceOrigin == "user" || (sourceOrigin.startsWith("https://") && safeHttpsUri(sourceOrigin) != null)) {
            "userscript source must be user-owned or HTTPS"
        }
        val report = inspect(source)
        require(report.runnable) { report.blockers.joinToString("; ") }
        val directives = requireNotNull(readDirectives(source))
        val name = requireNotNull(report.name)
        val matches = directives["match"].orEmpty()
        val excludes = directives["exclude-match"].orEmpty()
        val includes = directives["include"].orEmpty()
        val excludesGlobs = directives["exclude"].orEmpty()
        val runAt = directives["run-at"]?.singleOrNull() ?: "document-end"
        val revision = MessageDigest.getInstance("SHA-256").digest(source.toByteArray(Charsets.UTF_8))
            .joinToString("") { "%02x".format(it) }
        return UserScriptCandidate(id, name, "sha256:$revision", source, matches.toList(), excludes.toList(), runAt,
            sourceOrigin, includes.toList(), excludesGlobs.toList(), !directives.containsKey("noframes"))
    }

    /** WebView origin rules are broader than path rules; the injected source checks the full URL again. */
    fun frameOriginRules(candidate: UserScriptCandidate): Set<String> = (candidate.matches + candidate.includes).flatMap { pattern ->
        val host = pattern.removePrefix("https://").substringBefore('/').lowercase()
        require(host != "*") { "frame scripts require explicit hosts" }
        if (host.startsWith("*.") && candidate.matches.any { it.removePrefix("https://").substringBefore('/').lowercase() == host }) {
            listOf("https://$host", "https://${host.drop(2)}")
        } else listOf("https://$host")
    }.toSet()

    /** Injected at document start in every allowed frame, then waits for the requested stage. */
    fun frameScriptSource(candidate: UserScriptCandidate): String {
        val matchRules = jsArray(candidate.matches)
        val includeRules = jsArray(candidate.includes)
        val excludeMatchRules = jsArray(candidate.excludeMatches)
        val excludeRules = jsArray(candidate.excludes)
        val idle = candidate.runAt == "document-idle"
        return """
            (() => {
              if (location.protocol !== 'https:' || (location.port && location.port !== '443')) return;
              const url = new URL(location.href);
              const host = url.hostname.toLowerCase();
              const path = url.pathname + url.search;
              const glob = (rule, value) => {
                const parts = rule.split('*');
                if (parts.length === 1) return value === rule;
                if (!value.startsWith(parts[0])) return false;
                let cursor = parts[0].length;
                for (let i = 1; i < parts.length - 1; i++) {
                  const found = value.indexOf(parts[i], cursor);
                  if (found < 0) return false;
                  cursor = found + parts[i].length;
                }
                return value.slice(cursor).endsWith(parts[parts.length - 1]);
              };
              const matches = (rule, include) => {
                const rest = rule.slice(8);
                const split = rest.indexOf('/');
                const hostRule = rest.slice(0, split).toLowerCase();
                const pathRule = '/' + rest.slice(split + 1);
                const hostMatch = include ? glob(hostRule, host) :
                  hostRule === '*' || (hostRule.startsWith('*.') ?
                    host === hostRule.slice(2) || host.endsWith('.' + hostRule.slice(2)) : host === hostRule);
                return hostMatch && glob(pathRule, path);
              };
              if (!($matchRules.some(rule => matches(rule, false)) ||
                    $includeRules.some(rule => matches(rule, true))) ||
                  $excludeMatchRules.some(rule => matches(rule, false)) ||
                  $excludeRules.some(rule => matches(rule, true))) return;
              const run = () => {
                ${candidate.source}
              };
              const schedule = () => { if ($idle) setTimeout(run, 0); else run(); };
              if (document.readyState === 'loading')
                document.addEventListener('DOMContentLoaded', schedule, {once: true});
              else schedule();
            })();
        """.trimIndent()
    }

    private fun jsArray(values: List<String>) = values.joinToString(",", "[", "]") { value ->
        buildString {
            append('"')
            for (char in value) when (char) {
                '"' -> append("\\\"")
                '\\' -> append("\\\\")
                '\b' -> append("\\b")
                '\u000c' -> append("\\f")
                '\n' -> append("\\n")
                '\r' -> append("\\r")
                '\t' -> append("\\t")
                else -> if (char.code < 0x20 || char.code == 0x2028 || char.code == 0x2029)
                    append("\\u%04x".format(char.code)) else append(char)
            }
            append('"')
        }
    }

    private fun readDirectives(source: String): Map<String, List<String>>? {
        val header = source.take(MAX_HEADER_CHARS)
        val start = header.indexOf(START_MARKER)
        if (start < 0) return null
        val end = header.indexOf(END_MARKER, start + START_MARKER.length)
        if (end < 0) return null
        val directives = mutableMapOf<String, MutableList<String>>()
        for (line in header.substring(start + START_MARKER.length, end).lines()) {
            val match = directivePattern.matchEntire(line) ?: continue
            directives.getOrPut(match.groupValues[1].lowercase()) { mutableListOf() }.add(match.groupValues[2])
        }
        return directives
    }

    fun requiresFrames(source: String): Boolean = readDirectives(source)?.containsKey("noframes") == false

    fun matches(candidate: UserScriptCandidate, url: String): Boolean {
        val parsed = safeHttpsUri(url) ?: return false
        return (candidate.matches.any { patternMatches(it, parsed) } || candidate.includes.any { includeMatches(it, parsed) }) &&
            candidate.excludeMatches.none { patternMatches(it, parsed) } && candidate.excludes.none { includeMatches(it, parsed) }
    }

    private fun validateInclude(pattern: String) {
        require(pattern.length <= 512 && pattern.startsWith("https://") && !pattern.any(Char::isWhitespace) && '@' !in pattern) {
            "only bounded HTTPS @include globs are supported"
        }
        val rest = pattern.removePrefix("https://")
        require('/' in rest && rest.substringBefore('/').let { host ->
            host == "*" || host.matches(Regex("(\\*\\.)?[A-Za-z0-9-]+(\\.[A-Za-z0-9-]+)*"))
        }) { "invalid @include host" }
    }

    private fun includeMatches(pattern: String, url: URI): Boolean {
        if (url.port !in listOf(-1, 443)) return false
        val rest = pattern.removePrefix("https://")
        val hostRule = rest.substringBefore('/').lowercase()
        val host = url.host?.lowercase() ?: return false
        val hostRegex = "^" + hostRule.split('*').joinToString(".*") { Regex.escape(it) } + "$"
        if (!Regex(hostRegex).matches(host)) return false
        val path = (url.rawPath?.ifEmpty { "/" } ?: "/") + (url.rawQuery?.let { "?$it" } ?: "")
        val pathRegex = "^/" + rest.substringAfter('/').split('*').joinToString(".*") { Regex.escape(it) } + "$"
        return Regex(pathRegex).matches(path)
    }

    private fun validatePattern(pattern: String) {
        require(pattern.length <= 512 && pattern.startsWith("https://")) { "only bounded HTTPS @match rules are supported" }
        val authority = pattern.removePrefix("https://").substringBefore('/')
        require(authority.isNotBlank() && '/' in pattern.removePrefix("https://")) { "invalid @match rule" }
        require(authority == "*" || authority.matches(Regex("(\\*\\.)?[A-Za-z0-9-]+(\\.[A-Za-z0-9-]+)*"))) {
            "invalid @match host"
        }
    }

    private fun patternMatches(pattern: String, url: URI): Boolean {
        val rest = pattern.removePrefix("https://")
        val hostRule = rest.substringBefore('/').lowercase()
        val host = url.host?.lowercase() ?: return false
        val hostMatches = when {
            hostRule == "*" -> true
            hostRule.startsWith("*.") -> host == hostRule.drop(2) || host.endsWith(".${hostRule.drop(2)}")
            else -> host == hostRule
        }
        if (!hostMatches || url.port !in listOf(-1, 443)) return false
        val pathRule = rest.substringAfter('/', "")
        val path = (url.rawPath?.ifEmpty { "/" } ?: "/") + (url.rawQuery?.let { "?$it" } ?: "")
        val regex = "^/" + pathRule.split('*').joinToString(".*") { Regex.escape(it) } + "$"
        return Regex(regex).matches(path)
    }

    private fun safeHttpsUri(value: String): URI? = try {
        URI(value).takeIf { it.scheme.equals("https", true) && !it.host.isNullOrBlank() && it.userInfo == null }
    } catch (_: Exception) { null }
}

/** SDK manager for page-world, @grant none userscripts. Calls run on the UI thread. */
class AndroidUserScriptManager internal constructor(
    private val store: UserScriptStore,
    private val surfaceFor: (String) -> BrowserSurface?,
    private val surfaceIds: () -> Set<String>,
    private val onApplied: (String, String) -> Unit,
    private val isLegacyId: (String) -> Boolean,
) {
    private data class FrameRegistration(val revision: String, val handle: DocumentScriptHandle)
    private val handler = Handler(Looper.getMainLooper())
    private val generations = mutableMapOf<String, Long>()
    private val applied = mutableSetOf<Triple<String, Long, String>>()
    private val frameRegistrations = mutableMapOf<Pair<String, String>, FrameRegistration>()

    fun inspect(source: String) = UserScriptMetadata.inspect(source)
    fun inspect(source: String, surfaceId: String): UserScriptCompatibilityReport {
        requireMainThread()
        val report = inspect(source)
        if (!UserScriptMetadata.requiresFrames(source)) return report
        val surface = surfaceFor(surfaceId)
        return if (surface is FrameScriptSurface && surface.supportsFrameScripts) report
        else report.copy(blockers = report.blockers + "selected WebView does not support frame script registration")
    }
    fun prepare(id: String, source: String, sourceOrigin: String = "user") = UserScriptMetadata.prepare(id, source, sourceOrigin)
    fun list(): List<ApprovedUserScript> = store.list()

    /** Call only after the host has shown the candidate and obtained its approval. Updates need a new call. */
    fun installApproved(candidate: UserScriptCandidate) {
        requireMainThread()
        require(candidate == prepare(candidate.id, candidate.source, candidate.sourceOrigin)) { "userscript changed after review" }
        require(!isLegacyId(candidate.id)) { "userscript ID is already registered" }
        if (candidate.allFrames) surfaceIds().forEach { requireFrameSupport(requireNotNull(surfaceFor(it))) }
        store.put(ApprovedUserScript(candidate))
        reconcileAllFrames()
    }

    fun setEnabled(id: String, enabled: Boolean) {
        requireMainThread()
        val existing = store.list().firstOrNull { it.candidate.id == id } ?: throw IllegalArgumentException("unknown userscript")
        if (enabled && existing.candidate.allFrames) surfaceIds().forEach { requireFrameSupport(requireNotNull(surfaceFor(it))) }
        store.put(existing.copy(enabled = enabled))
        reconcileAllFrames()
    }

    fun remove(id: String) { requireMainThread(); store.remove(id); reconcileAllFrames() }

    internal fun assertSurfaceSupported(surface: BrowserSurface) {
        if (store.list().any { it.enabled && it.candidate.allFrames }) requireFrameSupport(surface)
    }

    internal fun onSurfaceRegistered(id: String) { reconcileSurface(id) }

    /** Forward the host's WebViewClient.onPageStarted for each main-frame navigation. */
    fun onNavigationStarted(surfaceId: String) {
        requireMainThread()
        generations[surfaceId] = (generations[surfaceId] ?: 0L) + 1L
        applied.removeAll { it.first == surfaceId }
    }

    /** Forward onPageFinished. document-idle runs on the next main-loop turn. */
    fun onNavigationFinished(surfaceId: String) {
        requireMainThread()
        val url = surfaceFor(surfaceId)?.currentUrl ?: return
        val generation = generations[surfaceId] ?: 0L
        for (record in store.list()) {
            val candidate = record.candidate
            if (!record.enabled || candidate.allFrames || !UserScriptMetadata.matches(candidate, url)) continue
            val run = {
                if (generations[surfaceId] == generation && surfaceFor(surfaceId)?.currentUrl == url) {
                    applyInternal(candidate, surfaceId, url, generation, false) { }
                }
            }
            if (candidate.runAt == "document-idle") handler.post { run() } else run()
        }
    }

    /** Explicit application still checks the approved URL rules and current page. */
    fun apply(id: String, surfaceId: String, completion: (Boolean) -> Unit) {
        requireMainThread()
        val record = store.list().firstOrNull { it.candidate.id == id && it.enabled }
            ?: throw IllegalArgumentException("unknown or disabled userscript")
        require(!record.candidate.allFrames) { "all-frame scripts are registered for automatic execution" }
        val url = surfaceFor(surfaceId)?.currentUrl ?: throw IllegalArgumentException("web view unavailable")
        require(UserScriptMetadata.matches(record.candidate, url)) { "userscript is not approved for this page" }
        applyInternal(record.candidate, surfaceId, url, generations[surfaceId] ?: 0L, true, completion)
    }

    internal fun contains(id: String) = store.list().any { it.candidate.id == id }

    internal fun forgetSurface(id: String) {
        generations.remove(id)
        applied.removeAll { it.first == id }
        frameRegistrations.keys.filter { it.first == id }.forEach { key -> frameRegistrations.remove(key)?.handle?.remove() }
    }

    private fun reconcileAllFrames() { surfaceIds().forEach(::reconcileSurface) }

    private fun reconcileSurface(surfaceId: String) {
        val surface = surfaceFor(surfaceId) as? FrameScriptSurface ?: return
        val desired = store.list().filter { it.enabled && it.candidate.allFrames }.associateBy { it.candidate.id }
        frameRegistrations.keys.filter { it.first == surfaceId }.forEach { key ->
            val registered = frameRegistrations[key] ?: return@forEach
            if (desired[key.second]?.candidate?.revision != registered.revision) {
                registered.handle.remove()
                frameRegistrations.remove(key)
            }
        }
        if (desired.isEmpty()) return
        requireFrameSupport(surface)
        for ((scriptId, record) in desired.toSortedMap()) {
            val key = surfaceId to scriptId
            if (frameRegistrations.containsKey(key)) continue
            val candidate = record.candidate
            val handle = surface.addFrameScript(UserScriptMetadata.frameScriptSource(candidate),
                UserScriptMetadata.frameOriginRules(candidate))
            frameRegistrations[key] = FrameRegistration(candidate.revision, handle)
        }
    }

    private fun requireFrameSupport(surface: BrowserSurface) {
        require(surface is FrameScriptSurface && surface.supportsFrameScripts) {
            "selected WebView does not support frame script registration"
        }
    }

    private fun applyInternal(candidate: UserScriptCandidate, surfaceId: String, url: String, generation: Long,
                              explicit: Boolean, completion: (Boolean) -> Unit) {
        val key = Triple(surfaceId, generation, candidate.id)
        if (!explicit && !applied.add(key)) return
        val guarded = "(() => { if (location.href !== ${JSONObject.quote(url)}) return false;\n${candidate.source}\nreturn true; })()"
        val surface = surfaceFor(surfaceId) ?: return completion(false)
        surface.evaluateApprovedScript(guarded) { success ->
            if (!success) applied.remove(key)
            if (success) onApplied(surfaceId, candidate.id)
            completion(success)
        }
    }

    private fun requireMainThread() {
        check(Looper.myLooper() == Looper.getMainLooper()) { "AndroidUserScriptManager must run on the UI thread" }
    }
}
