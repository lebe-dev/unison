package app.unison.settingsfile

import app.unison.service.NotificationSettings
import app.unison.service.OperatingMode
import org.json.JSONObject
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.util.Locale

/** Local settings of the app carried by the app section of a settings document. */
internal data class AppSection(
    val operatingMode: OperatingMode? = null,
    val notifications: NotificationPatch? = null,
)

/**
 * Notification categories of a document. An absent category keeps the stored
 * value, which keeps a hand edited file and a section of another version usable.
 */
internal data class NotificationPatch(
    val transfers: Boolean? = null,
    val requests: Boolean? = null,
    val clipboard: Boolean? = null,
    val confirmations: Boolean? = null,
    val hideStatusAfterDismissal: Boolean? = null,
    val statusDetails: Boolean? = null,
) {
    /** The stored settings with the categories of the document applied. */
    fun applyTo(current: NotificationSettings): NotificationSettings =
        current.copy(
            transfers = transfers ?: current.transfers,
            requests = requests ?: current.requests,
            clipboard = clipboard ?: current.clipboard,
            confirmations = confirmations ?: current.confirmations,
            hideStatusAfterDismissal = hideStatusAfterDismissal ?: current.hideStatusAfterDismissal,
            statusDetails = statusDetails ?: current.statusDetails,
        )
}

/** The JSON document of the settings the user keeps or restores. */
internal object SettingsFile {
    /** Type of the document the system picker shows. */
    const val MIME = "application/json"

    private const val PLATFORM = "android"

    private val NAME_TIME: DateTimeFormatter =
        DateTimeFormatter.ofPattern("yyyyMMdd-HHmmss", Locale.ROOT).withZone(ZoneOffset.UTC)

    /** Name of an export file. It matches the name the core produces. */
    fun fileName(now: Instant = Instant.now()): String = "unison-$PLATFORM-${NAME_TIME.format(now)}.json"

    /** The app section of a document this app exports. */
    fun appSection(
        operatingMode: OperatingMode,
        notifications: NotificationSettings,
    ): String =
        JSONObject()
            .put(
                PLATFORM,
                JSONObject()
                    .put("operating_mode", operatingMode.value)
                    .put(
                        "notifications",
                        JSONObject()
                            .put("transfers", notifications.transfers)
                            .put("requests", notifications.requests)
                            .put("clipboard", notifications.clipboard)
                            .put("confirmations", notifications.confirmations)
                            .put("hide_status_after_dismissal", notifications.hideStatusAfterDismissal)
                            .put("status_details", notifications.statusDetails),
                    ),
            ).toString()

    /** The settings of the app section. A document without a readable section
     * yields no settings, so an import from another platform and a hand edited
     * file stay usable. */
    fun appSectionOf(text: String): AppSection {
        val document = runCatching { JSONObject(text) }.getOrNull() ?: return AppSection()
        val section = document.optJSONObject("app")?.optJSONObject(PLATFORM) ?: return AppSection()

        val mode =
            section
                .optString("operating_mode")
                .takeIf { it.isNotEmpty() }
                ?.let { value -> OperatingMode.entries.firstOrNull { it.value == value } }
        val categories = section.optJSONObject("notifications")?.let(::notificationPatch)
        return AppSection(operatingMode = mode, notifications = categories)
    }

    /** notificationPatch reads the categories the object carries. */
    private fun notificationPatch(categories: JSONObject): NotificationPatch =
        NotificationPatch(
            transfers = boolOrNull(categories, "transfers"),
            requests = boolOrNull(categories, "requests"),
            clipboard = boolOrNull(categories, "clipboard"),
            confirmations = boolOrNull(categories, "confirmations"),
            hideStatusAfterDismissal = boolOrNull(categories, "hide_status_after_dismissal"),
            statusDetails = boolOrNull(categories, "status_details"),
        )

    /** boolOrNull returns a flag, or null when the object does not carry it as a
     * boolean. */
    private fun boolOrNull(
        values: JSONObject,
        key: String,
    ): Boolean? {
        if (!values.has(key)) {
            return null
        }
        return (values.opt(key) as? Boolean)
    }
}
