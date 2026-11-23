package app.unison.diagnostics

/** Fixed text that replaces an absolute path. */
private const val PLACEHOLDER = "[path]"

/** One character of a path, without the punctuation that usually ends it in a message. */
private const val PATH_CHAR = """[^\s"'<>|,;:()\[\]{}]"""

/**
 * Absolute paths of the platforms the app runs on. A token boundary keeps the
 * paths out of URLs and ordinary words. The spaces inside a path keep the match
 * going while the next part holds a separator.
 */
private val PATHS =
    Regex(
        """(?<![\w./:\\-])(?:""" +
            """(?:/Users|/home|/root|/tmp|/private|/var|/opt|/mnt|/data|/storage)/$PATH_CHAR+""" +
            """|[A-Za-z]:[\\/]$PATH_CHAR+)""" +
            """(?:\s+(?!\S*://)$PATH_CHAR*[\\/]$PATH_CHAR*)*""",
    )

/** Replaces the absolute paths that a message carries with [PLACEHOLDER]. */
internal fun redactPaths(value: String): String = PATHS.replace(value, PLACEHOLDER)
