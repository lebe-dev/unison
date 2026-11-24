package app.unison.history

import android.graphics.Bitmap
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import app.unison.core.CoreException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import unison.local.v1.Local
import kotlin.coroutines.cancellation.CancellationException

/** Loads the pixels of an item, or null when the core has none. */
typealias HistoryThumbnailLoader = suspend (id: String, maxSide: Int) -> ImageBitmap?

/** Thumbnails the core decodes, as bitmaps the screens draw. */
object HistoryThumbnails {
    /** Longer side of a tile of the image grid. */
    const val TILE_SIDE = 256

    /**
     * Longer side of the preview. The facade answers at most 1 MiB, and
     * 480 x 480 x 4 bytes is the largest square that fits.
     */
    const val PREVIEW_SIDE = 480

    /** Number of thumbnails kept in memory. */
    const val CACHE_ENTRIES = 64

    private const val PIXEL_BYTES = 4
    private const val RED_OFFSET = 0
    private const val GREEN_OFFSET = 1
    private const val BLUE_OFFSET = 2
    private const val ALPHA_OFFSET = 3
    private const val BYTE_MASK = 0xFF
    private const val ALPHA_SHIFT = 24
    private const val RED_SHIFT = 16
    private const val GREEN_SHIFT = 8

    /**
     * Pixels of a thumbnail as ARGB integers, or null when the response does
     * not describe a whole image. The core sends non-premultiplied RGBA.
     */
    fun argbPixels(response: Local.GetHistoryThumbnailResponse): IntArray? =
        argbPixels(response.width, response.height, response.rgba.toByteArray())

    /** Pixels of a thumbnail as ARGB integers, or null when the size does not fit the bytes. */
    fun argbPixels(
        width: Int,
        height: Int,
        rgba: ByteArray,
    ): IntArray? {
        if (width <= 0 || height <= 0) {
            return null
        }
        if (rgba.size != width * height * PIXEL_BYTES) {
            return null
        }
        val pixels = IntArray(width * height)
        for (index in pixels.indices) {
            val offset = index * PIXEL_BYTES
            val red = rgba[offset + RED_OFFSET].toInt() and BYTE_MASK
            val green = rgba[offset + GREEN_OFFSET].toInt() and BYTE_MASK
            val blue = rgba[offset + BLUE_OFFSET].toInt() and BYTE_MASK
            val alpha = rgba[offset + ALPHA_OFFSET].toInt() and BYTE_MASK
            pixels[index] = (alpha shl ALPHA_SHIFT) or (red shl RED_SHIFT) or (green shl GREEN_SHIFT) or blue
        }
        return pixels
    }

    /** Bitmap of a thumbnail, or null when the response is not a whole image. */
    fun bitmap(response: Local.GetHistoryThumbnailResponse): ImageBitmap? {
        val width = response.width
        val height = response.height
        val pixels = argbPixels(width, height, response.rgba.toByteArray()) ?: return null
        val bitmap = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
        // The core sends the pixels as they are, so the bitmap tells the
        // renderer not to premultiply the colors again.
        bitmap.setPremultiplied(false)
        bitmap.setPixels(pixels, 0, width, 0, 0, width, height)
        return bitmap.asImageBitmap()
    }

    /** Cache of the thumbnails of this device. */
    fun cache(load: suspend (id: String, maxSide: Int) -> Local.GetHistoryThumbnailResponse): HistoryThumbnailCache =
        HistoryThumbnailCache(load)
}

/** Keeps the thumbnails that were drawn lately, so the grid does not ask again. */
class HistoryThumbnailCache(
    private val load: suspend (id: String, maxSide: Int) -> Local.GetHistoryThumbnailResponse,
    private val maxEntries: Int = HistoryThumbnails.CACHE_ENTRIES,
) {
    private val mutex = Mutex()

    private val cache =
        object : LinkedHashMap<String, ImageBitmap>(maxEntries, LOAD_FACTOR, true) {
            override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, ImageBitmap>): Boolean = size > maxEntries
        }

    /** Bitmap of the item, null when the core has no thumbnail or the call fails. */
    suspend fun thumbnail(
        id: String,
        maxSide: Int,
    ): ImageBitmap? {
        if (id.isEmpty()) {
            return null
        }
        val key = "$maxSide/$id"
        mutex.withLock { cache[key] }?.let { return it }
        val bitmap =
            try {
                HistoryThumbnails.bitmap(load(id, maxSide))
            } catch (e: CancellationException) {
                throw e
            } catch (_: CoreException) {
                null
            }
        if (bitmap != null) {
            mutex.withLock { cache[key] = bitmap }
        }
        return bitmap
    }

    private companion object {
        const val LOAD_FACTOR = 0.75f
    }
}
