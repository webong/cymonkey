package dev.cymonkey.androidhelper

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.assertEquals
import org.junit.Test

class UserScriptMetadataTest {
    private val source = """
        // ==UserScript==
        // @name Reading tools
        // @match https://*.example.com/articles/*
        // @exclude-match https://private.example.com/articles/*
        // @grant none
        // @run-at document-end
        // ==/UserScript==
        document.body.dataset.readingTools = "true";
    """.trimIndent()

    @Test fun matchesApprovedPagesOnly() {
        val script = UserScriptMetadata.prepare("reading-tools", source)
        assertTrue(UserScriptMetadata.matches(script, "https://www.example.com/articles/one"))
        assertTrue(UserScriptMetadata.matches(script, "https://example.com/articles/two"))
        assertFalse(UserScriptMetadata.matches(script, "https://private.example.com/articles/one"))
        assertFalse(UserScriptMetadata.matches(script, "https://www.example.com/account"))
        assertFalse(UserScriptMetadata.matches(script, "http://www.example.com/articles/one"))
        assertEquals(71, script.revision.length)
    }

    @Test fun rejectsUnsupportedPrivilegesAndTiming() {
        assertRejected(source.replace("@grant none", "@grant GM_xmlhttpRequest"))
        assertRejected(source.replace("@run-at document-end", "@run-at document-start"))
        assertRejected(source.replace("@grant none", "@require https://example.com/dep.js"))
        assertRejected(source.replace("@match https://*.example.com/articles/*", "@match http://example.com/*"))
    }

    @Test fun inspectsLegacyAndModernManagerScriptsWithoutClaimingTheyRun() {
        val legacy = source.replace("@grant none", "@grant GM_getValue")
        val modern = source.replace("@grant none", "@grant GM.getValue")
        val grantless = source.lines().filterNot { it.contains("@grant") }.joinToString("\n")
        assertTrue(UserScriptMetadata.inspect(legacy).blockers.any { "GM_getValue" in it })
        assertTrue(UserScriptMetadata.inspect(modern).blockers.any { "GM.getValue" in it })
        assertTrue(UserScriptMetadata.inspect(grantless).blockers.any { "missing @grant" in it })
    }

    @Test fun supportsSimpleIncludeExcludeAndValuelessNoframes() {
        val script = """
            // ==UserScript==
            // @name Legacy URL rules
            // @include https://*.example.com/articles/*
            // @exclude https://private.example.com/articles/*
            // @noframes
            // @grant none
            // ==/UserScript==
            document.body.dataset.legacy = "true";
        """.trimIndent()
        val report = UserScriptMetadata.inspect(script)
        assertTrue(report.blockers.toString(), report.runnable)
        val candidate = UserScriptMetadata.prepare("legacy-rules", script)
        assertTrue(UserScriptMetadata.matches(candidate, "https://www.example.com/articles/one"))
        assertFalse(UserScriptMetadata.matches(candidate, "https://private.example.com/articles/one"))
        assertFalse(UserScriptMetadata.matches(candidate, "https://evil.test/path/.example.com/articles/one"))
        assertFalse(candidate.allFrames)
    }

    @Test fun frameRegistrationUsesScopedOriginsAndPreservesUrlExclusions() {
        val candidate = UserScriptMetadata.prepare("reading-tools", source)
        assertTrue(candidate.allFrames)
        assertEquals(setOf("https://*.example.com", "https://example.com"), UserScriptMetadata.frameOriginRules(candidate))
        val registered = UserScriptMetadata.frameScriptSource(candidate)
        assertTrue(registered.contains("https://private.example.com/articles/*"))
        assertTrue(registered.contains("DOMContentLoaded"))
        assertTrue(registered.contains("location.protocol !== 'https:'"))
    }

    @Test fun allFrameScriptsCannotUseOriginWideWildcard() {
        val broad = source.replace("https://*.example.com/articles/*", "https://*/*")
        assertTrue(UserScriptMetadata.inspect(broad).blockers.any { "explicit HTTPS hosts" in it })
        assertRejected(broad)
        val topOnly = broad.replace("// @grant none", "// @noframes\n// @grant none")
        assertTrue(UserScriptMetadata.inspect(topOnly).runnable)
    }

    @Test fun hostRulesAreCaseInsensitive() {
        val mixedCase = source.replace("*.example.com", "*.Example.COM")
        val candidate = UserScriptMetadata.prepare("mixed-case", mixedCase)
        assertTrue(UserScriptMetadata.matches(candidate, "https://www.example.com/articles/one"))
        assertEquals(setOf("https://*.example.com", "https://example.com"), UserScriptMetadata.frameOriginRules(candidate))
    }

    private fun assertRejected(value: String) {
        try {
            UserScriptMetadata.prepare("reading-tools", value)
            throw AssertionError("expected userscript to be rejected")
        } catch (_: IllegalArgumentException) { }
    }
}
