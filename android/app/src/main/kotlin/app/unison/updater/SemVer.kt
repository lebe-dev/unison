package app.unison.updater

/**
 * A semantic version with the precedence rules of SemVer 2.0.0. A version
 * without a pre-release is newer than any pre-release of the same numbers, and
 * build metadata does not take part in the comparison.
 */
class SemVer private constructor(
    val major: Long,
    val minor: Long,
    val patch: Long,
    private val preRelease: List<Identifier>,
) : Comparable<SemVer> {
    /** True when the version names an alpha, beta, release candidate or dev build. */
    val isPreRelease: Boolean
        get() = preRelease.isNotEmpty()

    /** The version without a leading v and without build metadata. */
    val text: String by lazy {
        buildString {
            append(major)
                .append('.')
                .append(minor)
                .append('.')
                .append(patch)
            if (preRelease.isNotEmpty()) {
                append('-')
                append(preRelease.joinToString(".") { it.text })
            }
        }
    }

    override fun compareTo(other: SemVer): Int {
        if (major != other.major) {
            return major.compareTo(other.major)
        }
        if (minor != other.minor) {
            return minor.compareTo(other.minor)
        }
        if (patch != other.patch) {
            return patch.compareTo(other.patch)
        }
        if (preRelease.isEmpty() || other.preRelease.isEmpty()) {
            return when {
                preRelease.isEmpty() && other.preRelease.isEmpty() -> 0
                preRelease.isEmpty() -> 1
                else -> -1
            }
        }
        for (index in 0 until minOf(preRelease.size, other.preRelease.size)) {
            val order = preRelease[index].compareTo(other.preRelease[index])
            if (order != 0) {
                return order
            }
        }
        return preRelease.size.compareTo(other.preRelease.size)
    }

    override fun equals(other: Any?): Boolean =
        other is SemVer &&
            major == other.major &&
            minor == other.minor &&
            patch == other.patch &&
            preRelease == other.preRelease

    override fun hashCode(): Int = listOf(major, minor, patch, preRelease).hashCode()

    override fun toString(): String = text

    /** One identifier of the pre-release part. */
    private sealed interface Identifier : Comparable<Identifier> {
        val text: String

        /** A number, compared as an integer so beta.10 is newer than beta.2. */
        data class Numeric(
            val value: Long,
        ) : Identifier {
            override val text: String
                get() = value.toString()

            override fun compareTo(other: Identifier): Int =
                when (other) {
                    is Numeric -> value.compareTo(other.value)
                    is Alpha -> -1
                }
        }

        /** A word, compared lexicographically and newer than a number. */
        data class Alpha(
            val value: String,
        ) : Identifier {
            override val text: String
                get() = value

            override fun compareTo(other: Identifier): Int =
                when (other) {
                    is Numeric -> 1
                    is Alpha -> value.compareTo(other.value)
                }
        }
    }

    companion object {
        /** Parses a version, returning null when the text is not a version. */
        fun parse(value: String): SemVer? {
            var text = value.trim()
            if (text.startsWith("v")) {
                text = text.substring(1)
            }
            val build = text.indexOf('+')
            if (build >= 0) {
                text = text.substring(0, build)
            }
            val dash = text.indexOf('-')
            val core = if (dash >= 0) text.substring(0, dash) else text
            val preText = if (dash >= 0) text.substring(dash + 1) else null

            val parts = core.split('.')
            if (parts.size != 3) {
                return null
            }
            val major = number(parts[0]) ?: return null
            val minor = number(parts[1]) ?: return null
            val patch = number(parts[2]) ?: return null
            val preRelease =
                if (preText == null) {
                    emptyList()
                } else {
                    preText.split('.').map { identifier(it) ?: return null }
                }
            return SemVer(major, minor, patch, preRelease)
        }

        /** Reads one numeric part of the version. */
        private fun number(part: String): Long? {
            if (part.isEmpty() || part.any { !it.isAsciiDigit() }) {
                return null
            }
            if (part.length > 1 && part.startsWith("0")) {
                return null
            }
            return part.toLongOrNull()
        }

        /** Reads one identifier of the pre-release part. */
        private fun identifier(part: String): Identifier? {
            if (part.isEmpty()) {
                return null
            }
            if (part.all { it.isAsciiDigit() }) {
                if (part.length > 1 && part.startsWith("0")) {
                    return null
                }
                return part.toLongOrNull()?.let(Identifier::Numeric)
            }
            if (part.any { !it.isAsciiLetterOrDigit() && it != '-' }) {
                return null
            }
            return Identifier.Alpha(part)
        }

        private fun Char.isAsciiDigit(): Boolean = this in '0'..'9'

        private fun Char.isAsciiLetterOrDigit(): Boolean = this in 'a'..'z' || this in 'A'..'Z' || isAsciiDigit()
    }
}
