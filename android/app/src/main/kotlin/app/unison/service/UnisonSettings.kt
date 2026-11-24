package app.unison.service

import android.content.Context
import android.os.Build
import androidx.core.content.edit
import app.unison.ui.state.DashboardLimit

/** How long the core stays running. */
enum class OperatingMode(
    val value: String,
) {
    /** The core runs from boot until the user stops it. */
    ALWAYS("always"),

    /** The core runs while the device is on Wi-Fi. */
    WIFI_ONLY("wifi_only"),

    /** The core runs only while the app is in the foreground. */
    APP_OPEN("app_open"),
    ;

    companion object {
        val DEFAULT = ALWAYS

        fun fromValue(value: String?): OperatingMode = entries.firstOrNull { it.value == value } ?: DEFAULT
    }
}

/** Settings the service needs before the core is up. */
object UnisonSettings {
    const val FILE = "unison.settings"

    /** Stored [OperatingMode.value]. */
    const val KEY_OPERATING_MODE = "service.operating_mode"

    /** Device name passed to the core at start. */
    const val KEY_DEVICE_NAME = "device.name"

    /** Hash of the last clipboard content this device sent or applied. */
    const val KEY_LAST_CLIP = "clipboard.last_hash"

    /** URI of the folder the user picked for received files, absent for the default one. */
    const val KEY_DOWNLOADS_TREE = "downloads.tree"

    /** Set once the first run screen was completed. */
    const val KEY_ONBOARDED = "onboarding.done"

    /** Set by the user to report errors to the developer. */
    const val KEY_SENTRY_ENABLED = "diagnostics.sentry_enabled"

    /** Notifications of the transfers; see [NotificationSettings]. */
    const val KEY_NOTIFICATIONS_TRANSFERS = "notifications.transfers"

    /** Notifications of the file offers. */
    const val KEY_NOTIFICATIONS_REQUESTS = "notifications.requests"

    /** Notifications of the clipboard items. */
    const val KEY_NOTIFICATIONS_CLIPBOARD = "notifications.clipboard"

    /** Notifications of the clipboard items that wait for an apply. */
    const val KEY_NOTIFICATIONS_CONFIRMATIONS = "notifications.confirmations"

    /** Keep the status notification hidden after the user dismisses it. */
    const val KEY_NOTIFICATIONS_HIDE_STATUS = "notifications.hide_status"

    /** Show the device name and the online devices in the status notification. */
    const val KEY_NOTIFICATIONS_STATUS_DETAILS = "notifications.status_details"

    /** Number of newest items each tab of the dashboard shows. */
    const val KEY_DASHBOARD_LIMIT = "dashboard.limit"

    /** True when the app looks for a new release in the background. */
    const val KEY_UPDATES_ENABLED = "updates.enabled"

    /** True when the release channel includes the pre-releases of GitHub. */
    const val KEY_UPDATES_BETA_CHANNEL = "updates.beta_channel"

    /** How often the background check runs. */
    const val KEY_UPDATES_INTERVAL = "updates.interval"

    /** Time of the last finished check in unix milliseconds. */
    const val KEY_UPDATES_LAST_CHECKED_MS = "updates.last_checked_ms"

    /** Version the user put away in the dashboard banner. */
    const val KEY_UPDATES_DISMISSED_VERSION = "updates.dismissed_version"

    /** Entity tag of the last answer of GitHub. */
    const val KEY_UPDATES_LAST_ETAG = "updates.last_etag"

    /** Release of the last background check, so the banner opens without a request. */
    const val KEY_UPDATES_CACHED_RELEASE = "updates.cached_release_json"

    fun operatingMode(context: Context): OperatingMode {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return OperatingMode.fromValue(prefs.getString(KEY_OPERATING_MODE, null))
    }

    fun setOperatingMode(
        context: Context,
        mode: OperatingMode,
    ) {
        context
            .getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putString(KEY_OPERATING_MODE, mode.value) }
    }

    /** Name of this device, always within the limit of the core. */
    fun deviceName(context: Context): String {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        val stored = prefs.getString(KEY_DEVICE_NAME, null)?.let(DeviceNames::fit)
        return stored?.ifEmpty { null } ?: DeviceNames.fit(Build.MODEL.orEmpty())
    }

    fun setDeviceName(
        context: Context,
        name: String,
    ) {
        context
            .getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putString(KEY_DEVICE_NAME, DeviceNames.fit(name)) }
    }

    /** Hash of the content this device sent or applied last, null when there was none. */
    fun lastClipHash(context: Context): String? {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getString(KEY_LAST_CLIP, null)
    }

    fun setLastClipHash(
        context: Context,
        hash: String,
    ) {
        context
            .getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putString(KEY_LAST_CLIP, hash) }
    }

    /** URI of the folder chosen for received files, null for the default folder. */
    fun downloadsTree(context: Context): String? {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getString(KEY_DOWNLOADS_TREE, null)
    }

    fun setDownloadsTree(
        context: Context,
        uri: String?,
    ) {
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE).edit {
            if (uri == null) remove(KEY_DOWNLOADS_TREE) else putString(KEY_DOWNLOADS_TREE, uri)
        }
    }

    /** True when the user already saw the first run screen. */
    fun onboarded(context: Context): Boolean {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getBoolean(KEY_ONBOARDED, false)
    }

    fun setOnboarded(
        context: Context,
        done: Boolean,
    ) {
        context
            .getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putBoolean(KEY_ONBOARDED, done) }
    }

    /** True when the user allows the app to send errors to the developer. */
    fun sentryEnabled(context: Context): Boolean {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getBoolean(KEY_SENTRY_ENABLED, false)
    }

    fun setSentryEnabled(
        context: Context,
        value: Boolean,
    ) {
        context
            .getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putBoolean(KEY_SENTRY_ENABLED, value) }
    }

    /** Number of newest items each tab of the dashboard shows. */
    fun dashboardLimit(context: Context): Int {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return DashboardLimit.normalize(prefs.getInt(KEY_DASHBOARD_LIMIT, DashboardLimit.DEFAULT))
    }

    fun setDashboardLimit(
        context: Context,
        value: Int,
    ) {
        context
            .getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putInt(KEY_DASHBOARD_LIMIT, DashboardLimit.normalize(value)) }
    }

    /** True when the app looks for a new release in the background. */
    fun updatesEnabled(context: Context): Boolean {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getBoolean(KEY_UPDATES_ENABLED, false)
    }

    fun setUpdatesEnabled(
        context: Context,
        value: Boolean,
    ) {
        context
            .getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putBoolean(KEY_UPDATES_ENABLED, value) }
    }

    /** True when the release channel includes the pre-releases of GitHub. */
    fun updatesBetaChannel(context: Context): Boolean {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getBoolean(KEY_UPDATES_BETA_CHANNEL, false)
    }

    fun setUpdatesBetaChannel(
        context: Context,
        value: Boolean,
    ) {
        context
            .getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putBoolean(KEY_UPDATES_BETA_CHANNEL, value) }
    }

    /** Stored [UpdateCheckInterval][app.unison.updater.UpdateCheckInterval] value. */
    fun updatesInterval(context: Context): String? {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getString(KEY_UPDATES_INTERVAL, null)
    }

    fun setUpdatesInterval(
        context: Context,
        value: String,
    ) {
        context
            .getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putString(KEY_UPDATES_INTERVAL, value) }
    }

    /** Time of the last finished check in unix milliseconds. */
    fun updatesLastCheckedMs(context: Context): Long {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getLong(KEY_UPDATES_LAST_CHECKED_MS, 0L)
    }

    fun setUpdatesLastCheckedMs(
        context: Context,
        value: Long,
    ) {
        context
            .getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putLong(KEY_UPDATES_LAST_CHECKED_MS, value) }
    }

    /** Version the user put away in the banner, null when none was put away. */
    fun updatesDismissedVersion(context: Context): String? {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getString(KEY_UPDATES_DISMISSED_VERSION, null)
    }

    fun setUpdatesDismissedVersion(
        context: Context,
        value: String?,
    ) {
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE).edit {
            if (value == null) remove(KEY_UPDATES_DISMISSED_VERSION) else putString(KEY_UPDATES_DISMISSED_VERSION, value)
        }
    }

    /** Entity tag of the last answer of GitHub, null when there was none. */
    fun updatesLastEtag(context: Context): String? {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getString(KEY_UPDATES_LAST_ETAG, null)
    }

    fun setUpdatesLastEtag(
        context: Context,
        value: String?,
    ) {
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE).edit {
            if (value == null) remove(KEY_UPDATES_LAST_ETAG) else putString(KEY_UPDATES_LAST_ETAG, value)
        }
    }

    /** Release of the last background check in JSON, null when there was none. */
    fun updatesCachedReleaseJson(context: Context): String? {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getString(KEY_UPDATES_CACHED_RELEASE, null)
    }

    fun setUpdatesCachedReleaseJson(
        context: Context,
        value: String?,
    ) {
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE).edit {
            if (value == null) remove(KEY_UPDATES_CACHED_RELEASE) else putString(KEY_UPDATES_CACHED_RELEASE, value)
        }
    }
}
