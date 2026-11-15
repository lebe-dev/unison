package app.unison.ui

import android.text.format.DateFormat
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import app.unison.R
import java.util.Date
import unison.local.v1.Local

/** Label of the operating system of a paired device. */
@Composable
fun platformLabel(platform: Local.Platform): String = when (platform) {
    Local.Platform.PLATFORM_MACOS -> stringResource(R.string.platform_macos)
    Local.Platform.PLATFORM_WINDOWS -> stringResource(R.string.platform_windows)
    Local.Platform.PLATFORM_ANDROID -> stringResource(R.string.platform_android)
    else -> stringResource(R.string.platform_unknown)
}

/** Label of the network path to a device, null when the path is unknown. */
@Composable
fun pathLabel(path: Local.Path): String? = when (path) {
    Local.Path.PATH_LAN -> stringResource(R.string.device_path_lan)
    Local.Path.PATH_RELAY -> stringResource(R.string.device_path_relay)
    else -> null
}

/** Label of the network state of this device. */
@Composable
fun networkLabel(network: Local.NetworkState?): String = when {
    network == null || !network.online -> stringResource(R.string.network_offline)
    network.wifi -> stringResource(R.string.network_wifi)
    network.cellular -> stringResource(R.string.network_cellular)
    else -> stringResource(R.string.network_connected)
}

/** Label of the clipboard pause in the form the notification uses. */
@Composable
fun pauseLabel(pause: Local.Pause?): String {
    if (pause == null || !pause.paused) {
        return stringResource(R.string.tile_pause_off)
    }
    if (pause.indefinite) {
        return stringResource(R.string.tile_pause_indefinite)
    }
    val format = DateFormat.getTimeFormat(LocalContext.current)
    return stringResource(R.string.tile_pause_until, format.format(Date(pause.untilUnixMs)))
}
