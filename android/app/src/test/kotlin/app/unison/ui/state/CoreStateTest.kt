package app.unison.ui.state

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local

class CoreStateTest {
    @Test
    fun devicesAreSplitByRole() {
        val state = CoreState(status = statusOf(personal("Mac", online = true), friends("Phone", online = true)))

        assertEquals(listOf("Mac"), state.devices(Local.Role.ROLE_PERSONAL).map { it.name })
        assertEquals(listOf("Phone"), state.devices(Local.Role.ROLE_FRIENDS).map { it.name })
    }

    @Test
    fun onlineDevicesComeFirstAndNamesAreSorted() {
        val state =
            CoreState(
                status =
                    statusOf(
                        personal("Zoe", online = false),
                        personal("Bob", online = true),
                        personal("Alice", online = false),
                    ),
            )

        assertEquals(
            listOf("Bob", "Alice", "Zoe"),
            state.devices(Local.Role.ROLE_PERSONAL).map { it.name },
        )
    }

    @Test
    fun revokedDevicesAreHidden() {
        val revoked = personal("Old", online = false).toBuilder().setRevoked(true).build()
        val state = CoreState(status = statusOf(revoked))

        assertEquals(emptyList<Local.Device>(), state.devices(Local.Role.ROLE_PERSONAL))
    }

    @Test
    fun missingStatusHasNoDevices() {
        assertEquals(emptyList<Local.Device>(), CoreState().devices(Local.Role.ROLE_PERSONAL))
    }

    @Test
    fun trackingParametersAreRemovedUntilTheSettingsSayOtherwise() {
        assertEquals(true, CoreState().stripTracking)
        assertFalse(CoreState(settings = Local.Settings.getDefaultInstance()).stripTracking)

        val kept =
            CoreState(
                settings =
                    Local.Settings
                        .newBuilder()
                        .setStripTracking(true)
                        .build(),
            )

        assertTrue(kept.stripTracking)
    }

    private fun statusOf(vararg devices: Local.Device): Local.GetStatusResponse =
        Local.GetStatusResponse
            .newBuilder()
            .addAllDevices(devices.toList())
            .build()

    private fun personal(
        name: String,
        online: Boolean,
    ): Local.Device = device(name, online, Local.Role.ROLE_PERSONAL)

    private fun friends(
        name: String,
        online: Boolean,
    ): Local.Device = device(name, online, Local.Role.ROLE_FRIENDS)

    private fun device(
        name: String,
        online: Boolean,
        role: Local.Role,
    ): Local.Device =
        Local.Device
            .newBuilder()
            .setName(name)
            .setOnline(online)
            .setRole(role)
            .build()
}
