package app.unison.diagnostics

import app.unison.policy.PolicyDefaults
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local

class JournalExportTest {

    private val generated = Instant.parse("2026-11-17T19:48:02.123Z")

    @Test
    fun headerCarriesTheVersionsTheDeviceAndTheSettings() {
        val text = JournalExport.header(info())

        assertTrue(text.startsWith("unison application log\n"))
        assertTrue(text.contains("generated: 2026-11-17T19:48:02.123Z\n"))
        assertTrue(text.contains("app: 0.1.0\n"))
        assertTrue(text.contains("core: 0.1.0\n"))
        assertTrue(text.contains("os: Android 15 (API 35)\n"))
        assertTrue(text.contains("device: Pixel (3f2a)\n"))
        assertTrue(text.contains("network: online (unmetered)\n"))
        assertTrue(text.contains("addresses: 192.168.1.2:42000, [::1]:42000\n"))
        assertTrue(text.contains("settings:\n"))
        assertTrue(text.contains("  device_name: Pixel\n"))
        assertTrue(text.contains("  require_confirmation: true\n"))
        assertTrue(text.contains("  require_file_confirmation: false\n"))
        assertTrue(text.contains("  blob_max_size_bytes: 1048576\n"))
        assertTrue(text.contains("  downloads_dir: [path]\n"))
        assertTrue(text.contains("  relay_max_size_bytes: 1048576\n"))
        assertTrue(text.contains("  history_ttl_text_seconds: 3600\n"))
        assertTrue(text.contains("  history_ttl_image_seconds: 7200\n"))
        assertTrue(text.contains("  history_ttl_files_seconds: 10800\n"))
        assertTrue(text.contains("  sentry_enabled: false\n"))
        assertTrue(text.contains("  strip_tracking: true\n"))
        assertTrue(text.contains("  journal_enabled: true\n"))
        assertTrue(text.contains("  relay.url: https://relay.example.com:443\n"))
        assertTrue(text.contains("  relay.token_set: true\n"))
        assertTrue(text.contains("  relay.connected: true\n"))
        assertTrue(text.contains("  relay.transport: quic\n"))
        assertTrue(text.contains("  relay.files_on_request: true\n"))
        assertTrue(text.endsWith("---\n"))
    }

    @Test
    fun headerCarriesTheWholePolicyTable() {
        val text = JournalExport.header(info())

        for (condition in listOf("unmetered", "metered", "roaming", "data_saver")) {
            for (kind in listOf("text", "image", "files")) {
                assertTrue("$condition.$kind action", text.contains("  policy.$condition.$kind.action: "))
                assertTrue("$condition.$kind size", text.contains("  policy.$condition.$kind.max_size_bytes: "))
            }
            assertTrue("$condition relay", text.contains("  policy.$condition.relay: "))
        }
        assertTrue(text.contains("  policy.unmetered.text.action: auto\n"))
        assertTrue(text.contains("  policy.data_saver.relay: false\n"))
    }

    @Test
    fun emptyAddressesAreOmitted() {
        val text = JournalExport.header(info().copy(addresses = emptyList()))

        assertFalse(text.contains("addresses:"))
    }

    @Test
    fun offlineNetworkHasNoCondition() {
        val text = JournalExport.header(info().copy(online = false, condition = null))

        assertTrue(text.contains("network: offline\n"))
    }

    @Test
    fun exportAppendsTheRecordsAfterTheHeader() {
        val records = listOf(
            JournalFormat.parse("2026-11-17T19:48:02.100Z INFO service created")!!,
            JournalFormat.parse("2026-11-17T19:48:02.150Z DEBUG a detail")!!,
        )

        val text = JournalExport.text(info(), records)

        assertTrue(text.startsWith("unison application log\n"))
        assertTrue(text.endsWith("---\n" +
            "2026-11-17T19:48:02.100Z INFO service created\n" +
            "2026-11-17T19:48:02.150Z DEBUG a detail\n"))
    }

    @Test
    fun emptyRelayIsReportedOff() {
        val text = JournalExport.header(info().copy(relay = Local.Relay.getDefaultInstance()))

        assertTrue(text.contains("  relay.url: \n"))
        assertTrue(text.contains("  relay.token_set: false\n"))
        assertTrue(text.contains("  relay.connected: false\n"))
        assertTrue(text.contains("  relay.transport: none\n"))
        assertTrue(text.contains("  relay.files_on_request: false\n"))
    }

    @Test
    fun relayUrlDropsUserInfoAndQuery() {
        assertEquals(
            "https://relay.example.com:443",
            JournalExport.relayUrl("https://user:secret@relay.example.com:443?token=abc"),
        )
        assertEquals(
            "relay.example.com:443",
            JournalExport.relayUrl("user:secret@relay.example.com:443"),
        )
        assertEquals(
            "https://relay.example.com:443/path",
            JournalExport.relayUrl("https://user@relay.example.com:443/path?token=abc"),
        )
    }

    @Test
    fun headerNeverCarriesTheRelayToken() {
        val relay = relay().toBuilder()
            .setUrl("https://user:secret@relay.example.com:443?token=abc")
            .build()

        val text = JournalExport.header(info().copy(relay = relay))

        assertFalse(text.contains("secret"))
        assertFalse(text.contains("token=abc"))
        assertTrue(text.contains("  relay.token_set: true\n"))
    }

    @Test
    fun headerReportsTheMarkerWhenTheSettingsDoNotKnowYet() {
        val settings = settings().toBuilder().setJournalEnabled(false).build()

        val text = JournalExport.header(info(settings).copy(journalEnabled = true))

        assertTrue(text.contains("  journal_enabled: true\n"))
    }

    @Test
    fun headerNeverCarriesTheDownloadsFolder() {
        val settings = settings().toBuilder()
            .setDownloadsDir("/data/user/0/app.unison/files/Downloads")
            .build()

        val text = JournalExport.header(info(settings))

        assertFalse(text.contains("/data/user"))
        assertTrue(text.contains("  downloads_dir: [path]\n"))
    }

    private fun info(settings: Local.Settings = settings()) = JournalHeaderInfo(
        generated = generated,
        appVersion = "0.1.0",
        coreVersion = "0.1.0",
        osRelease = "15",
        sdk = 35,
        deviceName = "Pixel",
        deviceId = "3f2a",
        online = true,
        condition = "unmetered",
        addresses = listOf("192.168.1.2:42000", "[::1]:42000"),
        settings = settings,
        journalEnabled = settings.journalEnabled,
        relay = relay(),
        policy = PolicyDefaults.config,
    )

    private fun relay(): Local.Relay = Local.Relay.newBuilder()
        .setUrl("https://relay.example.com:443")
        .setTokenSet(true)
        .setConnected(true)
        .setTransport(Local.RelayTransport.RELAY_TRANSPORT_QUIC)
        .setFilesOnRequest(true)
        .build()

    private fun settings(): Local.Settings = Local.Settings.newBuilder()
        .setDeviceName("Pixel")
        .setRequireConfirmation(true)
        .setRequireFileConfirmation(false)
        .setBlobMaxSizeBytes(1L shl 20)
        .setDownloadsDir("/data/user/0/app.unison/files/Downloads")
        .setRelayMaxSizeBytes(1L shl 20)
        .setHistoryTtlTextSeconds(3600)
        .setHistoryTtlImageSeconds(7200)
        .setHistoryTtlFilesSeconds(10800)
        .setSentryEnabled(false)
        .setStripTracking(true)
        .setJournalEnabled(true)
        .build()
}
