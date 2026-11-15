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
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import unison.mobile.v1.Mobile as MobileProto

/** Keeps the core running in the foreground. */
class UnisonService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var coreStarted = false
    private var statusJob: Job? = null

    override fun onCreate() {
        super.onCreate()
        NotificationChannels.create(this)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopSelf()
            return START_NOT_STICKY
        }

        startInForeground(getString(R.string.service_starting))
        startCore()
        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        scope.cancel()
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
                coreEvents.events.collect { event ->
                    if (event.type == MobileProto.EventType.EVENT_TYPE_PEER_ONLINE ||
                        event.type == MobileProto.EventType.EVENT_TYPE_PEER_OFFLINE
                    ) {
                        refreshStatus()
                    }
                }
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
        Mobile.start(config.toByteArray(), CorePlatform(this))
        Log.i(TAG, "core started")
        true
    } catch (e: Exception) {
        Log.e(TAG, "core start failed", e)
        false
    }

    private fun refreshStatus() {
        statusJob?.cancel()
        statusJob = scope.launch {
            val text = statusText()
            show(text)
        }
    }

    private suspend fun statusText(): CharSequence = try {
        val status = withContext(Dispatchers.IO) { CoreClient().status() }
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

        fun start(context: Context) {
            val intent = Intent(context, UnisonService::class.java).setAction(ACTION_START)
            ContextCompat.startForegroundService(context, intent)
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, UnisonService::class.java))
        }
    }
}
