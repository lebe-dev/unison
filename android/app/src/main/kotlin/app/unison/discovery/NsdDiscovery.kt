package app.unison.discovery

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.net.wifi.WifiManager
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.annotation.RequiresApi
import androidx.core.content.ContextCompat
import app.unison.core.mobile.Mobile
import java.net.InetAddress
import unison.mobile.v1.Mobile as MobileProto

/** Receives the devices found on the local network. */
fun interface CandidateSink {
    fun offer(
        service: String,
        addr: String,
        txt: ByteArray,
    )
}

/** Hands the discovered devices to the core running in this process. */
object CoreCandidates : CandidateSink {
    override fun offer(
        service: String,
        addr: String,
        txt: ByteArray,
    ) = Mobile.addPeerCandidate(service, addr, txt)
}

/**
 * Announces the services of the core over DNS-SD and reports the devices found
 * on the local network back to the core.
 */
class NsdDiscovery(
    context: Context,
    private val sink: CandidateSink = CoreCandidates,
) {
    private val appContext = context.applicationContext
    private val manager = appContext.getSystemService(NsdManager::class.java)
    private val main = Handler(Looper.getMainLooper())
    private val mainExecutor = ContextCompat.getMainExecutor(appContext)

    private var running = false
    private var lock: WifiManager.MulticastLock? = null
    private var networkRegistered = false
    private var currentNetwork: Network? = null

    /** Services the core wants to announce, whether registered or not. */
    private val desired = mutableMapOf<String, NsdServiceInfo>()

    /** Registrations the platform knows about. */
    private val announced = mutableMapOf<String, Advertisement>()

    /** Services being browsed. */
    private val browsing = mutableMapOf<String, NsdManager.DiscoveryListener>()

    private val queue = ArrayDeque<Resolve>()
    private val resolved = mutableSetOf<String>()
    private val callbacks = mutableMapOf<String, NsdManager.ServiceInfoCallback>()
    private var resolving: Resolve? = null

    /** Steps waiting for another attempt after a failure of the platform. */
    private val retries = mutableSetOf<Runnable>()

    private val timeout = Runnable { resolving?.let { finish(it, false) } }
    private val restart = Runnable { restartDiscovery() }

    /** Starts discovery: takes the multicast lock and browses for devices. */
    fun start() =
        onMain {
            if (running) {
                return@onMain
            }
            running = true
            acquireLock()
            registerNetwork()
            updateBrowsing(ServiceTypes.MAIN)
        }

    /** Stops discovery and withdraws every announcement. */
    fun stop() =
        onMain {
            if (!running) {
                return@onMain
            }
            running = false
            main.removeCallbacks(restart)
            for (retry in retries) {
                main.removeCallbacks(retry)
            }
            retries.clear()
            unregisterNetwork()
            for (service in browsing.keys.toList()) {
                stopBrowsing(service)
            }
            queue.clear()
            resolved.clear()
            main.removeCallbacks(timeout)
            resolving = null
            for (key in callbacks.keys.toList()) {
                unregisterCallback(key)
            }
            for (advertisement in announced.values.toList()) {
                unregister(advertisement)
            }
            desired.clear()
            releaseLock()
        }

    /** Applies a serialized unison.mobile.v1.Announcement. */
    fun announce(data: ByteArray) {
        val message =
            try {
                MobileProto.Announcement.parseFrom(data)
            } catch (e: Exception) {
                Log.w(TAG, "announcement does not decode: ${e.message}")
                return
            }
        onMain { publish(message) }
    }

    private fun publish(message: MobileProto.Announcement) {
        if (!running) {
            return
        }
        val service = message.service
        val type = ServiceTypes.typeOf(service)
        if (type == null) {
            Log.w(TAG, "ignored an announcement of an unknown service")
            return
        }
        if (message.withdraw) {
            desired.remove(service)
            reconcile(service)
            updateBrowsing(service)
            Log.i(TAG, "withdrew the announcement of $service")
            return
        }

        val info = serviceInfo(service, type, message) ?: return
        desired[service] = info
        reconcile(service)
        updateBrowsing(service)
    }

    private fun serviceInfo(
        service: String,
        type: String,
        message: MobileProto.Announcement,
    ): NsdServiceInfo? {
        val instance = message.instance
        val port = message.port
        if (instance.isEmpty() || instance.length > MAX_INSTANCE) {
            Log.w(TAG, "announcement of $service has an invalid instance name")
            return null
        }
        if (port !in 1..65535) {
            Log.w(TAG, "announcement of $service has an invalid port")
            return null
        }
        return try {
            NsdServiceInfo().apply {
                serviceName = instance
                serviceType = type
                this.port = port
                for ((key, value) in Candidates.records(message.txtMap)) {
                    setAttribute(key, value)
                }
            }
        } catch (e: IllegalArgumentException) {
            Log.w(TAG, "announcement of $service is not accepted: ${e.message}")
            null
        }
    }

    /** Brings the registration of the service in line with the wish of the core. */
    private fun reconcile(service: String) {
        val current = announced[service]
        val target = desired[service]
        when {
            current == null -> if (target != null) register(service, target)
            current.state != State.REGISTERED -> Unit
            target == null || !sameInfo(current.info, target) -> unregister(current)
        }
    }

    private fun register(
        service: String,
        info: NsdServiceInfo,
    ) {
        val nsd = manager
        if (!running || nsd == null) {
            return
        }
        val advertisement = Advertisement(service, info)
        val listener =
            object : NsdManager.RegistrationListener {
                override fun onServiceRegistered(info: NsdServiceInfo) =
                    onMain {
                        // The system may rename the instance to resolve a conflict.
                        advertisement.registeredName = info.serviceName
                        registered(advertisement)
                    }

                override fun onRegistrationFailed(
                    info: NsdServiceInfo,
                    errorCode: Int,
                ) = onMain { registrationFailed(advertisement, errorCode) }

                override fun onServiceUnregistered(info: NsdServiceInfo) = onMain { unregistered(advertisement) }

                override fun onUnregistrationFailed(
                    info: NsdServiceInfo,
                    errorCode: Int,
                ) = onMain { unregistrationFailed(advertisement, errorCode) }
            }
        advertisement.listener = listener
        announced[service] = advertisement
        try {
            nsd.registerService(info, NsdManager.PROTOCOL_DNS_SD, listener)
        } catch (e: Exception) {
            announced.remove(service)
            Log.w(TAG, "announcing $service failed: ${e.message}")
        }
    }

    private fun registered(advertisement: Advertisement) {
        // A withdrawal sent while the registration was on its way finishes on its own.
        if (advertisement.state == State.UNREGISTERING) {
            return
        }
        if (announced[advertisement.service] !== advertisement) {
            unregister(advertisement)
            return
        }
        advertisement.state = State.REGISTERED
        Log.i(TAG, "announced ${advertisement.service}")
        reconcile(advertisement.service)
    }

    private fun registrationFailed(
        advertisement: Advertisement,
        errorCode: Int,
    ) {
        if (announced[advertisement.service] !== advertisement) {
            return
        }
        announced.remove(advertisement.service)
        Log.w(TAG, "announcing ${advertisement.service} failed with code $errorCode, retrying later")
        retryLater { reconcile(advertisement.service) }
    }

    private fun unregistered(advertisement: Advertisement) {
        if (announced[advertisement.service] === advertisement) {
            announced.remove(advertisement.service)
        }
        reconcile(advertisement.service)
    }

    private fun unregistrationFailed(
        advertisement: Advertisement,
        errorCode: Int,
    ) {
        Log.w(TAG, "withdrawing ${advertisement.service} failed with code $errorCode")
        if (announced[advertisement.service] === advertisement) {
            announced.remove(advertisement.service)
        }
        reconcile(advertisement.service)
    }

    private fun unregister(advertisement: Advertisement) {
        if (advertisement.state == State.UNREGISTERING) {
            return
        }
        advertisement.state = State.UNREGISTERING
        try {
            manager?.unregisterService(advertisement.listener)
        } catch (e: Exception) {
            Log.w(TAG, "withdrawing ${advertisement.service} failed: ${e.message}")
            if (announced[advertisement.service] === advertisement) {
                announced.remove(advertisement.service)
            }
            reconcile(advertisement.service)
        }
    }

    /** Starts or stops browsing of the service. */
    private fun updateBrowsing(service: String) {
        val wanted =
            when (service) {
                ServiceTypes.MAIN -> running
                else -> running && desired.containsKey(service)
            }
        val active = browsing.containsKey(service)
        when {
            wanted && !active -> startBrowsing(service)
            !wanted && active -> stopBrowsing(service)
        }
    }

    private fun startBrowsing(service: String) {
        val nsd = manager
        val type = ServiceTypes.typeOf(service)
        if (nsd == null || type == null) {
            return
        }
        val listener =
            object : NsdManager.DiscoveryListener {
                override fun onStartDiscoveryFailed(
                    serviceType: String,
                    errorCode: Int,
                ) = onMain { discoveryFailed(service, errorCode) }

                override fun onStopDiscoveryFailed(
                    serviceType: String,
                    errorCode: Int,
                ) {
                    Log.w(TAG, "stopping discovery of $service failed with code $errorCode")
                }

                override fun onDiscoveryStarted(serviceType: String) {
                    Log.i(TAG, "browsing $service")
                }

                override fun onDiscoveryStopped(serviceType: String) {
                    Log.i(TAG, "stopped browsing $service")
                }

                override fun onServiceFound(info: NsdServiceInfo) = onMain { found(service, info) }

                override fun onServiceLost(info: NsdServiceInfo) = onMain { lost(service, info) }
            }
        browsing[service] = listener
        try {
            nsd.discoverServices(type, NsdManager.PROTOCOL_DNS_SD, listener)
        } catch (e: Exception) {
            browsing.remove(service)
            Log.w(TAG, "browsing $service failed: ${e.message}")
        }
    }

    private fun discoveryFailed(
        service: String,
        errorCode: Int,
    ) {
        browsing.remove(service)
        Log.w(TAG, "browsing $service failed with code $errorCode, retrying later")
        retryLater { updateBrowsing(service) }
    }

    /** Runs a step again after a pause, while discovery is still running. */
    private fun retryLater(step: () -> Unit) {
        val retry =
            object : Runnable {
                override fun run() {
                    retries.remove(this)
                    if (running) {
                        step()
                    }
                }
            }
        retries += retry
        main.postDelayed(retry, RETRY_DELAY_MS)
    }

    private fun stopBrowsing(service: String) {
        val listener = browsing.remove(service) ?: return
        try {
            manager?.stopServiceDiscovery(listener)
        } catch (e: Exception) {
            Log.w(TAG, "stopping discovery of $service failed: ${e.message}")
        }
        queue.removeAll { it.service == service }
        for (key in callbacks.keys.filter { it.startsWith("$service|") }) {
            unregisterCallback(key)
        }
        resolving?.let { current ->
            if (current.service == service) {
                finish(current, false)
            }
        }
    }

    private fun found(
        service: String,
        info: NsdServiceInfo,
    ) {
        if (!running) {
            return
        }
        val type = ServiceTypes.typeOf(service) ?: return
        if (info.serviceType.removeSuffix(".") != type.removeSuffix(".")) {
            return
        }
        // The own announcement comes back from the network; dialing it would only
        // run a full handshake with this device.
        if (Candidates.isOwn(info.serviceName, ownNames(service))) {
            return
        }
        Log.i(TAG, "found a device of $service")
        requestResolve(service, info)
    }

    private fun lost(
        service: String,
        info: NsdServiceInfo,
    ) {
        val key = keyOf(service, info.serviceName)
        resolved.remove(key)
        queue.removeAll { it.key == key }
        resolving?.let { current ->
            if (current.key == key) {
                finish(current, false)
            }
        }
        Log.i(TAG, "lost a device of $service")
    }

    /** Instance names this device announces for the service. */
    private fun ownNames(service: String): List<String> =
        listOfNotNull(
            desired[service]?.serviceName,
            announced[service]?.info?.serviceName,
            announced[service]?.registeredName,
        )

    /** Queues one service for resolution. Resolution runs one request at a time. */
    private fun requestResolve(
        service: String,
        info: NsdServiceInfo,
    ) {
        val key = keyOf(service, info.serviceName)
        if (key in resolved || queue.any { it.key == key } || resolving?.key == key) {
            return
        }
        queue.addLast(Resolve(service, info))
        pump()
    }

    private fun pump() {
        if (!running || resolving != null || queue.isEmpty()) {
            return
        }
        val request = queue.removeFirst()
        resolving = request
        main.postDelayed(timeout, RESOLVE_TIMEOUT_MS)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            resolveWithCallback(request)
        } else {
            resolveLegacy(request)
        }
    }

    @RequiresApi(Build.VERSION_CODES.UPSIDE_DOWN_CAKE)
    private fun resolveWithCallback(request: Resolve) {
        val callback =
            object : NsdManager.ServiceInfoCallback {
                override fun onServiceInfoCallbackRegistrationFailed(errorCode: Int) {
                    onMain {
                        Log.w(TAG, "resolving a device of ${request.service} failed with code $errorCode")
                        finish(request, false)
                    }
                }

                override fun onServiceUpdated(serviceInfo: NsdServiceInfo) {
                    onMain {
                        if (resolving !== request) {
                            return@onMain
                        }
                        finish(request, emit(request.service, serviceInfo))
                    }
                }

                override fun onServiceLost() {
                    onMain {
                        if (resolving !== request) {
                            return@onMain
                        }
                        Log.i(TAG, "lost a device of ${request.service} while resolving")
                        finish(request, false)
                    }
                }

                override fun onServiceInfoCallbackUnregistered() = Unit
            }
        callbacks[request.key] = callback
        try {
            manager?.registerServiceInfoCallback(request.info, mainExecutor, callback)
        } catch (e: Exception) {
            Log.w(TAG, "resolving a device of ${request.service} failed: ${e.message}")
            finish(request, false)
        }
    }

    @Suppress("DEPRECATION")
    private fun resolveLegacy(request: Resolve) {
        try {
            manager?.resolveService(
                request.info,
                object : NsdManager.ResolveListener {
                    override fun onResolveFailed(
                        serviceInfo: NsdServiceInfo,
                        errorCode: Int,
                    ) {
                        onMain {
                            Log.w(TAG, "resolving a device of ${request.service} failed with code $errorCode")
                            if (resolving !== request) {
                                return@onMain
                            }
                            finish(request, false)
                            // Before Android 14 one resolution runs at a time, also across the
                            // ones that timed out here; the device is tried again shortly.
                            val busy = errorCode == NsdManager.FAILURE_ALREADY_ACTIVE
                            if (busy && request.attempt < MAX_RESOLVE_ATTEMPTS) {
                                requeue(request)
                            }
                        }
                    }

                    override fun onServiceResolved(serviceInfo: NsdServiceInfo) {
                        onMain {
                            if (resolving !== request) {
                                return@onMain
                            }
                            finish(request, emit(request.service, serviceInfo))
                        }
                    }
                },
            )
        } catch (e: Exception) {
            Log.w(TAG, "resolving a device of ${request.service} failed: ${e.message}")
            finish(request, false)
        }
    }

    /** Queues a resolution once more after a short pause. */
    private fun requeue(request: Resolve) {
        val retry =
            object : Runnable {
                override fun run() {
                    retries.remove(this)
                    if (!running || !browsing.containsKey(request.service)) {
                        return
                    }
                    val key = request.key
                    if (key in resolved || queue.any { it.key == key } || resolving?.key == key) {
                        return
                    }
                    queue.addLast(Resolve(request.service, request.info, request.attempt + 1))
                    pump()
                }
            }
        retries += retry
        main.postDelayed(retry, RESOLVE_RETRY_MS)
    }

    private fun finish(
        request: Resolve,
        ok: Boolean,
    ) {
        if (resolving !== request) {
            return
        }
        resolving = null
        main.removeCallbacks(timeout)
        unregisterCallback(request.key)
        if (ok) {
            resolved.add(request.key)
        }
        pump()
    }

    /** Reports one resolved device to the core, false when it cannot be dialed. */
    private fun emit(
        service: String,
        info: NsdServiceInfo,
    ): Boolean {
        val port = info.port
        if (port !in 1..65535) {
            return false
        }
        val txt = Candidates.proto(info.attributes ?: emptyMap())
        var reported = false
        // One device can resolve to several addresses of this network, and a
        // VPN or a virtual bridge adds addresses that do not reach it. Every
        // address becomes its own candidate, so a dead one does not keep the
        // device offline.
        // The system cache may still hold the record of an earlier run of this app
        // under another instance name; its address is this device.
        for (host in Candidates.addresses(hostsOf(info), ownAddresses())) {
            val addr = Candidates.address(host, port) ?: continue
            try {
                sink.offer(service, addr, txt)
            } catch (e: Exception) {
                Log.w(TAG, "reporting a device of $service failed: ${e.message}")
                continue
            }
            reported = true
        }
        if (!reported) {
            Log.w(TAG, "a device of $service has no usable address")
            return false
        }
        Log.i(TAG, "reported a device of $service")
        return true
    }

    /** Addresses of this device on its current networks. */
    private fun ownAddresses(): Set<InetAddress> {
        val connectivity = appContext.getSystemService(ConnectivityManager::class.java) ?: return emptySet()
        return try {
            @Suppress("DEPRECATION")
            connectivity.allNetworks
                .mapNotNull { connectivity.getLinkProperties(it) }
                .flatMap { link -> link.linkAddresses.map { it.address } }
                .toSet()
        } catch (e: Exception) {
            Log.w(TAG, "reading the own addresses failed: ${e.message}")
            emptySet()
        }
    }

    @Suppress("DEPRECATION")
    private fun hostsOf(info: NsdServiceInfo): List<InetAddress?> =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            info.hostAddresses.toList()
        } else {
            listOf(info.host)
        }

    private fun unregisterCallback(key: String) {
        val callback = callbacks.remove(key) ?: return
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            return
        }
        try {
            manager?.unregisterServiceInfoCallback(callback)
        } catch (e: Exception) {
            Log.w(TAG, "unregistering a service info callback failed: ${e.message}")
        }
    }

    private fun acquireLock() {
        if (lock != null) {
            return
        }
        val wifi = appContext.getSystemService(WifiManager::class.java) ?: return
        try {
            lock =
                wifi.createMulticastLock(LOCK_TAG).apply {
                    setReferenceCounted(false)
                    acquire()
                }
            Log.i(TAG, "multicast lock taken")
        } catch (e: Exception) {
            Log.w(TAG, "multicast lock failed: ${e.message}")
        }
    }

    private fun releaseLock() {
        val current = lock ?: return
        lock = null
        try {
            if (current.isHeld) {
                current.release()
            }
            Log.i(TAG, "multicast lock released")
        } catch (e: Exception) {
            Log.w(TAG, "releasing the multicast lock failed: ${e.message}")
        }
    }

    private fun registerNetwork() {
        if (networkRegistered) {
            return
        }
        val connectivity = appContext.getSystemService(ConnectivityManager::class.java) ?: return
        currentNetwork = connectivity.activeNetwork
        try {
            connectivity.registerDefaultNetworkCallback(networkCallback)
            networkRegistered = true
        } catch (e: Exception) {
            Log.w(TAG, "network callback failed: ${e.message}")
        }
    }

    private fun unregisterNetwork() {
        if (!networkRegistered) {
            return
        }
        networkRegistered = false
        currentNetwork = null
        val connectivity = appContext.getSystemService(ConnectivityManager::class.java) ?: return
        try {
            connectivity.unregisterNetworkCallback(networkCallback)
        } catch (e: Exception) {
            Log.w(TAG, "network callback removal failed: ${e.message}")
        }
    }

    private val networkCallback =
        object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) =
                onMain {
                    if (currentNetwork != network) {
                        currentNetwork = network
                        scheduleRestart()
                    }
                }

            override fun onLost(network: Network) =
                onMain {
                    if (currentNetwork == network) {
                        currentNetwork = null
                    }
                    scheduleRestart()
                }
        }

    /** Restarts discovery once the network settles after a change. */
    private fun scheduleRestart() {
        if (!running) {
            return
        }
        main.removeCallbacks(restart)
        main.postDelayed(restart, RESTART_DELAY_MS)
    }

    private fun restartDiscovery() {
        if (!running) {
            return
        }
        Log.i(TAG, "network changed, restarting discovery")
        for (service in browsing.keys.toList()) {
            stopBrowsing(service)
        }
        resolved.clear()
        for (advertisement in announced.values.toList()) {
            unregister(advertisement)
        }
        // Services whose registration failed earlier are not in announced, so no
        // withdrawal callback brings them back.
        for (service in desired.keys.toList()) {
            reconcile(service)
        }
        updateBrowsing(ServiceTypes.MAIN)
        updateBrowsing(ServiceTypes.PAIRING)
    }

    private fun onMain(block: () -> Unit) {
        if (Looper.myLooper() == Looper.getMainLooper()) {
            block()
        } else {
            main.post(block)
        }
    }

    private enum class State {
        REGISTERING,
        REGISTERED,
        UNREGISTERING,
    }

    private class Advertisement(
        val service: String,
        val info: NsdServiceInfo,
    ) {
        var state = State.REGISTERING
        lateinit var listener: NsdManager.RegistrationListener

        /** Name the system registered, which differs from the asked one after a conflict. */
        var registeredName: String? = null
    }

    private class Resolve(
        val service: String,
        val info: NsdServiceInfo,
        val attempt: Int = 1,
    ) {
        val key = keyOf(service, info.serviceName)
    }

    private companion object {
        const val TAG = "NsdDiscovery"
        const val LOCK_TAG = "unison.discovery"
        const val RESOLVE_TIMEOUT_MS = 10_000L
        const val RESTART_DELAY_MS = 500L
        const val RETRY_DELAY_MS = 10_000L
        const val RESOLVE_RETRY_MS = 1_000L
        const val MAX_RESOLVE_ATTEMPTS = 3
        const val MAX_INSTANCE = 63
    }
}

/** Key of one service instance, unique across services. */
private fun keyOf(
    service: String,
    instance: String,
): String = "$service|$instance"

/** Compares two announcements by the content the platform registers. */
private fun sameInfo(
    a: NsdServiceInfo,
    b: NsdServiceInfo,
): Boolean {
    if (a.serviceName != b.serviceName || a.port != b.port) {
        return false
    }
    val left = a.attributes ?: emptyMap()
    val right = b.attributes ?: emptyMap()
    if (left.keys != right.keys) {
        return false
    }
    return left.all { (key, value) -> right[key]?.contentEquals(value) == true }
}
