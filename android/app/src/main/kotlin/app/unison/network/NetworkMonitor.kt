package app.unison.network

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.core.content.ContextCompat

/**
 * Follows the network conditions of the device: the default network, the
 * presence of Wi-Fi and the system data saver. Every change is reported once.
 */
class NetworkMonitor(
    context: Context,
    private val listener: Listener,
) {
    /** Receives the conditions when they change. */
    fun interface Listener {
        fun onNetworkConditions(conditions: NetworkConditions)
    }

    private val appContext = context.applicationContext
    private val connectivity = appContext.getSystemService(ConnectivityManager::class.java)
    private val main = Handler(Looper.getMainLooper())
    private val wifiNetworks = mutableSetOf<Network>()

    private var defaultCapabilities: NetworkCapabilities? = null
    private var registered = false

    /** Conditions of the last report, offline before the first callback. */
    var conditions = NetworkConditions()
        private set

    /** Starts the callbacks. Safe to call twice. */
    fun start() {
        if (registered) {
            return
        }
        val manager = connectivity
        if (manager == null) {
            Log.w(TAG, "connectivity service is not available")
            return
        }
        registered = true
        try {
            manager.registerDefaultNetworkCallback(defaultCallback, main)
        } catch (e: Exception) {
            Log.w(TAG, "default network callback failed: ${e.message}")
        }
        try {
            manager.registerNetworkCallback(wifiRequest, wifiCallback, main)
        } catch (e: Exception) {
            Log.w(TAG, "wi-fi network callback failed: ${e.message}")
        }
        try {
            ContextCompat.registerReceiver(
                appContext,
                saverReceiver,
                IntentFilter(ConnectivityManager.ACTION_RESTRICT_BACKGROUND_CHANGED),
                ContextCompat.RECEIVER_NOT_EXPORTED,
            )
        } catch (e: Exception) {
            Log.w(TAG, "data saver receiver failed: ${e.message}")
        }
    }

    /** Stops the callbacks and forgets the network. */
    fun stop() {
        if (!registered) {
            return
        }
        registered = false
        wifiNetworks.clear()
        defaultCapabilities = null
        val manager = connectivity
        try {
            manager?.unregisterNetworkCallback(defaultCallback)
        } catch (e: Exception) {
            Log.w(TAG, "default network callback removal failed: ${e.message}")
        }
        try {
            manager?.unregisterNetworkCallback(wifiCallback)
        } catch (e: Exception) {
            Log.w(TAG, "wi-fi network callback removal failed: ${e.message}")
        }
        try {
            appContext.unregisterReceiver(saverReceiver)
        } catch (e: Exception) {
            Log.w(TAG, "data saver receiver removal failed: ${e.message}")
        }
    }

    private val defaultCallback =
        object : ConnectivityManager.NetworkCallback() {
            override fun onCapabilitiesChanged(
                network: Network,
                capabilities: NetworkCapabilities,
            ) {
                defaultCapabilities = capabilities
                publish()
            }

            override fun onLost(network: Network) {
                defaultCapabilities = null
                publish()
            }
        }

    private val wifiCallback =
        object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                if (wifiNetworks.add(network)) {
                    publish()
                }
            }

            override fun onLost(network: Network) {
                if (wifiNetworks.remove(network)) {
                    publish()
                }
            }
        }

    private val saverReceiver =
        object : BroadcastReceiver() {
            override fun onReceive(
                context: Context?,
                intent: Intent?,
            ) = publish()
        }

    private fun publish() {
        val capabilities = defaultCapabilities
        val cellular = capabilities?.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) == true
        val roaming =
            cellular &&
                capabilities != null &&
                !capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_ROAMING)
        val next =
            NetworkConditions(
                online = capabilities != null,
                metered =
                    capabilities != null &&
                        !capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED),
                roaming = roaming,
                vpn = capabilities?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true,
                dataSaver = dataSaver(),
                wifi = wifiNetworks.isNotEmpty(),
                ethernet = capabilities?.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) == true,
            )
        if (next == conditions) {
            return
        }
        conditions = next
        Log.i(TAG, "network changed: ${describe(next)}")
        listener.onNetworkConditions(next)
    }

    private fun dataSaver(): Boolean = BackgroundData.dataSaver(BackgroundData.status(appContext))

    private fun describe(conditions: NetworkConditions): String =
        with(conditions) {
            "online=$online metered=$metered roaming=$roaming vpn=$vpn " +
                "wifi=$wifi ethernet=$ethernet data_saver=$dataSaver"
        }

    private companion object {
        const val TAG = "NetworkMonitor"

        // The builder asks for internet access by default; a local Wi-Fi without an uplink
        // still carries the peers, so it counts as Wi-Fi too.
        val wifiRequest: NetworkRequest =
            NetworkRequest
                .Builder()
                .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
                .removeCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                .build()
    }
}
