package app.unison.pairing

/** Pairing codes that another app, such as the system camera, opens as a link. */
object PairingLink {
    /** Action of an intent that opens a link. */
    const val ACTION_VIEW = "android.intent.action.VIEW"

    private const val PREFIX = "unison://pair?"

    /** The pairing code of an intent that opens one, or null for any other intent. */
    fun code(action: String?, data: String?): String? {
        if (action != ACTION_VIEW || data == null) {
            return null
        }
        if (!data.startsWith(PREFIX, ignoreCase = true)) {
            return null
        }
        return data
    }
}
