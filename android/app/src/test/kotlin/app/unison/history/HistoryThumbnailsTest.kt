package app.unison.history

import com.google.protobuf.ByteString
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local

class HistoryThumbnailsTest {
    @Test
    fun pixelsBecomePremultipliedArgb() {
        val rgba =
            byteArrayOf(
                0x10,
                0x20,
                0x30,
                0xFF.toByte(),
                0x01,
                0x02,
                0x03,
                0x00,
            )

        val pixels = HistoryThumbnails.argbPixels(width = 2, height = 1, rgba = rgba)

        assertArrayEquals(
            intArrayOf(
                0xFF102030.toInt(),
                0x00000000,
            ),
            pixels,
        )
    }

    @Test
    fun transparentPixelsArePremultiplied() {
        val rgba = byteArrayOf(0x7F, 0x7F, 0x7F, 0x7F)

        val pixels = HistoryThumbnails.argbPixels(width = 1, height = 1, rgba = rgba)

        assertArrayEquals(intArrayOf(0x7F3F3F3F), pixels)
    }

    @Test
    fun fullyTransparentPixelsCarryNoColor() {
        val rgba = byteArrayOf(0xFF.toByte(), 0x80.toByte(), 0x40, 0x00)

        val pixels = HistoryThumbnails.argbPixels(width = 1, height = 1, rgba = rgba)

        assertArrayEquals(intArrayOf(0x00000000), pixels)
    }

    @Test
    fun responseSizeMustFitTheBytes() {
        assertNull(HistoryThumbnails.argbPixels(width = 2, height = 2, rgba = ByteArray(8)))
        assertNull(HistoryThumbnails.argbPixels(width = 1, height = 1, rgba = ByteArray(8)))
    }

    @Test
    fun emptyAndNegativeSizesAreRefused() {
        assertNull(HistoryThumbnails.argbPixels(width = 0, height = 1, rgba = ByteArray(0)))
        assertNull(HistoryThumbnails.argbPixels(width = -1, height = 1, rgba = ByteArray(0)))
        assertNull(HistoryThumbnails.argbPixels(width = 1, height = 0, rgba = ByteArray(0)))
    }

    @Test
    fun responseIsConverted() {
        val response =
            Local.GetHistoryThumbnailResponse
                .newBuilder()
                .setWidth(1)
                .setHeight(1)
                .setRgba(ByteString.copyFrom(byteArrayOf(0x10, 0x20, 0x30, 0xFF.toByte())))
                .build()

        val pixels = HistoryThumbnails.argbPixels(response)

        assertArrayEquals(intArrayOf(0xFF102030.toInt()), pixels)
    }

    @Test
    fun brokenResponseIsRefused() {
        val response =
            Local.GetHistoryThumbnailResponse
                .newBuilder()
                .setWidth(1)
                .setHeight(1)
                .setRgba(ByteString.copyFrom(byteArrayOf(0x10)))
                .build()

        assertNull(HistoryThumbnails.argbPixels(response))
    }

    @Test
    fun previewSideFitsTheFacadeCap() {
        assertEquals(480, HistoryThumbnails.PREVIEW_SIDE)
        assertTrue(HistoryThumbnails.PREVIEW_SIDE * HistoryThumbnails.PREVIEW_SIDE * 4 <= 1 shl 20)
    }
}
