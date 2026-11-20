package app.unison.policy

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local

class PolicyProtoTest {
    @Test
    fun tableOfTheCoreIsReadBack() {
        val settings =
            Local.Settings
                .newBuilder()
                .setUnmetered(unmeteredRow())
                .setMetered(restrictedRow())
                .setRoaming(restrictedRow().toBuilder().setRelay(true))
                .setDataSaver(restrictedRow())
                .setRelayMaxSizeBytes(2 * PolicySize.MEGABYTE)
                .build()

        val table = PolicyProto.table(settings)

        assertEquals(
            PolicyMode.CONFIRM,
            table[PolicyCondition.ROAMING][PolicyKind.IMAGE].mode,
        )
        assertEquals(
            4 * PolicySize.MEGABYTE,
            table[PolicyCondition.ROAMING][PolicyKind.IMAGE].maxSizeBytes,
        )
        assertTrue(table[PolicyCondition.ROAMING].relay)
        assertEquals(2 * PolicySize.MEGABYTE, table.relayMaxSizeBytes)
        assertEquals(PolicyMode.AUTO, table[PolicyCondition.UNMETERED][PolicyKind.TEXT].mode)
        assertEquals(PolicyMode.AUTO, table[PolicyCondition.METERED][PolicyKind.TEXT].mode)
        assertFalse(table[PolicyCondition.DATA_SAVER].relay)
    }

    @Test
    fun requestCarriesTheWholeTable() {
        val table =
            PolicyDefaults.config
                .with(
                    PolicyCondition.METERED,
                    PolicyDefaults.config[PolicyCondition.METERED]
                        .with(PolicyKind.IMAGE, PolicyCell(PolicyMode.CONFIRM, PolicySize.ofMegabytes(4))),
                ).copy(relayMaxSizeBytes = PolicySize.ofMegabytes(3))

        val request = PolicyProto.request(table)
        val settings =
            Local.Settings
                .newBuilder()
                .setUnmetered(request.unmetered)
                .setMetered(request.metered)
                .setRoaming(request.roaming)
                .setDataSaver(request.dataSaver)
                .setRelayMaxSizeBytes(request.relayMaxSizeBytes)
                .build()

        assertEquals(table, PolicyProto.table(settings))
        assertEquals(3 * PolicySize.MEGABYTE, request.relayMaxSizeBytes)
    }

    @Test
    fun missingRowsFallBackToTheDefaults() {
        val table = PolicyProto.table(Local.Settings.getDefaultInstance())

        assertEquals(PolicyDefaults.config, table)
    }

    @Test
    fun resetAsksTheCoreToDropTheOverrides() {
        assertTrue(PolicyProto.reset().resetPolicy)
    }

    private fun unmeteredRow(): Local.PolicyRow =
        Local.PolicyRow
            .newBuilder()
            .setText(cell(Local.PolicyAction.POLICY_ACTION_AUTO, 10))
            .setImage(cell(Local.PolicyAction.POLICY_ACTION_AUTO, 10))
            .setFiles(cell(Local.PolicyAction.POLICY_ACTION_METADATA, 10))
            .setRelay(true)
            .build()

    private fun restrictedRow(): Local.PolicyRow =
        Local.PolicyRow
            .newBuilder()
            .setText(cell(Local.PolicyAction.POLICY_ACTION_AUTO, 1))
            .setImage(cell(Local.PolicyAction.POLICY_ACTION_CONFIRM, 4))
            .setFiles(cell(Local.PolicyAction.POLICY_ACTION_METADATA, 1))
            .setRelay(false)
            .build()

    private fun cell(
        action: Local.PolicyAction,
        megabytes: Int,
    ): Local.PolicyCell =
        Local.PolicyCell
            .newBuilder()
            .setAction(action)
            .setMaxSizeBytes(megabytes * PolicySize.MEGABYTE)
            .build()
}
