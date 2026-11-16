package app.unison.policy

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local

class PolicySettingsTest {

    @Test
    fun defaultsMatchTheTable() {
        val table = PolicyDefaults.config

        assertCell(table, PolicyCondition.UNMETERED, PolicyKind.TEXT, PolicyMode.AUTO, 10)
        assertCell(table, PolicyCondition.UNMETERED, PolicyKind.IMAGE, PolicyMode.AUTO, 10)
        assertCell(table, PolicyCondition.UNMETERED, PolicyKind.FILES, PolicyMode.METADATA, 10)

        assertCell(table, PolicyCondition.METERED, PolicyKind.TEXT, PolicyMode.AUTO, 1)
        assertCell(table, PolicyCondition.METERED, PolicyKind.IMAGE, PolicyMode.AUTO, 1)
        assertCell(table, PolicyCondition.METERED, PolicyKind.FILES, PolicyMode.METADATA, 1)

        assertCell(table, PolicyCondition.ROAMING, PolicyKind.TEXT, PolicyMode.AUTO, 1)
        assertCell(table, PolicyCondition.ROAMING, PolicyKind.IMAGE, PolicyMode.METADATA, 1)
        assertCell(table, PolicyCondition.ROAMING, PolicyKind.FILES, PolicyMode.METADATA, 1)

        assertCell(table, PolicyCondition.DATA_SAVER, PolicyKind.TEXT, PolicyMode.AUTO, 1)
        assertCell(table, PolicyCondition.DATA_SAVER, PolicyKind.IMAGE, PolicyMode.METADATA, 1)
        assertCell(table, PolicyCondition.DATA_SAVER, PolicyKind.FILES, PolicyMode.METADATA, 1)
    }

    @Test
    fun relayIsAllowedOnlyWithoutRestrictions() {
        val table = PolicyDefaults.config

        assertTrue(table[PolicyCondition.UNMETERED].relay)
        assertTrue(table[PolicyCondition.METERED].relay)
        assertFalse(table[PolicyCondition.ROAMING].relay)
        assertFalse(table[PolicyCondition.DATA_SAVER].relay)
        assertEquals(PolicyDefaults.RELAY_MAX_SIZE, table.relayMaxSizeBytes)
    }

    @Test
    fun keysUseTheNamesOfTheCore() {
        assertEquals(
            "policy.network.unmetered.image.mode",
            PolicyKeys.mode(PolicyCondition.UNMETERED, PolicyKind.IMAGE),
        )
        assertEquals(
            "policy.network.data_saver.files.max_size",
            PolicyKeys.maxSize(PolicyCondition.DATA_SAVER, PolicyKind.FILES),
        )
        assertEquals("policy.network.metered.relay", PolicyKeys.relay(PolicyCondition.METERED))
        assertEquals("policy.relay.max_size", PolicyKeys.RELAY_MAX_SIZE)
        val cells = PolicyCondition.entries.size * PolicyKind.entries.size
        assertEquals(2 * cells + PolicyCondition.entries.size + 1, PolicyKeys.all().size)
    }

    @Test
    fun cellChangeKeepsTheRestOfTheRow() {
        val table = PolicyDefaults.config
        val row = table[PolicyCondition.ROAMING]
        val changed = table.with(
            PolicyCondition.ROAMING,
            row.with(PolicyKind.IMAGE, PolicyCell(PolicyMode.AUTO, PolicySize.ofMegabytes(4))),
        )

        assertEquals(PolicyMode.AUTO, changed[PolicyCondition.ROAMING][PolicyKind.IMAGE].mode)
        assertEquals(4 * PolicySize.MEGABYTE, changed[PolicyCondition.ROAMING][PolicyKind.IMAGE].maxSizeBytes)
        assertEquals(row[PolicyKind.TEXT], changed[PolicyCondition.ROAMING][PolicyKind.TEXT])
        assertEquals(table[PolicyCondition.UNMETERED], changed[PolicyCondition.UNMETERED])
    }

    @Test
    fun conditionFollowsTheRestrictionsOfTheNetwork() {
        assertNull(conditionOf(null))
        assertNull(conditionOf(network(online = false, metered = true)))
        assertEquals(
            PolicyCondition.DATA_SAVER,
            conditionOf(network(online = true, metered = true, roaming = true, dataSaver = true)),
        )
        assertEquals(
            PolicyCondition.ROAMING,
            conditionOf(network(online = true, metered = true, roaming = true)),
        )
        assertEquals(PolicyCondition.METERED, conditionOf(network(online = true, metered = true)))
        assertEquals(PolicyCondition.UNMETERED, conditionOf(network(online = true)))
    }

    @Test
    fun sizesAreEditedInWholeMegabytes() {
        assertEquals(10L, PolicySize.toMegabytes(PolicyDefaults.LAN_MAX_SIZE))
        assertEquals(1L, PolicySize.toMegabytes(1))
        assertEquals(3 * PolicySize.MEGABYTE, PolicySize.ofMegabytes(3))
        assertEquals(PolicySize.MEGABYTE, PolicySize.ofMegabytes(0))
    }

    private fun assertCell(
        table: PolicyConfig,
        condition: PolicyCondition,
        kind: PolicyKind,
        mode: PolicyMode,
        megabytes: Long,
    ) {
        val cell = table[condition][kind]
        assertEquals("$condition/$kind mode", mode, cell.mode)
        assertEquals("$condition/$kind limit", megabytes * PolicySize.MEGABYTE, cell.maxSizeBytes)
    }

    private fun network(
        online: Boolean = false,
        metered: Boolean = false,
        roaming: Boolean = false,
        dataSaver: Boolean = false,
    ): Local.NetworkState = Local.NetworkState.newBuilder()
        .setOnline(online)
        .setMetered(metered)
        .setRoaming(roaming)
        .setDataSaver(dataSaver)
        .build()
}
