package app.unison.ui.pairing

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import java.util.Locale
import kotlinx.coroutines.delay

/** Remaining time until [expiresAtUnixMs], updated twice a second. */
@Composable
fun rememberWindowRemaining(expiresAtUnixMs: Long): String {
    var remaining by remember(expiresAtUnixMs) {
        mutableLongStateOf((expiresAtUnixMs - System.currentTimeMillis()).coerceAtLeast(0))
    }
    LaunchedEffect(expiresAtUnixMs) {
        while (remaining > 0) {
            delay(REFRESH_MS)
            remaining = (expiresAtUnixMs - System.currentTimeMillis()).coerceAtLeast(0)
        }
    }
    return formatRemaining(remaining)
}

/** Remaining time as minutes and seconds, rounded up to the next second. */
internal fun formatRemaining(remainingMs: Long): String {
    val seconds = (remainingMs.coerceAtLeast(0) + MILLIS_PER_SECOND - 1) / MILLIS_PER_SECOND
    return String.format(Locale.US, "%d:%02d", seconds / SECONDS_PER_MINUTE, seconds % SECONDS_PER_MINUTE)
}

private const val REFRESH_MS = 500L
private const val MILLIS_PER_SECOND = 1_000L
private const val SECONDS_PER_MINUTE = 60L
