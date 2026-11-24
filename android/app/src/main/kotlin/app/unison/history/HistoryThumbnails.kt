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
     * Longer side of the fullscreen viewer. The core allows up to 1024 px, and
     * the facade answers `GetHistoryThumbnail` up to 8 MiB, which holds
     * 1024 x 1024 x 4 bytes.
     */
    const val FULLSCREEN_SIDE = 1024

    /** Number of thumbnails kept in memory. */
    const val CACHE_ENTRIES = 64

    /** Largest thumbnail that stays in memory; a fullscreen image would not fit. */
    const val CACHE_MAX_SIDE = TILE_SIDE

    private const val PIXEL_BYTES = 4
    private const val RED_OFFSET = 0
    private const val GREEN_OFFSET = 1
    private const val BLUE_OFFSET = 2
    private const val ALPHA_OFFSET = 3
    private const val BYTE_MASK = 0xFF
    private const val ALPHA_SHIFT = 24
    private const val RED_SHIFT = 16
    private const val GREEN_SHIFT = 8
    private const val PREMULTIPLY_ROUNDING = 127
    private const val PREMULTIPLY_DIVISOR = 255

    /**
     * Pixels of a thumbnail as ARGB integers, or null when the response does
     * not describe a whole image. The core sends non-premultiplied RGBA, and
     * Android bitmaps are premultiplied, so the colors are multiplied here.
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
            val premultipliedRed = (red * alpha + PREMULTIPLY_ROUNDING) / PREMULTIPLY_DIVISOR
            val premultipliedGreen = (green * alpha + PREMULTIPLY_ROUNDING) / PREMULTIPLY_DIVISOR
            val premultipliedBlue = (blue * alpha + PREMULTIPLY_ROUNDING) / PREMULTIPLY_DIVISOR
            pixels[index] =
                (alpha shl ALPHA_SHIFT) or
                (premultipliedRed shl RED_SHIFT) or
                (premultipliedGreen shl GREEN_SHIFT) or
                premultipliedBlue
        }
        return pixels
    }

    /** Bitmap of a thumbnail, or null when the response is not a whole image. */
    fun bitmap(response: Local.GetHistoryThumbnailResponse): ImageBitmap? {
        val width = response.width
        val height = response.height
        val pixels = argbPixels(width, height, response.rgba.toByteArray()) ?: return null
        val bitmap = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
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
        if (bitmap != null && maxSide <= HistoryThumbnails.CACHE_MAX_SIDE) {
            mutex.withLock { cache[key] = bitmap }
        }
        return bitmap
    }

    private companion object {
        const val LOAD_FACTOR = 0.75f
    }
}
