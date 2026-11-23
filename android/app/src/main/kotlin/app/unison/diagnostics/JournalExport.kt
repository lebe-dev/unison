package app.unison.diagnostics

import android.content.ClipData
import android.content.Context
import android.content.Intent
import android.os.Build
import androidx.core.content.FileProvider
import app.unison.BuildConfig
import app.unison.R
import app.unison.core.CoreClient
import app.unison.policy.PolicyCondition
import app.unison.policy.PolicyConfig
import app.unison.policy.PolicyKind
import app.unison.policy.conditionOf
import unison.local.v1.Local
import java.io.File
import java.io.IOException
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.util.Locale

/** Values the header of an export carries. */
internal data class JournalHeaderInfo(
    val generated: Instant,
    val appVersion: String,
    val coreVersion: String,
    val osRelease: String,
    val sdk: Int,
    val deviceName: String,
    val deviceId: String,
    val online: Boolean,
    val condition: String?,
    val addresses: List<String>,
    val settings: Local.Settings,
    val journalEnabled: Boolean,
    val relay: Local.Relay,
    val policy: PolicyConfig,
)

/** The text of the application log that the user copies or shares. */
object JournalExport {
    /** Directory of the export files inside the cache. */
    const val DIRECTORY = "exports"

    /** Type of the exported file. */
    const val MIME = "text/plain"

    private const val AUTHORITY_SUFFIX = ".files"

    private val NAME_TIME: DateTimeFormatter =
        DateTimeFormatter.ofPattern("yyyyMMdd-HHmmss", Locale.ROOT).withZone(ZoneOffset.UTC)

    /** Header and every record, including the debug ones. */
    fun text(
        context: Context,
        status: Local.GetStatusResponse?,
        settings: Local.Settings,
        policy: PolicyConfig,
        relay: Local.Relay,
        now: Instant = Instant.now(),
    ): String {
        val info =
            JournalHeaderInfo(
                generated = now,
                appVersion = BuildConfig.VERSION_NAME,
                coreVersion = status?.version?.takeIf { it.isNotEmpty() } ?: coreVersion(),
                osRelease = Build.VERSION.RELEASE.orEmpty(),
                sdk = Build.VERSION.SDK_INT,
                deviceName = status?.deviceName.orEmpty().ifEmpty { settings.deviceName },
                deviceId = status?.deviceId.orEmpty(),
                online = status?.network?.online == true,
                condition = conditionOf(status?.network)?.name?.lowercase(Locale.ROOT),
                addresses = status?.addrsList.orEmpty(),
                settings = settings,
                // The marker keeps the truth while the core does not answer.
                journalEnabled = settings.journalEnabled || Journal.enabled(),
                relay = relay,
                policy = policy,
            )
        return text(info, Journal.records(context))
    }

    /** Writes the text to the cache and returns the file. */
    fun write(
        context: Context,
        text: String,
        now: Instant = Instant.now(),
    ): File {
        val directory = File(context.cacheDir, DIRECTORY)
        if (!directory.isDirectory && !directory.mkdirs()) {
            throw IOException("cannot create the export directory")
        }
        val file = File(directory, "unison-log-${NAME_TIME.format(now)}.log")
        file.writeText(text)
        return file
    }

    /** Chooser that hands the file to another app. */
    fun shareIntent(
        context: Context,
        file: File,
    ): Intent {
        val uri = FileProvider.getUriForFile(context, context.packageName + AUTHORITY_SUFFIX, file)
        val send =
            Intent(Intent.ACTION_SEND).apply {
                type = MIME
                putExtra(Intent.EXTRA_STREAM, uri)
                clipData = ClipData.newRawUri(null, uri)
                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            }
        return Intent.createChooser(send, context.getString(R.string.journal_share))
    }

    /** Version of the bound core, empty when the library does not answer. */
    private fun coreVersion(): String =
        try {
            CoreClient().version
        } catch (_: Throwable) {
            ""
        }

    /** Header followed by the records. */
    internal fun text(
        info: JournalHeaderInfo,
        records: List<JournalRecord>,
    ): String =
        buildString {
            append(header(info))
            for (record in records) {
                append(record.text)
                append('\n')
            }
        }

    /** Header of the export, without secrets and absolute paths. */
    internal fun header(info: JournalHeaderInfo): String =
        buildString {
            appendLine("unison application log")
            appendLine("generated: ${JournalFormat.time(info.generated)}")
            appendLine("app: ${info.appVersion}")
            appendLine("core: ${info.coreVersion}")
            appendLine("os: Android ${info.osRelease} (API ${info.sdk})")
            appendLine("device: ${info.deviceName} (${info.deviceId})")
            append(
                when {
                    !info.online -> "network: offline\n"
                    info.condition != null -> "network: online (${info.condition})\n"
                    else -> "network: online\n"
                },
            )
            if (info.addresses.isNotEmpty()) {
                appendLine("addresses: ${info.addresses.joinToString(", ")}")
            }

            val settings = info.settings
            appendLine("settings:")
            appendLine("  device_name: ${settings.deviceName}")
            appendLine("  require_confirmation: ${settings.requireConfirmation}")
            appendLine("  require_file_confirmation: ${settings.requireFileConfirmation}")
            appendLine("  blob_max_size_bytes: ${settings.blobMaxSizeBytes}")
            appendLine("  downloads_dir: [path]")
            appendLine("  relay_max_size_bytes: ${settings.relayMaxSizeBytes}")
            appendLine("  history_ttl_text_seconds: ${settings.historyTtlTextSeconds}")
            appendLine("  history_ttl_image_seconds: ${settings.historyTtlImageSeconds}")
            appendLine("  history_ttl_files_seconds: ${settings.historyTtlFilesSeconds}")
            appendLine("  sentry_enabled: ${settings.sentryEnabled}")
            appendLine("  strip_tracking: ${settings.stripTracking}")
            appendLine("  journal_enabled: ${info.journalEnabled}")
            appendLine("  relay.url: ${relayUrl(info.relay.url)}")
            appendLine("  relay.token_set: ${info.relay.tokenSet}")
            appendLine("  relay.connected: ${info.relay.connected}")
            appendLine("  relay.transport: ${transportName(info.relay.transport)}")
            appendLine("  relay.files_on_request: ${info.relay.filesOnRequest}")
            for (condition in PolicyCondition.entries) {
                val name = condition.name.lowercase(Locale.ROOT)
                val row = info.policy[condition]
                for (kind in PolicyKind.entries) {
                    val cell = row[kind]
                    val field = "policy.$name.${kind.name.lowercase(Locale.ROOT)}"
                    appendLine("  $field.action: ${cell.mode.name.lowercase(Locale.ROOT)}")
                    appendLine("  $field.max_size_bytes: ${cell.maxSizeBytes}")
                }
                appendLine("  policy.$name.relay: ${row.relay}")
            }
            appendLine("---")
        }

    /** Relay address without the credentials that a pasted URL may carry. */
    internal fun relayUrl(url: String): String {
        val withoutQuery = url.trim().substringBefore('?')
        val schemeEnd = withoutQuery.indexOf("://")
        val authorityStart = if (schemeEnd >= 0) schemeEnd + 3 else 0
        val pathStart = withoutQuery.indexOf('/', authorityStart)
        val authorityEnd = if (pathStart >= 0) pathStart else withoutQuery.length
        val authority = withoutQuery.substring(authorityStart, authorityEnd)
        val at = authority.lastIndexOf('@')
        if (at < 0) {
            return withoutQuery
        }
        return withoutQuery.substring(0, authorityStart) +
            authority.substring(at + 1) +
            withoutQuery.substring(authorityEnd)
    }

    /** Name of the relay transport without the proto prefix. */
    private fun transportName(transport: Local.RelayTransport): String =
        when (transport) {
            Local.RelayTransport.RELAY_TRANSPORT_QUIC -> "quic"
            Local.RelayTransport.RELAY_TRANSPORT_WSS -> "wss"
            else -> "none"
        }
}
