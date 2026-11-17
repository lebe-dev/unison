package app.unison.service

import android.content.Context
import androidx.core.content.edit

/** Notifications the user lets the app show. */
data class NotificationSettings(
    /** Progress and result of the file transfers. */
    val transfers: Boolean = true,

    /** File offers of Friends devices that wait for an answer. */
    val requests: Boolean = true,

    /** Incoming clipboard items and the results of their downloads. */
    val clipboard: Boolean = true,

    /** Clipboard items that wait for the user to apply them. */
    val confirmations: Boolean = true,

    /** Keep the status notification hidden after the user dismisses it. */
    val hideStatusAfterDismissal: Boolean = true,

    /** Show the device name and the number of online devices in the status notification. */
    val statusDetails: Boolean = true,
) {
    companion object {
        /** Reads the stored settings, the defaults when none were saved. */
        fun load(context: Context): NotificationSettings {
            val defaults = NotificationSettings()
            val prefs = context.getSharedPreferences(UnisonSettings.FILE, Context.MODE_PRIVATE)
            return NotificationSettings(
                transfers = prefs.getBoolean(UnisonSettings.KEY_NOTIFICATIONS_TRANSFERS, defaults.transfers),
                requests = prefs.getBoolean(UnisonSettings.KEY_NOTIFICATIONS_REQUESTS, defaults.requests),
                clipboard = prefs.getBoolean(UnisonSettings.KEY_NOTIFICATIONS_CLIPBOARD, defaults.clipboard),
                confirmations = prefs.getBoolean(
                    UnisonSettings.KEY_NOTIFICATIONS_CONFIRMATIONS,
                    defaults.confirmations,
                ),
                hideStatusAfterDismissal = prefs.getBoolean(
                    UnisonSettings.KEY_NOTIFICATIONS_HIDE_STATUS,
                    defaults.hideStatusAfterDismissal,
                ),
                statusDetails = prefs.getBoolean(
                    UnisonSettings.KEY_NOTIFICATIONS_STATUS_DETAILS,
                    defaults.statusDetails,
                ),
            )
        }

        fun save(context: Context, settings: NotificationSettings) {
            context.getSharedPreferences(UnisonSettings.FILE, Context.MODE_PRIVATE).edit {
                putBoolean(UnisonSettings.KEY_NOTIFICATIONS_TRANSFERS, settings.transfers)
                putBoolean(UnisonSettings.KEY_NOTIFICATIONS_REQUESTS, settings.requests)
                putBoolean(UnisonSettings.KEY_NOTIFICATIONS_CLIPBOARD, settings.clipboard)
                putBoolean(UnisonSettings.KEY_NOTIFICATIONS_CONFIRMATIONS, settings.confirmations)
                putBoolean(UnisonSettings.KEY_NOTIFICATIONS_HIDE_STATUS, settings.hideStatusAfterDismissal)
                putBoolean(UnisonSettings.KEY_NOTIFICATIONS_STATUS_DETAILS, settings.statusDetails)
            }
        }
    }
}
