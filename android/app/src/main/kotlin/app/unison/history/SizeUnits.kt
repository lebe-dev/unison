package app.unison.history

import android.content.Context
import app.unison.R
import java.util.Locale

/** Unit names and number format of a human readable size. */
data class SizeUnits(
    val bytes: String,
    val names: List<String>,
    val locale: Locale,
) {
    companion object {
        /** Units of the locale the resource files are written in. */
        val ENGLISH = SizeUnits("B", listOf("KB", "MB", "GB", "TB"), Locale.US)

        /** Units of the current locale of the given context. */
        fun of(context: Context): SizeUnits {
            val resources = context.resources
            val language = resources.configuration.locales[0].language
            return SizeUnits(
                bytes = resources.getString(R.string.unit_bytes),
                names = listOf(
                    resources.getString(R.string.unit_kilobytes),
                    resources.getString(R.string.unit_megabytes),
                    resources.getString(R.string.unit_gigabytes),
                    resources.getString(R.string.unit_terabytes),
                ),
                locale = if (language == RUSSIAN) Locale.forLanguageTag(RUSSIAN) else Locale.ENGLISH,
            )
        }

        private const val RUSSIAN = "ru"
    }
}
