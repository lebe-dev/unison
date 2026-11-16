package app.unison.bridge

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.core.CoreClient
import app.unison.core.CoreStarter
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.CountDownLatch
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit
import unison.local.v1.Local

/**
 * The test client is a separate app signed with the same key, so the permission
 * and the signature check both have to accept it.
 */
@RunWith(AndroidJUnit4::class)
class BridgeServiceTest {

    private val context: Context = ApplicationProvider.getApplicationContext()

    private lateinit var connection: ServiceConnection

    @Test
    fun testClientBindsAndReadsTheVersion() {
        val bridge = IUnisonBridge.Stub.asInterface(bind())
        try {
            assertEquals(BridgeContract.PROTOCOL_VERSION, bridge.protocolVersion())
            assertTrue(bridge.recentRemoteItems(0).isEmpty())
        } finally {
            context.unbindService(connection)
        }
    }

    @Test
    fun publishesTextIntoTheCore() {
        val core = CoreClient()
        assertTrue("the core did not start", runBlocking { CoreStarter.await(context, START_MS) })
        runBlocking { core.setPause(Local.PauseMode.PAUSE_MODE_OFF, 0) }

        val text = "bridge probe ${System.currentTimeMillis()}"
        val bridge = IUnisonBridge.Stub.asInterface(bind())
        try {
            bridge.publishText(ClipPayload(text, null, "text/plain", text.length.toLong()))
            assertTrue("the item did not reach the history", runBlocking { awaitHistory(core, text) })
        } finally {
            context.unbindService(connection)
        }
    }

    @Test
    fun reportsTheResultOfAPublish() {
        val core = CoreClient()
        assertTrue("the core did not start", runBlocking { CoreStarter.await(context, START_MS) })
        runBlocking { core.setPause(Local.PauseMode.PAUSE_MODE_OFF, 0) }

        val results = LinkedBlockingQueue<PublishResult>()
        val callback = object : IPublishCallback.Stub() {
            override fun onPublishResult(result: PublishResult?) {
                result?.let(results::put)
            }
        }
        val text = "bridge result ${System.currentTimeMillis()}"
        val bridge = IUnisonBridge.Stub.asInterface(bind())
        try {
            bridge.publishTextWithResult(
                ClipPayload(text, null, "text/plain", text.length.toLong()),
                callback,
            )
            val result = results.poll(WAIT_S, TimeUnit.SECONDS)
            assertEquals(BridgeContract.PUBLISH_ACCEPTED, result?.code)
            assertEquals("text/plain", result?.mime)
        } finally {
            context.unbindService(connection)
        }
    }

    /** Waits until the text shows up in the history. */
    private suspend fun awaitHistory(core: CoreClient, text: String): Boolean {
        val deadline = System.currentTimeMillis() + START_MS
        while (System.currentTimeMillis() < deadline) {
            val items = core.listHistory(limit = 10).itemsList
            val found = items.any { item ->
                item.item.representationsList.any { it.inlineBytes.toStringUtf8() == text }
            }
            if (found) {
                return true
            }
            delay(POLL_MS)
        }
        return false
    }

    /** Binds to the service and returns its binder. */
    private fun bind(): IBinder {
        val connected = CountDownLatch(1)
        var binder: IBinder? = null
        connection = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName?, service: IBinder?) {
                binder = service
                connected.countDown()
            }

            override fun onServiceDisconnected(name: ComponentName?) = Unit
        }
        val intent = Intent(BridgeContract.ACTION_BIND)
            .setComponent(ComponentName(BridgeContract.PACKAGE, BridgeContract.SERVICE_CLASS))
        assertTrue("the bridge service is not bindable", context.bindService(intent, connection, Context.BIND_AUTO_CREATE))
        assertTrue("the bridge service did not connect", connected.await(WAIT_S, TimeUnit.SECONDS))
        return binder ?: throw AssertionError("the bridge service returned no binder")
    }

    private companion object {
        private const val WAIT_S = 10L
        private const val START_MS = 20_000L
        private const val POLL_MS = 200L
    }
}
