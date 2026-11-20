package app.unison.pairing

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.core.CoreClient
import app.unison.core.CoreStarter
import app.unison.core.coreEvents
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeoutOrNull
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local
import java.io.File

/**
 * Pairs with the device of a pairing code that was put into the files of the
 * app, which stands in for the camera of a check without a screen to scan.
 */
@RunWith(AndroidJUnit4::class)
class PairingCodeFlowTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Test
    fun pairsWithTheDeviceOfThePushedCode() {
        val file = File(context.filesDir, CODE_FILE)
        assumeTrue("the code of the other device was not pushed", file.exists())
        val code = file.readText().trim()
        // The code is one shot, a later run must not use it again.
        file.delete()
        assertTrue("the code file is empty", code.isNotEmpty())
        assertTrue("the core did not start", runBlocking { CoreStarter.await(context, START_TIMEOUT_MS) })
        val core = CoreClient()

        runBlocking { core.scanPairingQr(code) }

        val paired = runBlocking { awaitPaired(PAIR_TIMEOUT_MS) }
        assertNotNull("the pairing did not complete", paired)
        val status = runBlocking { core.status() }
        assertTrue("the other device is not listed", status.devicesList.any { it.deviceId == paired?.deviceId })
    }

    /** Waits for the event of a completed pairing. */
    private suspend fun awaitPaired(timeoutMs: Long): Local.Event? =
        withTimeoutOrNull(timeoutMs) {
            coreEvents.events
                .filter { it.hasEvent() && it.event.kind == Local.EventKind.EVENT_KIND_PAIRING_PAIRED }
                .map { it.event }
                .first()
        }

    private companion object {
        const val CODE_FILE = "pairing-code.txt"
        const val START_TIMEOUT_MS = 30_000L
        const val PAIR_TIMEOUT_MS = 180_000L
    }
}
