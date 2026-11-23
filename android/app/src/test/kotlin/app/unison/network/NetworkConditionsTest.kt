package app.unison.network

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NetworkConditionsTest {
    @Test
    fun stateCarriesEveryFlagOfTheCore() {
        val conditions =
            NetworkConditions(
                online = true,
                metered = true,
                roaming = true,
                vpn = true,
                dataSaver = true,
            )

        val state = conditions.toProto()

        assertTrue(state.online)
        assertTrue(state.metered)
        assertTrue(state.roaming)
        assertTrue(state.vpn)
        assertTrue(state.dataSaver)
    }

    @Test
    fun offlineStateIsReportedOffline() {
        val state = NetworkConditions().toProto()

        assertFalse(state.online)
        assertFalse(state.metered)
        assertFalse(state.roaming)
        assertFalse(state.vpn)
        assertFalse(state.dataSaver)
    }

    @Test
    fun wifiOnlyModeRunsOnWifiAndEthernet() {
        assertTrue(NetworkConditions(online = true, wifi = true).localNetwork)
        assertTrue(NetworkConditions(online = true, ethernet = true).localNetwork)
    }

    @Test
    fun wifiOnlyModeDoesNotRunOnCellular() {
        val cellular = NetworkConditions(online = true, metered = true)

        assertFalse(cellular.localNetwork)
    }
}
