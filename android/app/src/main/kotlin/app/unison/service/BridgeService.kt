package app.unison.service

import android.app.Service
import android.content.Intent
import android.os.IBinder
import app.unison.bridge.CallerVerifier
import app.unison.bridge.MobileBridgeCore
import app.unison.bridge.UnisonBridge
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel

/**
 * Bound service that serves the keyboard bridge.
 *
 * The signature permission keeps foreign apps from binding; every call is also
 * checked against the signature of its caller.
 */
class BridgeService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private var bridge: UnisonBridge? = null

    override fun onCreate() {
        super.onCreate()
        bridge = UnisonBridge(this, CallerVerifier.of(this), MobileBridgeCore(this), scope)
    }

    override fun onBind(intent: Intent?): IBinder? = bridge?.asBinder()

    override fun onDestroy() {
        bridge?.close()
        bridge = null
        scope.cancel()
        super.onDestroy()
    }
}
