package app.unison.settingsfile

import app.unison.service.NotificationSettings
import app.unison.service.OperatingMode
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.time.Instant
import java.time.ZoneOffset

/** Tests of the app section of a settings document. */
internal class SettingsFileTest {
    @Test
    fun `the file name follows the platform format`() {
        val at = Instant.parse("2026-11-24T10:20:30Z")
        assertEquals("unison-android-20261124-102030.json", SettingsFile.fileName(at))
    }

    @Test
    fun `the file name uses UTC`() {
        val at = Instant.parse("2026-11-24T23:20:30Z")
        assertEquals("unison-android-20261124-232030.json", SettingsFile.fileName(at.atOffset(ZoneOffset.ofHours(3)).toInstant()))
    }

    @Test
    fun `the app section carries the local settings`() {
        val section =
            SettingsFile.appSection(
                operatingMode = OperatingMode.WIFI_ONLY,
                notifications = NotificationSettings(transfers = false, requests = true),
            )
        val read = SettingsFile.appSectionOf("""{"app": $section}""")
        assertEquals(OperatingMode.WIFI_ONLY, read.operatingMode)
        assertEquals(false, read.notifications?.transfers)
        assertEquals(true, read.notifications?.requests)
        assertEquals(true, read.notifications?.clipboard)
    }

    @Test
    fun `the section of another platform is ignored`() {
        val read = SettingsFile.appSectionOf("""{"app": {"desktop": {"theme": "dark"}}}""")
        assertNull(read.operatingMode)
        assertNull(read.notifications)
    }

    @Test
    fun `an unknown operating mode is ignored`() {
        val read = SettingsFile.appSectionOf("""{"app": {"android": {"operating_mode": "always_on"}}}""")
        assertNull(read.operatingMode)
    }

    @Test
    fun `the absent categories keep the stored values`() {
        val current = NotificationSettings()
        val read = SettingsFile.appSectionOf("""{"app": {"android": {"notifications": {"transfers": false}}}}""")
        val applied = read.notifications?.applyTo(current) ?: current
        assertEquals(false, applied.transfers)
        assertEquals(current.requests, applied.requests)
        assertEquals(current.clipboard, applied.clipboard)
    }

    @Test
    fun `a value of the wrong type keeps the stored value`() {
        val current = NotificationSettings(transfers = true)
        val read = SettingsFile.appSectionOf("""{"app": {"android": {"notifications": {"transfers": "no"}}}}""")
        assertEquals(null, read.notifications?.transfers)
        assertEquals(true, read.notifications?.applyTo(current)?.transfers)
    }

    @Test
    fun `text that is not a document is accepted`() {
        assertEquals(AppSection(), SettingsFile.appSectionOf("not json"))
        assertEquals(AppSection(), SettingsFile.appSectionOf("[]"))
        assertEquals(AppSection(), SettingsFile.appSectionOf("null"))
    }
}
