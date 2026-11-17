package app.unison.service

import android.content.Context
import android.os.Build
import androidx.core.content.edit

/** How long the core stays running. */
enum class OperatingMode(val value: String) {
    /** The core runs from boot until the user stops it. */
    ALWAYS("always"),

    /** The core runs while the device is on Wi-Fi. */
    WIFI_ONLY("wifi_only"),

    /** The core runs only while the app is in the foreground. */
    APP_OPEN("app_open");

    companion object {
        val DEFAULT = ALWAYS

        fun fromValue(value: String?): OperatingMode =
            entries.firstOrNull { it.value == value } ?: DEFAULT
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

    fun operatingMode(context: Context): OperatingMode {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return OperatingMode.fromValue(prefs.getString(KEY_OPERATING_MODE, null))
    }

    fun setOperatingMode(context: Context, mode: OperatingMode) {
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putString(KEY_OPERATING_MODE, mode.value) }
    }

    fun deviceName(context: Context): String {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getString(KEY_DEVICE_NAME, null) ?: Build.MODEL
    }

    fun setDeviceName(context: Context, name: String) {
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putString(KEY_DEVICE_NAME, name) }
    }

    /** Hash of the content this device sent or applied last, null when there was none. */
    fun lastClipHash(context: Context): String? {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getString(KEY_LAST_CLIP, null)
    }

    fun setLastClipHash(context: Context, hash: String) {
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putString(KEY_LAST_CLIP, hash) }
    }

    /** URI of the folder chosen for received files, null for the default folder. */
    fun downloadsTree(context: Context): String? {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getString(KEY_DOWNLOADS_TREE, null)
    }

    fun setDownloadsTree(context: Context, uri: String?) {
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE).edit {
            if (uri == null) remove(KEY_DOWNLOADS_TREE) else putString(KEY_DOWNLOADS_TREE, uri)
        }
    }

    /** True when the user already saw the first run screen. */
    fun onboarded(context: Context): Boolean {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        return prefs.getBoolean(KEY_ONBOARDED, false)
    }

    fun setOnboarded(context: Context, done: Boolean) {
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
            .edit { putBoolean(KEY_ONBOARDED, done) }
    }
}
