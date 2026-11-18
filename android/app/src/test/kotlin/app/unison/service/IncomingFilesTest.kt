package app.unison.service

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local

class IncomingFilesTest {

    @Test
    fun incomingTransferBringsAFile() {
        val event = transfer(Local.TransferDirection.TRANSFER_DIRECTION_INCOMING)

        assertTrue(IncomingFiles.arrives(event))
    }

    @Test
    fun outgoingTransferDoesNot() {
        val event = transfer(Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING)

        assertFalse(IncomingFiles.arrives(event))
    }

    @Test
    fun otherEventsDoNot() {
        val event = Local.Event.newBuilder()
            .setKind(Local.EventKind.EVENT_KIND_CLIPBOARD)
            .setTransfer(Local.Transfer.newBuilder().setDirection(Local.TransferDirection.TRANSFER_DIRECTION_INCOMING))
            .build()

        assertFalse(IncomingFiles.arrives(event))
    }

    @Test
    fun newOfferChangesTheRequests() {
        val event = transfer(Local.TransferDirection.TRANSFER_DIRECTION_INCOMING, Local.TransferState.TRANSFER_STATE_PENDING)

        assertTrue(IncomingFiles.changesRequests(event, shown = emptySet()))
    }

    @Test
    fun answeredOfferChangesTheRequests() {
        val event = transfer(Local.TransferDirection.TRANSFER_DIRECTION_INCOMING, Local.TransferState.TRANSFER_STATE_DECLINED)

        assertTrue(IncomingFiles.changesRequests(event, shown = setOf(ID)))
    }

    @Test
    fun progressDoesNotChangeTheRequests() {
        val event = transfer(Local.TransferDirection.TRANSFER_DIRECTION_INCOMING, Local.TransferState.TRANSFER_STATE_TRANSFERRING)

        assertFalse(IncomingFiles.changesRequests(event, shown = emptySet()))
    }

    @Test
    fun outgoingTransferDoesNotChangeTheRequests() {
        val event = transfer(Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING, Local.TransferState.TRANSFER_STATE_PENDING)

        assertFalse(IncomingFiles.changesRequests(event, shown = setOf(ID)))
    }

    @Test
    fun outgoingTransferLeaves() {
        assertTrue(IncomingFiles.leaves(transfer(Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING)))
        assertFalse(IncomingFiles.leaves(transfer(Local.TransferDirection.TRANSFER_DIRECTION_INCOMING)))
    }

    private fun transfer(
        direction: Local.TransferDirection,
        state: Local.TransferState = Local.TransferState.TRANSFER_STATE_TRANSFERRING,
    ): Local.Event = Local.Event.newBuilder()
        .setKind(Local.EventKind.EVENT_KIND_TRANSFER)
        .setTransfer(Local.Transfer.newBuilder().setId(ID).setDirection(direction).setState(state))
        .build()

    private companion object {
        const val ID = "transfer-1"
    }
}
