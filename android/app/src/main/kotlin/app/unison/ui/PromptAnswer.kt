package app.unison.ui

/** Answer to a question of [PromptDialog]. */
sealed interface PromptAnswer {
    /** Writes a held clipboard item to the clipboard. */
    data object Apply : PromptAnswer

    /** Lets a held clipboard item go; it stays in the history. */
    data object Skip : PromptAnswer

    /** Accepts a file offer. */
    data object Accept : PromptAnswer

    /** Declines a file offer. */
    data object Decline : PromptAnswer

    /** Accepts an introduction and optionally turns the Friends search on. */
    data class AcceptMediated(
        val enableFriendsDiscovery: Boolean,
    ) : PromptAnswer

    /** Declines an introduction. */
    data object DeclineMediated : PromptAnswer
}
