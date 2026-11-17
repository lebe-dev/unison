package app.unison.service

import android.annotation.SuppressLint
import android.app.Notification
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.ServiceInfo
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import app.unison.R
import app.unison.clipboard.HeldItems
import app.unison.clipboard.HeldNotifications
import app.unison.core.CoreClient
import app.unison.core.CoreException
import app.unison.core.coreEvents
import app.unison.core.mobile.Mobile
import app.unison.diagnostics.Diagnostics
import app.unison.discovery.NsdDiscovery
import app.unison.history.HistoryDownloads
import app.unison.history.HistoryNotifications
import app.unison.network.NetworkConditions
import app.unison.network.NetworkMonitor
import app.unison.requests.RequestNotifications
import app.unison.share.TransferNotifications
import app.unison.share.TransferProgress
import app.unison.storage.BlobSink
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import unison.local.v1.Local
import unison.mobile.v1.Mobile as MobileProto

/** Keeps the core running in the foreground. */
class UnisonService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val client = CoreClient()
    private val coreMutex = Mutex()
    private var coreRunning = false

    /** A core started by this instance may be running; written on the IO thread. */
    @Volatile
    private var coreStarted = false

    @Volatile
    private var destroyed = false

    /** True once the service has posted its foreground notification. */
    private var statusPosted = false

    /** The user dismissed the status notification; it stays away until a file arrives. */
    private var statusDismissed = false

    private var eventsJob: Job? = null
    private var requestsDirty = false
    private var statusJob: Job? = null
    private var transfersJob: Job? = null
    private var requestsJob: Job? = null
    private var requestsTimer: Job? = null
    private val watchedTransfers = mutableSetOf<String>()
    private val reportedTransfers = mutableSetOf<String>()
    private val notifiedRequests = mutableSetOf<String>()
    private var transferNames = emptyList<String>()
    private var transfersCanceled = false
    private lateinit var discovery: NsdDiscovery
    private lateinit var network: NetworkMonitor
    private lateinit var blobs: BlobSink
    private lateinit var downloads: HistoryDownloads

    override fun onCreate() {
        super.onCreate()
        NotificationChannels.create(this)
        discovery = NsdDiscovery(this)
        network = NetworkMonitor(this, ::onNetworkConditions)
        blobs = BlobSink(this, scope)
        downloads = HistoryDownloads(this, client)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopSelf()
            return START_NOT_STICKY
        }
        if (intent?.action == ACTION_STATUS_DISMISSED) {
            // The user swiped the status notification away; a later file brings it back.
            if (NotificationSettings.load(this).hideStatusAfterDismissal) {
                statusDismissed = true
            }
            return START_STICKY
        }

        if (!statusPosted) {
            startInForeground(getString(R.string.service_starting))
        }
        startCore()
        if (intent?.action == ACTION_DOWNLOAD_ITEM) {
            intent.getStringExtra(EXTRA_ITEM_ID)?.let(::download)
        }
        if (intent?.action == ACTION_CANCEL_TRANSFERS) {
            cancelTransfers()
        }
        if (intent?.action == ACTION_APPLY_ITEM) {
            intent.getStringExtra(EXTRA_ITEM_ID)?.let(::applyItem)
        }
        if (intent?.action == ACTION_ACCEPT_REQUEST || intent?.action == ACTION_DECLINE_REQUEST) {
            val requestId = intent.getStringExtra(EXTRA_REQUEST_ID)
            if (requestId != null) {
                answerRequest(requestId, accept = intent.action == ACTION_ACCEPT_REQUEST)
            }
        }
        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        // Written before coreStarted is read: a start that is still running either sees
        // this flag or is seen here, so the core never outlives the service.
        destroyed = true
        network.stop()
        scope.cancel()
        discovery.stop()
        NotificationManagerCompat.from(this).cancel(TransferNotifications.ID)
        for (requestId in notifiedRequests) {
            NotificationManagerCompat.from(this).cancel(RequestNotifications.idOf(requestId))
        }
        notifiedRequests.clear()
        coreRunning = false
        statusPosted = false
        if (coreStarted) {
            runBlocking(Dispatchers.IO) { stopCoreBlocking() }
        }
        super.onDestroy()
    }

    private fun startInForeground(text: CharSequence) {
        ServiceCompat.startForeground(
            this,
            NOTIFICATION_ID,
            ServiceNotifications.status(this, text),
            ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE,
        )
        statusPosted = true
    }

    /** Starts the network monitor and the core, as the operating mode allows. */
    private fun startCore() {
        network.start()
        applyMode()
    }

    /** Reports the conditions to the core and starts or stops it as the mode asks. */
    private fun applyMode(report: Boolean = false) {
        scope.launch {
            coreMutex.withLock {
                if (report && coreRunning) {
                    reportNetwork()
                }
                val mode = UnisonSettings.operatingMode(this@UnisonService)
                val allowed = mode != OperatingMode.WIFI_ONLY || network.conditions.localNetwork
                when {
                    allowed && !coreRunning -> startCoreLocked()
                    // A start command of a running service restores the status.
                    allowed && coreRunning -> refreshStatus()
                    !allowed && coreRunning -> pauseCoreLocked()
                    !allowed -> show(getString(R.string.service_waiting_wifi))
                }
            }
        }
    }

    /** Reacts to a change of the network conditions. */
    private fun onNetworkConditions(conditions: NetworkConditions) {
        Log.i(TAG, "network state applied: online=${conditions.online} local=${conditions.localNetwork}")
        applyMode(report = true)
    }

    /** Starts the core. Runs under the lock of [applyMode]. */
    private suspend fun startCoreLocked() {
        val started = withContext(Dispatchers.IO) { startCoreBlocking() }
        if (!started) {
            stopSelf()
            return
        }
        coreRunning = true
        reportNetwork()
        watchEvents()
        refreshStatus()
    }

    /** Stops the core until the operating mode or the network allows it again. */
    private suspend fun pauseCoreLocked() {
        coreRunning = false
        withContext(Dispatchers.IO) { stopCoreBlocking() }
        discovery.stop()
        Log.i(TAG, "core paused until the network allows it")
        show(getString(R.string.service_waiting_wifi))
    }

    /** Sends the last known network conditions to the core. */
    private fun reportNetwork() {
        val state = network.conditions.toProto()
        try {
            Mobile.networkChanged(state.toByteArray())
        } catch (e: Exception) {
            Log.w(TAG, "reporting the network failed: ${e.message}")
        }
    }

    /** Collects the events of the core once per service. */
    private fun watchEvents() {
        if (eventsJob?.isActive == true) {
            return
        }
        eventsJob = scope.launch {
            coreEvents.events.collect(::handle)
        }
    }

    private fun startCoreBlocking(): Boolean = try {
        val debug = applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0
        val config = MobileProto.Config.newBuilder()
            .setDataDir(filesDir.absolutePath)
            .setDeviceName(UnisonSettings.deviceName(this))
            .setDebugLog(debug)
            .build()
        // A core left by an earlier instance of the service would make the start fail and
        // still call back into the platform of that instance.
        stopCoreBlocking()
        discovery.start()
        Mobile.start(config.toByteArray(), CorePlatform(this, discovery, blobs))
        coreStarted = true
        // The service may have been destroyed while the core started; then nobody else stops it.
        if (destroyed) {
            Log.i(TAG, "core stopped right after the start, the service is gone")
            stopCoreBlocking()
            discovery.stop()
            false
        } else {
            Log.i(TAG, "core started")
            true
        }
    } catch (e: Exception) {
        Log.e(TAG, "core start failed", e)
        Diagnostics.capture(e)
        discovery.stop()
        false
    }

    /** Stops the core of the process; does nothing when none runs. */
    private fun stopCoreBlocking() {
        try {
            Mobile.stop()
        } catch (e: Exception) {
            Log.w(TAG, "core stop failed: ${e.message}")
            Diagnostics.capture(e)
        }
        coreStarted = false
    }

    /** Reacts to the events of the core that change what the user sees. */
    private fun handle(event: MobileProto.Event) {
        when {
            event.type == MobileProto.EventType.EVENT_TYPE_PEER_ONLINE ||
                event.type == MobileProto.EventType.EVENT_TYPE_PEER_OFFLINE -> refreshStatus()
            event.hasEvent() && event.event.kind == Local.EventKind.EVENT_KIND_CLIP_DOWNLOAD -> notifyPending(event.event)
            event.hasEvent() && event.event.kind == Local.EventKind.EVENT_KIND_CLIP_APPLY -> notifyHeld(event.event)
            event.hasEvent() && event.event.kind == Local.EventKind.EVENT_KIND_TRANSFER -> {
                if (IncomingFiles.arrives(event.event)) {
                    restoreStatus()
                }
                refreshTransfers()
                refreshRequests()
            }
        }
    }

    /** Keeps the transfers notification in step with the running transfers. */
    private fun refreshTransfers() {
        if (transfersJob?.isActive == true) {
            return
        }
        transfersJob = scope.launch {
            var sawActive = false
            while (true) {
                val transfers = listTransfers()
                if (transfers == null) {
                    failTransfers()
                    return@launch
                }
                val active = TransferProgress.active(transfers)
                if (active.isEmpty()) {
                    finishTransfers(transfers, fresh = !sawActive)
                    return@launch
                }
                sawActive = true
                watchedTransfers += active.map { it.id }
                transferNames = TransferProgress.names(active)
                showTransfers(TransferNotifications.progress(this@UnisonService, active))
                delay(TRANSFERS_POLL_MS)
            }
        }
    }

    /** Running outgoing transfers, or null when the core does not answer. */
    private suspend fun listTransfers(): List<Local.Transfer>? = try {
        withContext(Dispatchers.IO) { client.listTransfers() }.transfersList
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        Log.w(TAG, "cannot read the transfers: ${e.message}")
        null
    }

    /**
     * Shows the result of the transfers that just ended. A transfer can end
     * before the first poll, then the ones that started a moment ago are
     * reported once.
     */
    private fun finishTransfers(transfers: List<Local.Transfer>, fresh: Boolean) {
        val ended = transfers.filter { it.id in watchedTransfers }
        val reported = if (ended.isNotEmpty() || !fresh) ended else justStarted(transfers)
        if (reported.isEmpty()) {
            transfersCanceled = false
            return
        }
        if (transferNames.isEmpty()) {
            transferNames = TransferProgress.names(reported)
        }
        val failed = reported.any { it.state == Local.TransferState.TRANSFER_STATE_FAILED }
        val message = when {
            transfersCanceled -> getString(R.string.transfer_canceled)
            failed -> getString(R.string.transfer_failed)
            else -> getString(R.string.transfer_sent, namesText())
        }
        reportedTransfers += reported.map { it.id }
        if (reportedTransfers.size > REPORTED_LIMIT) {
            reportedTransfers.clear()
        }
        watchedTransfers.clear()
        transfersCanceled = false
        showTransfers(TransferNotifications.result(this, message))
    }

    /** Outgoing transfers that started recently and were not reported yet. */
    private fun justStarted(transfers: List<Local.Transfer>): List<Local.Transfer> {
        val since = System.currentTimeMillis() - RECENT_TRANSFER_MS
        return transfers.filter { transfer ->
            transfer.direction == Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING &&
                transfer.id !in reportedTransfers &&
                transfer.createdAtUnixMs >= since
        }
    }

    /** Tells the user that the transfers cannot be followed any more. */
    private fun failTransfers() {
        if (watchedTransfers.isEmpty()) {
            transfersCanceled = false
            return
        }
        watchedTransfers.clear()
        transfersCanceled = false
        showTransfers(TransferNotifications.result(this, getString(R.string.transfer_failed)))
    }

    /** Stops every running outgoing transfer. */
    private fun cancelTransfers() {
        transfersCanceled = true
        scope.launch {
            val active = listTransfers()?.let(TransferProgress::active).orEmpty()
            for (transfer in active) {
                try {
                    withContext(Dispatchers.IO) { client.cancelTransfer(transfer.id) }
                } catch (e: Exception) {
                    Log.w(TAG, "cannot cancel a transfer: ${e.message}")
                }
            }
            refreshTransfers()
        }
    }

    /** Names of the peers of the transfers, as the texts expect them. */
    private fun namesText(): String = when {
        transferNames.isEmpty() -> getString(R.string.app_name)
        transferNames.size == 1 -> transferNames.first()
        else -> resources.getQuantityString(
            R.plurals.transfer_more_devices,
            transferNames.size - 1,
            transferNames.first(),
            transferNames.size - 1,
        )
    }

    @SuppressLint("MissingPermission")
    private fun showTransfers(notification: Notification) {
        if (!NotificationSettings.load(this).transfers) {
            return
        }
        // The service runs without POST_NOTIFICATIONS; the notification is just hidden.
        NotificationManagerCompat.from(this).notify(TransferNotifications.ID, notification)
    }

    /** Tells the user about an item that has to be downloaded first. */
    private fun notifyPending(event: Local.Event) {
        val item = event.item
        if (item.id.isEmpty()) {
            return
        }
        scope.launch {
            showPending(item, deviceName(event.deviceId))
        }
    }

    private suspend fun deviceName(deviceId: String): String = try {
        withContext(Dispatchers.IO) { client.status() }
            .devicesList
            .firstOrNull { it.deviceId == deviceId }
            ?.name
            .orEmpty()
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        Log.w(TAG, "cannot read the name of $deviceId: ${e.message}")
        ""
    }

    @SuppressLint("MissingPermission")
    private fun showPending(item: Local.ClipItem, device: String) {
        if (!NotificationSettings.load(this).clipboard) {
            return
        }
        // The service runs without POST_NOTIFICATIONS; the notification is just hidden.
        NotificationManagerCompat.from(this).notify(
            HistoryNotifications.idOf(item.id),
            HistoryNotifications.pending(this, item, device),
        )
    }

    /** Tells the user about an item that waits for the apply action. */
    private fun notifyHeld(event: Local.Event) {
        val item = event.item
        if (item.id.isEmpty()) {
            return
        }
        scope.launch { showHeld(item, deviceName(event.deviceId)) }
    }

    @SuppressLint("MissingPermission")
    private fun showHeld(item: Local.ClipItem, device: String) {
        if (!NotificationSettings.load(this).confirmations) {
            return
        }
        // The service runs without POST_NOTIFICATIONS; the notification is just hidden.
        NotificationManagerCompat.from(this).notify(
            HeldNotifications.idOf(item.id),
            HeldNotifications.held(this, item, device),
        )
    }

    /** Applies the clipboard item of the notification action. */
    private fun applyItem(itemId: String) {
        show(getString(R.string.clipboard_applying))
        scope.launch {
            val applied = try {
                withContext(Dispatchers.IO) { client.applyHistoryItem(itemId) }
                true
            } catch (e: Exception) {
                Log.w(TAG, "apply of $itemId failed: ${e.message}")
                false
            }
            NotificationManagerCompat.from(this@UnisonService).cancel(HeldNotifications.idOf(itemId))
            if (applied) {
                HeldItems.shared.remove(itemId)
            }
            showApplyResult(applied)
            refreshStatus()
        }
    }

    @SuppressLint("MissingPermission")
    private fun showApplyResult(applied: Boolean) {
        if (!NotificationSettings.load(this).clipboard) {
            return
        }
        // The service runs without POST_NOTIFICATIONS; the notification is just hidden.
        NotificationManagerCompat.from(this).notify(
            HeldNotifications.RESULT_ID,
            HeldNotifications.result(this, applied),
        )
    }

    /** Keeps the request notifications in step with the offers of the core. */
    private fun refreshRequests() {
        if (requestsJob?.isActive == true) {
            // An offer may have arrived after the running read; read once more when it ends.
            requestsDirty = true
            return
        }
        requestsDirty = false
        requestsJob = scope.launch {
            val requests = listRequests()
            if (requests != null) {
                showRequests(requests)
                scheduleRequestsRefresh()
            }
        }.also { job ->
            job.invokeOnCompletion { cause ->
                if (cause == null && requestsDirty) {
                    scope.launch { refreshRequests() }
                }
            }
        }
    }

    /** Shows the offers that wait and removes the notifications of the answered ones. */
    private fun showRequests(requests: List<Local.ShareRequest>) {
        val current = requests.map { it.id }.toSet()
        for (gone in notifiedRequests - current) {
            NotificationManagerCompat.from(this).cancel(RequestNotifications.idOf(gone))
            notifiedRequests -= gone
        }
        for (request in requests) {
            notifiedRequests += request.id
            showRequest(request)
        }
    }

    /** Reads the offers again while some of them still wait for an answer. */
    private fun scheduleRequestsRefresh() {
        requestsTimer?.cancel()
        if (notifiedRequests.isEmpty()) {
            return
        }
        requestsTimer = scope.launch {
            delay(REQUESTS_REFRESH_MS)
            refreshRequests()
        }
    }

    /** Offers of Friends that wait for an answer, or null when the core does not answer. */
    private suspend fun listRequests(): List<Local.ShareRequest>? = try {
        withContext(Dispatchers.IO) { client.requests() }.requestsList
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        Log.w(TAG, "cannot read the requests: ${e.message}")
        null
    }

    @SuppressLint("MissingPermission")
    private fun showRequest(request: Local.ShareRequest) {
        if (!NotificationSettings.load(this).requests) {
            return
        }
        // The service runs without POST_NOTIFICATIONS; the notification is just hidden.
        NotificationManagerCompat.from(this).notify(
            RequestNotifications.idOf(request.id),
            RequestNotifications.request(this, request),
        )
    }

    /** Answers the offer of the notification action. */
    private fun answerRequest(requestId: String, accept: Boolean) {
        show(getString(R.string.requests_answering))
        scope.launch {
            val answered = try {
                withContext(Dispatchers.IO) { client.decideRequest(requestId, accept) }
                true
            } catch (e: Exception) {
                Log.w(TAG, "cannot answer the request: ${e.message}")
                false
            }
            NotificationManagerCompat.from(this@UnisonService)
                .cancel(RequestNotifications.idOf(requestId))
            notifiedRequests -= requestId
            showRequestResult(answered, accept)
            refreshRequests()
            // The answer replaced the text of the persistent notification.
            refreshStatus()
        }
    }

    @SuppressLint("MissingPermission")
    private fun showRequestResult(answered: Boolean, accept: Boolean) {
        if (!NotificationSettings.load(this).requests) {
            return
        }
        val text = when {
            !answered -> getString(R.string.requests_answer_failed)
            accept -> getString(R.string.requests_accepted)
            else -> getString(R.string.requests_declined)
        }
        // The service runs without POST_NOTIFICATIONS; the notification is just hidden.
        NotificationManagerCompat.from(this).notify(
            RequestNotifications.RESULT_ID,
            RequestNotifications.result(this, text),
        )
    }

    /** Saves the payloads of an item into the Downloads collection. */
    private fun download(itemId: String) {
        show(getString(R.string.history_downloading))
        scope.launch {
            val saved = try {
                withContext(Dispatchers.IO) { downloads.save(itemId) }
            } catch (e: Exception) {
                Log.w(TAG, "download of $itemId failed: ${e.message}")
                0
            }
            showResult(itemId, saved > 0)
            refreshStatus()
        }
    }

    @SuppressLint("MissingPermission")
    private fun showResult(itemId: String, saved: Boolean) {
        NotificationManagerCompat.from(this).cancel(HistoryNotifications.idOf(itemId))
        if (!NotificationSettings.load(this).clipboard) {
            return
        }
        NotificationManagerCompat.from(this).notify(
            HistoryNotifications.RESULT_ID,
            HistoryNotifications.result(this@UnisonService, saved),
        )
    }

    private fun refreshStatus() {
        if (statusDismissed) {
            return
        }
        statusJob?.cancel()
        statusJob = scope.launch {
            val text = statusText()
            if (!coreRunning) {
                return@launch
            }
            show(text)
        }
    }

    /** Brings the status notification back after a file arrives. */
    private fun restoreStatus() {
        if (!statusDismissed) {
            return
        }
        statusDismissed = false
        refreshStatus()
    }

    private suspend fun statusText(): CharSequence = try {
        val status = withContext(Dispatchers.IO) { client.status() }
        val online = status.devicesList.count { it.online }
        Log.i(TAG, "core is up, device ${status.deviceId}")
        if (!NotificationSettings.load(this).statusDetails) {
            getString(R.string.service_status_short)
        } else {
            getString(
                R.string.service_status,
                status.deviceName,
                resources.getQuantityString(R.plurals.service_peers_online, online, online),
            )
        }
    } catch (e: CancellationException) {
        throw e
    } catch (e: CoreException) {
        Log.w(TAG, "status failed: ${e.message}")
        getString(R.string.service_failed)
    } catch (e: Exception) {
        Log.w(TAG, "status failed: ${e.message}")
        getString(R.string.service_failed)
    }

    @SuppressLint("MissingPermission")
    private fun show(text: CharSequence) {
        if (statusDismissed) {
            return
        }
        // The service runs without POST_NOTIFICATIONS; the notification is just hidden.
        NotificationManagerCompat.from(this).notify(
            NOTIFICATION_ID,
            ServiceNotifications.status(this, text),
        )
    }

    companion object {
        private const val TAG = "UnisonService"

        /** Identifier of the persistent notification. */
        const val NOTIFICATION_ID = 1

        /** Starts the core, or keeps the running one. */
        const val ACTION_START = "app.unison.action.START"

        /** Stops the service and the core. */
        const val ACTION_STOP = "app.unison.action.STOP"

        /** The user dismissed the persistent notification. */
        const val ACTION_STATUS_DISMISSED = "app.unison.action.STATUS_DISMISSED"

        /** Downloads one history entry. */
        const val ACTION_DOWNLOAD_ITEM = "app.unison.action.DOWNLOAD_ITEM"

        /** Stops the outgoing file transfers. */
        const val ACTION_CANCEL_TRANSFERS = "app.unison.action.CANCEL_TRANSFERS"

        /** Applies a clipboard item that waited for a confirmation. */
        const val ACTION_APPLY_ITEM = "app.unison.action.APPLY_ITEM"

        /** Accepts the file offer of a Friends device. */
        const val ACTION_ACCEPT_REQUEST = "app.unison.action.ACCEPT_REQUEST"

        /** Declines the file offer of a Friends device. */
        const val ACTION_DECLINE_REQUEST = "app.unison.action.DECLINE_REQUEST"

        /** Entry the download action refers to. */
        const val EXTRA_ITEM_ID = "app.unison.extra.ITEM_ID"

        /** File offer the answer actions refer to. */
        const val EXTRA_REQUEST_ID = "app.unison.extra.REQUEST_ID"

        fun start(context: Context) {
            val intent = Intent(context, UnisonService::class.java).setAction(ACTION_START)
            ContextCompat.startForegroundService(context, intent)
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, UnisonService::class.java))
        }

        /** Time between two reads of the running transfers. */
        private const val TRANSFERS_POLL_MS = 500L

        /** Age of a finished transfer that is still reported after a single poll. */
        private const val RECENT_TRANSFER_MS = 30_000L

        /** Transfers remembered as reported before the list is dropped. */
        private const val REPORTED_LIMIT = 32

        /** Time between two reads of the offers that still wait for an answer. */
        private const val REQUESTS_REFRESH_MS = 30_000L
    }
}
