package app.unison.service

import android.annotation.SuppressLint
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
import app.unison.core.CoreClient
import app.unison.core.CoreException
import app.unison.core.coreEvents
import app.unison.core.mobile.Mobile
import app.unison.discovery.NsdDiscovery
import app.unison.history.HistoryDownloads
import app.unison.history.HistoryNotifications
import app.unison.storage.BlobSink
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import unison.local.v1.Local
import unison.mobile.v1.Mobile as MobileProto

/** Keeps the core running in the foreground. */
class UnisonService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val client = CoreClient()
    private var coreStarted = false
    private var statusJob: Job? = null
    private lateinit var discovery: NsdDiscovery
    private lateinit var blobs: BlobSink
    private lateinit var downloads: HistoryDownloads

    override fun onCreate() {
        super.onCreate()
        NotificationChannels.create(this)
        discovery = NsdDiscovery(this)
        blobs = BlobSink(this, scope)
        downloads = HistoryDownloads(this, client)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopSelf()
            return START_NOT_STICKY
        }

        startInForeground(getString(R.string.service_starting))
        startCore()
        if (intent?.action == ACTION_DOWNLOAD_ITEM) {
            intent.getStringExtra(EXTRA_ITEM_ID)?.let(::download)
        }
        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        scope.cancel()
        discovery.stop()
        if (coreStarted) {
            coreStarted = false
            runBlocking(Dispatchers.IO) {
                try {
                    Mobile.stop()
                } catch (e: Exception) {
                    Log.w(TAG, "core stop failed: ${e.message}")
                }
            }
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
    }

    private fun startCore() {
        if (coreStarted) {
            return
        }
        coreStarted = true

        scope.launch {
            val started = withContext(Dispatchers.IO) { startCoreBlocking() }
            if (!started) {
                stopSelf()
                return@launch
            }
            scope.launch {
                coreEvents.events.collect(::handle)
            }
            refreshStatus()
        }
    }

    private fun startCoreBlocking(): Boolean = try {
        val debug = applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0
        val config = MobileProto.Config.newBuilder()
            .setDataDir(filesDir.absolutePath)
            .setDeviceName(UnisonSettings.deviceName(this))
            .setDebugLog(debug)
            .build()
        discovery.start()
        Mobile.start(config.toByteArray(), CorePlatform(this, discovery, blobs))
        Log.i(TAG, "core started")
        true
    } catch (e: Exception) {
        Log.e(TAG, "core start failed", e)
        discovery.stop()
        false
    }

    /** Reacts to the events of the core that change what the user sees. */
    private fun handle(event: MobileProto.Event) {
        when {
            event.type == MobileProto.EventType.EVENT_TYPE_PEER_ONLINE ||
                event.type == MobileProto.EventType.EVENT_TYPE_PEER_OFFLINE -> refreshStatus()
            event.hasEvent() && event.event.kind == Local.EventKind.EVENT_KIND_CLIP_DOWNLOAD -> notifyPending(event.event)
        }
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
    } catch (e: Exception) {
        Log.w(TAG, "cannot read the name of $deviceId: ${e.message}")
        ""
    }

    @SuppressLint("MissingPermission")
    private fun showPending(item: Local.ClipItem, device: String) {
        // The service runs without POST_NOTIFICATIONS; the notification is just hidden.
        NotificationManagerCompat.from(this).notify(
            HistoryNotifications.idOf(item.id),
            HistoryNotifications.pending(this, item, device),
        )
    }

    /** Saves the payloads of an item into the Downloads collection. */
    private fun download(itemId: String) {
        startInForeground(getString(R.string.history_downloading))
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
        NotificationManagerCompat.from(this).apply {
            cancel(HistoryNotifications.idOf(itemId))
            notify(HistoryNotifications.RESULT_ID, HistoryNotifications.result(this@UnisonService, saved))
        }
    }

    private fun refreshStatus() {
        statusJob?.cancel()
        statusJob = scope.launch {
            val text = statusText()
            show(text)
        }
    }

    private suspend fun statusText(): CharSequence = try {
        val status = withContext(Dispatchers.IO) { client.status() }
        val online = status.devicesList.count { it.online }
        Log.i(TAG, "core is up, device ${status.deviceId}")
        getString(
            R.string.service_status,
            status.deviceName,
            resources.getQuantityString(R.plurals.service_peers_online, online, online),
        )
    } catch (e: CoreException) {
        Log.w(TAG, "status failed: ${e.message}")
        getString(R.string.service_failed)
    } catch (e: Exception) {
        Log.w(TAG, "status failed: ${e.message}")
        getString(R.string.service_failed)
    }

    @SuppressLint("MissingPermission")
    private fun show(text: CharSequence) {
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

        /** Downloads one history entry. */
        const val ACTION_DOWNLOAD_ITEM = "app.unison.action.DOWNLOAD_ITEM"

        /** Entry the download action refers to. */
        const val EXTRA_ITEM_ID = "app.unison.extra.ITEM_ID"

        fun start(context: Context) {
            val intent = Intent(context, UnisonService::class.java).setAction(ACTION_START)
            ContextCompat.startForegroundService(context, intent)
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, UnisonService::class.java))
        }
    }
}
