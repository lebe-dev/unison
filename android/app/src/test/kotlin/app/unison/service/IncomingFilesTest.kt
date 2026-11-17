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

    private fun transfer(direction: Local.TransferDirection): Local.Event = Local.Event.newBuilder()
        .setKind(Local.EventKind.EVENT_KIND_TRANSFER)
        .setTransfer(Local.Transfer.newBuilder().setDirection(direction))
        .build()
}
