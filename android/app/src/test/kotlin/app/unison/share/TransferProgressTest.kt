package app.unison.share

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local

class TransferProgressTest {
    @Test
    fun runningOutgoingTransfersAreActive() {
        val transfers =
            listOf(
                transfer("a", Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING, Local.TransferState.TRANSFER_STATE_OFFERED),
                transfer("b", Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING, Local.TransferState.TRANSFER_STATE_TRANSFERRING),
                transfer("c", Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING, Local.TransferState.TRANSFER_STATE_DONE),
                transfer("d", Local.TransferDirection.TRANSFER_DIRECTION_INCOMING, Local.TransferState.TRANSFER_STATE_PENDING),
            )

        assertEquals(listOf("a", "b"), TransferProgress.active(transfers).map { it.id })
    }

    @Test
    fun percentSumsTheTransfers() {
        val transfers =
            listOf(
                transfer(
                    "a",
                    Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING,
                    Local.TransferState.TRANSFER_STATE_TRANSFERRING,
                    bytes = 30,
                    total = 100,
                ),
                transfer(
                    "b",
                    Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING,
                    Local.TransferState.TRANSFER_STATE_TRANSFERRING,
                    bytes = 20,
                    total = 100,
                ),
            )

        assertEquals(25, TransferProgress.percent(transfers))
        assertEquals(50, TransferProgress.bytes(transfers))
        assertEquals(200, TransferProgress.totalBytes(transfers))
    }

    @Test
    fun unknownSizeHasNoPercent() {
        val transfers =
            listOf(
                transfer("a", Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING, Local.TransferState.TRANSFER_STATE_OFFERED),
            )

        assertEquals(0, TransferProgress.percent(transfers))
    }

    @Test
    fun namesAreDistinctAndSorted() {
        val transfers =
            listOf(
                transfer(
                    "a",
                    Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING,
                    Local.TransferState.TRANSFER_STATE_TRANSFERRING,
                    name = "Mac",
                ),
                transfer(
                    "b",
                    Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING,
                    Local.TransferState.TRANSFER_STATE_TRANSFERRING,
                    name = "Laptop",
                ),
                transfer(
                    "c",
                    Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING,
                    Local.TransferState.TRANSFER_STATE_TRANSFERRING,
                    name = "Mac",
                ),
            )

        assertEquals(listOf("Laptop", "Mac"), TransferProgress.names(transfers))
        assertTrue(TransferProgress.names(emptyList()).isEmpty())
    }

    private fun transfer(
        id: String,
        direction: Local.TransferDirection,
        state: Local.TransferState,
        bytes: Long = 0,
        total: Long = 0,
        name: String = "Mac",
    ): Local.Transfer =
        Local.Transfer
            .newBuilder()
            .setId(id)
            .setDirection(direction)
            .setState(state)
            .setBytes(bytes)
            .setTotalBytes(total)
            .setDeviceName(name)
            .setDeviceId("ab".repeat(32))
            .build()
}
