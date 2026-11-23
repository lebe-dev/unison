package app.unison.ui

import androidx.annotation.DrawableRes
import androidx.annotation.StringRes
import app.unison.R

/**
 * Top level destinations of the app. Pairing is a task started from the
 * devices, so it opens over the whole screen instead of sitting in the bar.
 */
enum class Destination(
    @StringRes val label: Int,
    @DrawableRes val icon: Int,
    val inBar: Boolean = true,
) {
    DEVICES(R.string.nav_devices, R.drawable.ic_nav_devices),
    ADD(R.string.nav_add, R.drawable.ic_nav_add, inBar = false),
    REQUESTS(R.string.nav_requests, R.drawable.ic_nav_requests),
    HISTORY(R.string.nav_history, R.drawable.ic_nav_history),
    SETTINGS(R.string.nav_settings, R.drawable.ic_nav_settings),
    ;

    companion object {
        val DEFAULT = DEVICES

        /** Destination of a screen name sent by a notification, or null. */
        fun ofScreen(name: String?): Destination? =
            when (name) {
                SCREEN_REQUESTS -> REQUESTS
                SCREEN_HISTORY -> HISTORY
                else -> null
            }

        /** Name of the requests screen in a notification intent. */
        const val SCREEN_REQUESTS = "requests"

        /** Name of the history screen in a notification intent. */
        const val SCREEN_HISTORY = "history"
    }
}
