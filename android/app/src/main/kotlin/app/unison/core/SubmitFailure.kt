package app.unison.core

/**
 * Reason the facade gave for refusing content handed to it with submitClip.
 * The facade reports every failure as an exception that carries the message of
 * the core, so the reason is read from that message.
 */
enum class SubmitFailure {
    /** The clipboard synchronization is paused. */
    PAUSED,

    /** The core does not take content of this type. */
    UNSUPPORTED,

    /** The core is not running. */
    NOT_RUNNING,

    /** Any other failure. */
    OTHER,
    ;

    companion object {
        /** Parts of the messages of clip.ErrPaused, clip.ErrUnsupportedContent and the facade. */
        private const val PAUSED_MARK = "synchronization is paused"
        private const val UNSUPPORTED_MARK = "unsupported content"
        private const val NOT_RUNNING_MARK = "core is not running"

        /** Reason of a failed submit. */
        fun of(error: Throwable): SubmitFailure {
            val message = error.message.orEmpty()
            return when {
                message.contains(PAUSED_MARK) -> PAUSED
                message.contains(UNSUPPORTED_MARK) -> UNSUPPORTED
                message.contains(NOT_RUNNING_MARK) -> NOT_RUNNING
                else -> OTHER
            }
        }
    }
}
