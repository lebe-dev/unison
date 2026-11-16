package app.unison.bridge

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test
import java.io.ByteArrayInputStream

class BridgeBinaryTest {

    @Test
    fun readsContentWithinTheLimit() {
        val data = ByteArray(1024) { (it % 251).toByte() }
        val read = BridgeBinary.read(ByteArrayInputStream(data), 1024)
        assertNotNull(read)
        assertArrayEquals(data, read)
    }

    @Test
    fun readsAnEmptyStream() {
        val read = BridgeBinary.read(ByteArrayInputStream(ByteArray(0)), 1024)
        assertNotNull(read)
        assertArrayEquals(ByteArray(0), read)
    }

    @Test
    fun rejectsContentAboveTheLimit() {
        val data = ByteArray(1025)
        assertNull(BridgeBinary.read(ByteArrayInputStream(data), 1024))
    }
}
