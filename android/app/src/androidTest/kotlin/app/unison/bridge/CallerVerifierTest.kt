package app.unison.bridge

import android.content.Context
import android.os.Process
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

/** The bridge accepts this app only and refuses an app signed with another key. */
@RunWith(AndroidJUnit4::class)
class CallerVerifierTest {
    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Test
    fun ownAppIsAccepted() {
        val verifier = CallerVerifier.of(context)
        verifier.verify(Process.myUid())
        assertTrue(verifier.isSignedLikeApp(Process.myUid()))
    }

    @Test
    fun packageOfAForeignKeyIsRefused() {
        val uid = context.packageManager.getApplicationInfo(FOREIGN_PACKAGE, 0).uid
        val verifier = CallerVerifier.of(context)
        assertThrows(SecurityException::class.java) { verifier.verify(uid) }
    }

    @Test
    fun packageOfTheSameKeyIsAccepted() {
        val uid = context.packageManager.getApplicationInfo(TEST_PACKAGE, 0).uid
        val verifier = CallerVerifier.of(context)
        verifier.verify(uid)
        assertTrue(verifier.isSignedLikeApp(uid))
    }

    private companion object {
        /** Package of the platform, signed with a key of the system. */
        private const val FOREIGN_PACKAGE = "android"

        /** Package of the test client, signed with the key of the app. */
        private const val TEST_PACKAGE = "app.unison.test"
    }
}
