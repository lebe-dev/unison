package app.unison.pairing

import unison.local.v1.Local

/**
 * Pairing codes that another app, such as the system camera, opens as a link.
 *
 * Any app or web page can open such a link, so the code only reaches the core
 * after the user confirms it in the app.
 */
object PairingLink {
    /** Action of an intent that opens a link. */
    const val ACTION_VIEW = "android.intent.action.VIEW"

    private const val PREFIX = "unison://pair?"
    private const val ROLE_PARAM = "role"

    /** The pairing code of an intent that opens one, or null for any other intent. */
    fun code(
        action: String?,
        data: String?,
    ): String? {
        if (action != ACTION_VIEW || data == null) {
            return null
        }
        if (!data.startsWith(PREFIX, ignoreCase = true)) {
            return null
        }
        return data
    }

    /** Role the device of the code proposes, unspecified when the code names none. */
    fun role(uri: String): Local.Role {
        val query = uri.substringAfter('?', missingDelimiterValue = "")
        val value =
            query
                .split('&')
                .firstOrNull { it.substringBefore('=') == ROLE_PARAM }
                ?.substringAfter('=', missingDelimiterValue = "")
        return when (value) {
            "personal" -> Local.Role.ROLE_PERSONAL
            "friends" -> Local.Role.ROLE_FRIENDS
            else -> Local.Role.ROLE_UNSPECIFIED
        }
    }
}
