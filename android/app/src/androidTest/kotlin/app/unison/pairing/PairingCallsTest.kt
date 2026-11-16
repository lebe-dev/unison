package app.unison.pairing

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.core.CoreClient
import app.unison.core.CoreException
import app.unison.core.CoreStarter
import app.unison.core.LocalAddresses
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

/** Exercises the pairing calls of the local API through the in-process core. */
@RunWith(AndroidJUnit4::class)
class PairingCallsTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Test
    fun windowOpensAndTheCodeIsReadBack() {
        assertTrue("the core did not start", runBlocking { CoreStarter.await(context, TIMEOUT_MS) })
        val core = CoreClient()
        runCatching { runBlocking { core.closePairingWindow() } }

        val window = runBlocking { core.openPairingWindow(Local.Role.ROLE_PERSONAL) }
        assertTrue("the window is not open", window.open)
        assertTrue("the window has no deadline", window.expiresAtUnixMs > System.currentTimeMillis())

        // The app cannot list the interfaces itself, so it hands its own
        // addresses to the code. A device without a network has none.
        val ipv4 = LocalAddresses(context).ipv4()
        assumeTrue("the device has no IPv4 address", ipv4.isNotEmpty())
        val port = window.addr.substringAfterLast(':')
        assertTrue("the window has no port: ${window.addr}", port.isNotEmpty())

        val code = runBlocking { core.pairingQr(ipv4.map { "$it:$port" }) }
        assertTrue(
            "the code is not a pairing code: ${code.uri.take(PREFIX_LENGTH)}",
            code.uri.startsWith(CODE_PREFIX),
        )
        assertTrue("the code proposes another role", code.uri.contains("&role=personal"))
        assertTrue("the code carries no address", code.uri.contains("&a="))

        // The same text the camera would read reaches the scan path of the core.
        runBlocking { core.scanPairingQr(code.uri) }

        runBlocking { core.closePairingWindow() }
        try {
            runBlocking { core.pairingQr() }
            fail("a closed window returned a code")
        } catch (e: CoreException) {
            assertTrue("unexpected code $e", e.code == CoreException.CODE_FAILED_PRECONDITION)
        }
    }

    @Test
    fun malformedCodeIsRefused() {
        assertTrue("the core did not start", runBlocking { CoreStarter.await(context, TIMEOUT_MS) })
        val core = CoreClient()

        try {
            runBlocking { core.scanPairingQr("not a pairing code") }
            fail("a malformed code was accepted")
        } catch (e: CoreException) {
            assertTrue("unexpected code $e", e.isInvalidArgument)
        }
    }

    private companion object {
        const val TIMEOUT_MS = 30_000L
        const val PREFIX_LENGTH = 24
        const val CODE_PREFIX = "unison://pair?v=1&id="
    }
}
