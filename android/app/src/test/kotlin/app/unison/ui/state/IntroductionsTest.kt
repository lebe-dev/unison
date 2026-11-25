package app.unison.ui.state

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local

class IntroductionsTest {
    @Test
    fun personalDevicesCanJoinAsEitherRole() {
        val roles = Introductions.roles(device("a", Local.Role.ROLE_PERSONAL), device("b", Local.Role.ROLE_PERSONAL))

        assertEquals(listOf(Local.Role.ROLE_PERSONAL, Local.Role.ROLE_FRIENDS), roles)
        assertEquals(
            Local.Role.ROLE_PERSONAL,
            Introductions.defaultRole(
                device("a", Local.Role.ROLE_PERSONAL),
                device("b", Local.Role.ROLE_PERSONAL),
            ),
        )
    }

    @Test
    fun friendsChainStaysFriends() {
        val cases =
            listOf(
                Local.Role.ROLE_FRIENDS to Local.Role.ROLE_PERSONAL,
                Local.Role.ROLE_PERSONAL to Local.Role.ROLE_FRIENDS,
                Local.Role.ROLE_FRIENDS to Local.Role.ROLE_FRIENDS,
            )
        for ((first, second) in cases) {
            val roles = Introductions.roles(device("a", first), device("b", second))
            assertEquals("the pair may not be raised above Friends", listOf(Local.Role.ROLE_FRIENDS), roles)
            assertEquals(Local.Role.ROLE_FRIENDS, Introductions.defaultRole(device("a", first), device("b", second)))
        }
    }

    @Test
    fun onlyOnlineSupportedDevicesCanBeIntroduced() {
        assertTrue(
            Introductions.canIntroduce(
                device("a", Local.Role.ROLE_PERSONAL)
                    .toBuilder()
                    .setOnline(true)
                    .setSupportsMediatedPair(true)
                    .build(),
            ),
        )
        assertFalse(Introductions.canIntroduce(device("a", Local.Role.ROLE_PERSONAL)))
        assertFalse(
            Introductions.canIntroduce(
                device("a", Local.Role.ROLE_PERSONAL)
                    .toBuilder()
                    .setOnline(false)
                    .setSupportsMediatedPair(true)
                    .build(),
            ),
        )
        assertFalse(
            Introductions.canIntroduce(
                device("a", Local.Role.ROLE_PERSONAL)
                    .toBuilder()
                    .setOnline(true)
                    .setSupportsMediatedPair(false)
                    .build(),
            ),
        )
    }

    private fun device(
        id: String,
        role: Local.Role,
    ): Local.Device =
        Local.Device
            .newBuilder()
            .setDeviceId(id)
            .setName(id)
            .setRole(role)
            .build()
}
