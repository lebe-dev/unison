package app.unison.storage

import android.content.ContentUris
import android.content.Context
import android.net.Uri
import android.os.ParcelFileDescriptor
import android.provider.MediaStore
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import java.io.FileOutputStream
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancel
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import unison.mobile.v1.Mobile

@RunWith(AndroidJUnit4::class)
class DownloadsTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Test
    fun fileIsSavedIntoTheDownloadsCollection() {
        val name = uniqueName("txt")
        val file = File(context.cacheDir, name).apply { writeText(CONTENT) }

        try {
            assertTrue(Downloads.save(context, file, name, "text/plain"))

            val uri = find(name)
            assertNotNull("no entry for $name", uri)
            assertEquals(CONTENT, read(uri!!))
        } finally {
            find(name)?.let { context.contentResolver.delete(it, null, null) }
            file.delete()
        }
    }

    @Test
    fun nameGetsTheExtensionOfTheMimeType() {
        assertEquals("image.png", Downloads.nameWithExtension("image", "image/png"))
        assertEquals("photo.jpg", Downloads.nameWithExtension("photo.jpg", "image/png"))
        assertEquals("unison", Downloads.nameWithExtension("", "unknown/type"))
        assertEquals("clip", Downloads.nameWithExtension("../clip", "unknown/type"))
    }

    @Test
    fun sinkCopiesThePayloadOfAnIncomingFile() {
        val name = uniqueName("txt")
        val scope = CoroutineScope(Dispatchers.IO)
        try {
            val sink = BlobSink(context, scope)
            val fd = sink.open(meta(name, CONTENT.length.toLong()))
            assertTrue("sink returned $fd", fd >= 0)

            ParcelFileDescriptor.adoptFd(fd.toInt()).use { descriptor ->
                FileOutputStream(descriptor.fileDescriptor).use { it.write(CONTENT.toByteArray()) }
            }

            val uri = await(name)
            assertNotNull("no entry for $name", uri)
            assertEquals(CONTENT, read(uri!!))
            context.contentResolver.delete(uri, null, null)
        } finally {
            scope.cancel()
        }
    }

    @Test
    fun sinkDropsAPayloadOfAnotherSize() {
        val name = uniqueName("txt")
        val scope = CoroutineScope(Dispatchers.IO)
        try {
            val sink = BlobSink(context, scope)
            val fd = sink.open(meta(name, CONTENT.length + 10L))
            assertTrue(fd >= 0)

            ParcelFileDescriptor.adoptFd(fd.toInt()).use { descriptor ->
                FileOutputStream(descriptor.fileDescriptor).use { it.write(CONTENT.toByteArray()) }
            }

            Thread.sleep(SETTLE_MS)
            assertNull("the entry of another size was published", find(name))
        } finally {
            scope.cancel()
        }
    }

    /** Waits until the entry appears. */
    private fun await(name: String): Uri? {
        val deadline = System.currentTimeMillis() + TIMEOUT_MS
        while (System.currentTimeMillis() < deadline) {
            find(name)?.let { return it }
            Thread.sleep(POLL_MS)
        }
        return null
    }

    /** Finds an entry of the app by its display name. */
    private fun find(name: String): Uri? {
        val collection = MediaStore.Downloads.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY)
        val projection = arrayOf(MediaStore.Downloads._ID)
        val selection = "${MediaStore.Downloads.DISPLAY_NAME} = ?"
        context.contentResolver.query(collection, projection, selection, arrayOf(name), null)?.use { cursor ->
            if (!cursor.moveToFirst()) {
                return null
            }
            return ContentUris.withAppendedId(collection, cursor.getLong(0))
        }
        return null
    }

    /** Reads the content of an entry. */
    private fun read(uri: Uri): String =
        context.contentResolver.openInputStream(uri)!!.use { it.readBytes().decodeToString() }

    private fun meta(name: String, size: Long): ByteArray = Mobile.FilePayload.newBuilder()
        .setFileName(name)
        .setMimeType("text/plain")
        .setSize(size)
        .build()
        .toByteArray()

    private fun uniqueName(extension: String): String =
        "unison-test-${System.currentTimeMillis()}.$extension"

    private companion object {
        const val CONTENT = "hello from unison"
        const val TIMEOUT_MS = 5000L
        const val POLL_MS = 50L
        const val SETTLE_MS = 500L
    }
}
