package app.unison.updater

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Tests of the semantic version of a release. */
internal class SemVerTest {
    @Test
    fun `a plain version is read`() {
        val version = SemVer.parse("0.3.12") ?: error("the version was refused")
        assertEquals(0L, version.major)
        assertEquals(3L, version.minor)
        assertEquals(12L, version.patch)
        assertFalse(version.isPreRelease)
        assertEquals("0.3.12", version.text)
    }

    @Test
    fun `a leading v is accepted`() {
        assertEquals("0.3.0", SemVer.parse("v0.3.0")?.text)
    }

    @Test
    fun `build metadata does not change the version`() {
        assertEquals(SemVer.parse("0.3.0"), SemVer.parse("0.3.0+build.7"))
        assertEquals(0, SemVer.parse("0.3.0")!!.compareTo(SemVer.parse("0.3.0+abc")!!))
    }

    @Test
    fun `numbers decide first`() {
        assertTrue(SemVer.parse("0.3.0")!! > SemVer.parse("0.2.9")!!)
        assertTrue(SemVer.parse("1.0.0")!! > SemVer.parse("0.99.99")!!)
        assertEquals(0, SemVer.parse("0.3.0")!!.compareTo(SemVer.parse("0.3.0")!!))
    }

    @Test
    fun `a release is newer than its pre-releases`() {
        assertTrue(SemVer.parse("0.3.0")!! > SemVer.parse("0.3.0-beta.2")!!)
        assertTrue(SemVer.parse("0.3.0-rc.1")!! > SemVer.parse("0.3.0-beta.10")!!)
        assertTrue(SemVer.parse("0.3.0-beta.1")!! > SemVer.parse("0.3.0-alpha.9")!!)
    }

    @Test
    fun `pre-release numbers compare as integers`() {
        assertTrue(SemVer.parse("0.3.0-beta.10")!! > SemVer.parse("0.3.0-beta.2")!!)
        assertTrue(SemVer.parse("0.3.0-beta.2")!! > SemVer.parse("0.3.0-beta")!!)
    }

    @Test
    fun `a word is newer than a number`() {
        assertTrue(SemVer.parse("0.3.0-alpha")!! > SemVer.parse("0.3.0-1")!!)
    }

    @Test
    fun `a pre-release is marked`() {
        assertTrue(SemVer.parse("0.2.0-a1b2c3d")!!.isPreRelease)
        assertFalse(SemVer.parse("0.2.0")!!.isPreRelease)
    }

    @Test
    fun `the text keeps the pre-release`() {
        assertEquals("0.3.0-beta.2", SemVer.parse("v0.3.0-beta.2+sha")?.text)
    }

    @Test
    fun `an unreadable version is refused`() {
        listOf("", "abc", "1", "1.2", "1.2.3.4", "1.02.3", "1.2.3-", "1.2.3-beta..1", "1.2.3-beta 1").forEach { text ->
            assertNull("$text must be refused", SemVer.parse(text))
        }
    }

    @Test
    fun `equal versions are equal objects`() {
        assertEquals(SemVer.parse("0.3.0-beta.2"), SemVer.parse("v0.3.0-beta.2"))
        assertEquals(SemVer.parse("0.3.0-beta.2").hashCode(), SemVer.parse("0.3.0-beta.2").hashCode())
    }
}
