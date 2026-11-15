package app.unison.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext

private val LightColors = lightColorScheme(
    primary = Color(0xFF2C5F7C),
    onPrimary = Color(0xFFFFFFFF),
    primaryContainer = Color(0xFFC7E5F6),
    onPrimaryContainer = Color(0xFF0A3448),
    secondary = Color(0xFF4E616D),
    onSecondary = Color(0xFFFFFFFF),
    secondaryContainer = Color(0xFFD1E5F2),
    onSecondaryContainer = Color(0xFF0A3448),
    tertiary = Color(0xFF5F5C7A),
    onTertiary = Color(0xFFFFFFFF),
    surface = Color(0xFFF7FAFC),
    background = Color(0xFFF7FAFC),
    error = Color(0xFFB3261E),
)

private val DarkColors = darkColorScheme(
    primary = Color(0xFF93C9E4),
    onPrimary = Color(0xFF00344B),
    primaryContainer = Color(0xFF164B65),
    onPrimaryContainer = Color(0xFFC7E5F6),
    secondary = Color(0xFFB5C9D6),
    onSecondary = Color(0xFF20333E),
    secondaryContainer = Color(0xFF364955),
    onSecondaryContainer = Color(0xFFD1E5F2),
    tertiary = Color(0xFFC8C4E3),
    onTertiary = Color(0xFF303048),
    surface = Color(0xFF101416),
    background = Color(0xFF101416),
    error = Color(0xFFF2B8B5),
)

/** Material 3 theme with the colors of the system on Android 12 and later. */
@Composable
fun UnisonTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    dynamicColor: Boolean = true,
    content: @Composable () -> Unit,
) {
    val context = LocalContext.current
    val colors = when {
        dynamicColor && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S ->
            if (darkTheme) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        darkTheme -> DarkColors
        else -> LightColors
    }
    MaterialTheme(colorScheme = colors, content = content)
}
