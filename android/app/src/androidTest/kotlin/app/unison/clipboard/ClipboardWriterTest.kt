package app.unison.clipboard

import android.Manifest
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.util.Base64
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import app.unison.bridge.BridgeContract
import com.google.protobuf.ByteString
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import unison.mobile.v1.Mobile

@RunWith(AndroidJUnit4::class)
class ClipboardWriterTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Test
    fun imageIsStoredAndExposedThroughTheFileProvider() {
        val payload = Mobile.ClipPayload.newBuilder()
            .setMimeType("image/png")
            .setData(ByteString.copyFrom(PNG))
            .build()

        ClipboardWriter.write(context, payload.toByteArray())

        val clip = primaryClip()
        assertNotNull("the clipboard is empty", clip)
        val uri = clip!!.getItemAt(0).uri
        assertNotNull("the clipboard holds no uri", uri)
        assertEquals("content", uri!!.scheme)
        assertEquals("app.unison.files", uri.authority)
        assertEquals("image/png", clip.description.getMimeType(0))
        assertTrue("uri ${uri.path} is outside the cache", uri.path.orEmpty().startsWith("/clipboard/"))

        val stored = context.contentResolver.openInputStream(uri)!!.use { it.readBytes() }
        assertArrayEquals(PNG, stored)
    }

    @Test
    fun textIsStillWritten() {
        val payload = Mobile.ClipPayload.newBuilder()
            .addParts(
                Mobile.ClipPart.newBuilder()
                    .setMimeType("text/plain")
                    .setData(ByteString.copyFromUtf8("hello")),
            )
            .build()

        ClipboardWriter.write(context, payload.toByteArray())

        val clip = primaryClip()
        assertNotNull(clip)
        assertEquals("hello", clip!!.getItemAt(0).text.toString())
    }

    @Test
    fun textNamesTheItemAndTheApp() {
        val payload = Mobile.ClipPayload.newBuilder()
            .setItemId(ITEM_ID)
            .addParts(
                Mobile.ClipPart.newBuilder()
                    .setMimeType("text/plain")
                    .setData(ByteString.copyFromUtf8("tagged")),
            )
            .build()

        ClipboardWriter.write(context, payload.toByteArray())

        val clip = primaryClip()
        assertNotNull(clip)
        assertEquals(BridgeContract.CLIP_LABEL, clip!!.description.label.toString())
        assertEquals(ITEM_ID, clip.description.extras?.getString(BridgeContract.EXTRA_ITEM_ID))
    }

    @Test
    fun imageNamesTheItemAndTheApp() {
        val payload = Mobile.ClipPayload.newBuilder()
            .setItemId(ITEM_ID)
            .setMimeType("image/png")
            .setData(ByteString.copyFrom(PNG))
            .build()

        ClipboardWriter.write(context, payload.toByteArray())

        val clip = primaryClip()
        assertNotNull(clip)
        assertEquals(BridgeContract.CLIP_LABEL, clip!!.description.label.toString())
        assertEquals(ITEM_ID, clip.description.extras?.getString(BridgeContract.EXTRA_ITEM_ID))
    }

    @Test
    fun contentWithoutAnItemHasNoItemExtra() {
        val payload = Mobile.ClipPayload.newBuilder()
            .addParts(
                Mobile.ClipPart.newBuilder()
                    .setMimeType("text/plain")
                    .setData(ByteString.copyFromUtf8("untagged")),
            )
            .build()

        ClipboardWriter.write(context, payload.toByteArray())

        val clip = primaryClip()
        assertNotNull(clip)
        assertNull(clip!!.description.extras?.getString(BridgeContract.EXTRA_ITEM_ID))
    }

    /** Reads the primary clip, asking the shell for background access if needed. */
    private fun primaryClip(): ClipData? {
        val manager = context.getSystemService(ClipboardManager::class.java)
        manager?.primaryClip?.let { return it }

        val automation = InstrumentationRegistry.getInstrumentation().uiAutomation
        automation.adoptShellPermissionIdentity(READ_CLIPBOARD_IN_BACKGROUND)
        return try {
            manager?.primaryClip
        } finally {
            automation.dropShellPermissionIdentity()
        }
    }

    private companion object {
        /** Shell permission that lifts the background read restriction. */
        const val READ_CLIPBOARD_IN_BACKGROUND = "android.permission.READ_CLIPBOARD_IN_BACKGROUND"

        const val ITEM_ID = "0f8fad5b-d9cb-469f-a165-70867728950e"

        /** One transparent pixel. */
        val PNG: ByteArray = Base64.decode(
            "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8AAAwAB/wFhHkAAAAAASUVORK5CYII=",
            Base64.NO_WRAP,
        )
    }
}
