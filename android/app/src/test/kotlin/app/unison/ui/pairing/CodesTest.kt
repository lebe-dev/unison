package app.unison.ui.pairing

import com.google.zxing.BinaryBitmap
import com.google.zxing.RGBLuminanceSource
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CodesTest {

    @Test
    fun codeIsDrawnBackToTheSameText() {
        val uri = pairingUri()

        val modules = pairingCodeModules(uri)
        assertNotNull(modules)
        val rows = modules ?: return
        assertEquals(rows.size, rows.first().size)

        val decoded = QRCodeReader().decode(BinaryBitmap(HybridBinarizer(pixelsOf(rows))))
        assertEquals(uri, decoded.text)
    }

    @Test
    fun longestCodeStillFits() {
        val uri = pairingUri().padEnd(MAX_CODE_LENGTH, 'a')

        val modules = pairingCodeModules(uri)
        assertNotNull(modules)
        assertTrue((modules?.size ?: 0) > 0)
    }

    @Test
    fun digitsAreGroupedByThree() {
        assertEquals("123 456", formatSasDigits("123456"))
        assertEquals("123 45", formatSasDigits("12345"))
    }

    @Test
    fun remainingTimeIsMinutesAndSeconds() {
        assertEquals("3:00", formatRemaining(180_000))
        assertEquals("2:35", formatRemaining(155_000))
        assertEquals("0:01", formatRemaining(1))
        assertEquals("0:00", formatRemaining(0))
        assertEquals("0:00", formatRemaining(-5))
    }

    private fun pairingUri(): String =
        "unison://pair?v=1" +
            "&id=" + "ab".repeat(DEVICE_ID_BYTES) +
            "&fp=" + "cd".repeat(FINGERPRINT_BYTES) +
            "&t=" + "ef".repeat(TOKEN_BYTES) +
            "&a=192.168.1.5:41234,[fd00::1]:41234" +
            "&role=personal"

    private fun pixelsOf(modules: Array<BooleanArray>): RGBLuminanceSource {
        val cells = modules.size + QUIET_ZONE * 2
        val total = cells * SCALE
        val pixels = IntArray(total * total) { WHITE }
        for (y in modules.indices) {
            for (x in modules[y].indices) {
                if (!modules[y][x]) {
                    continue
                }
                val left = (x + QUIET_ZONE) * SCALE
                val top = (y + QUIET_ZONE) * SCALE
                for (dy in 0 until SCALE) {
                    for (dx in 0 until SCALE) {
                        pixels[(top + dy) * total + left + dx] = BLACK
                    }
                }
            }
        }
        return RGBLuminanceSource(total, total, pixels)
    }

    private companion object {
        const val QUIET_ZONE = 4
        const val SCALE = 4
        const val WHITE = 0xFFFFFFFF.toInt()
        const val BLACK = 0xFF000000.toInt()
        const val DEVICE_ID_BYTES = 32
        const val FINGERPRINT_BYTES = 32
        const val TOKEN_BYTES = 16
        const val MAX_CODE_LENGTH = 512
    }
}
