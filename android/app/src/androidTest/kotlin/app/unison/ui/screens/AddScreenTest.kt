package app.unison.ui.screens

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.ui.pairing.PAIRING_CODE_TAG
import app.unison.ui.state.PairingRequest
import app.unison.ui.state.PairingResult
import app.unison.ui.state.PairingState
import app.unison.ui.theme.UnisonTheme
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class AddScreenTest {

    @get:Rule
    val compose = createComposeRule()

    @Test
    fun closedWindowOffersTheActions() {
        setContent(PairingState())

        compose.onNodeWithText("Pair a device").assertIsDisplayed()
        compose.onNodeWithText("Scan a code").assertIsDisplayed()
        compose.onNodeWithText("Open pairing window").assertIsDisplayed()
        compose.onNodeWithText("Personal").assertIsDisplayed()
        compose.onNodeWithText("Friends").assertIsDisplayed()
    }

    @Test
    fun openWindowShowsTheCodeAndTheTimeLeft() {
        val expiresAt = System.currentTimeMillis() + WINDOW_MS
        setContent(
            PairingState(
                window = Local.PairingWindow.newBuilder()
                    .setOpen(true)
                    .setAddr("192.168.1.5:41234")
                    .setExpiresAtUnixMs(expiresAt)
                    .build(),
                qr = Local.PairingQr.newBuilder()
                    .setUri(CODE)
                    .setExpiresAtUnixMs(expiresAt)
                    .build(),
            ),
        )

        compose.onNodeWithText("Your code").assertIsDisplayed()
        compose.onNodeWithTag(PAIRING_CODE_TAG).assertIsDisplayed()
        compose.onNodeWithText("Extend for 3 minutes").assertIsDisplayed()
        compose.onNodeWithText("Close window").assertIsDisplayed()
        compose.onNodeWithText("No devices found yet").assertIsDisplayed()
    }

    @Test
    fun candidateStartsTheExchange() {
        val paired = AtomicBoolean(false)
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                AddScreen(
                    state = PairingState(
                        window = Local.PairingWindow.newBuilder()
                            .setOpen(true)
                            .setExpiresAtUnixMs(System.currentTimeMillis() + WINDOW_MS)
                            .build(),
                        candidates = listOf(
                            Local.PairingCandidate.newBuilder().setAddr("192.168.1.7:41234").build(),
                        ),
                    ),
                    onRoleChange = {},
                    onOpenWindow = {},
                    onExtendWindow = {},
                    onCloseWindow = {},
                    onRefreshCandidates = {},
                    onPairWith = { paired.set(it == "192.168.1.7:41234") },
                    onConfirm = {},
                    onReject = {},
                    onScan = {},
                    onCancelScan = {},
                    onDismissResult = {},
                )
            }
        }

        compose.onNodeWithText("192.168.1.7:41234").assertIsDisplayed()
        compose.onNodeWithText("Pair").performClick()

        assertTrue(paired.get())
    }

    @Test
    fun scannedCodeAsksToAddTheDevice() {
        val confirmed = AtomicBoolean(false)
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                AddScreen(
                    state = PairingState(
                        request = PairingRequest(
                            deviceId = "abcd",
                            name = "Mac",
                            role = Local.Role.ROLE_PERSONAL,
                            platform = Local.Platform.PLATFORM_MACOS,
                            sas = null,
                            method = Local.PairingMethod.PAIRING_METHOD_QR,
                        ),
                    ),
                    onRoleChange = {},
                    onOpenWindow = {},
                    onExtendWindow = {},
                    onCloseWindow = {},
                    onRefreshCandidates = {},
                    onPairWith = {},
                    onConfirm = { confirmed.set(true) },
                    onReject = {},
                    onScan = {},
                    onCancelScan = {},
                    onDismissResult = {},
                )
            }
        }

        compose.onNodeWithText("Add Mac as Personal?").assertIsDisplayed()
        compose.onNodeWithText("The other device scanned your code.").assertIsDisplayed()
        compose.onNodeWithText("Add").performClick()

        assertTrue(confirmed.get())
    }

    @Test
    fun comparedCodeShowsTheDigitsAndTheEmoji() {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                AddScreen(
                    state = PairingState(
                        request = PairingRequest(
                            deviceId = "abcd",
                            name = "Pixel",
                            role = Local.Role.ROLE_FRIENDS,
                            platform = Local.Platform.PLATFORM_ANDROID,
                            sas = Local.Sas.newBuilder()
                                .setDigits("123456")
                                .addEmoji("\uD83D\uDC15")
                                .addEmoji("\uD83C\uDF3F")
                                .build(),
                            method = Local.PairingMethod.PAIRING_METHOD_SAS,
                        ),
                    ),
                    onRoleChange = {},
                    onOpenWindow = {},
                    onExtendWindow = {},
                    onCloseWindow = {},
                    onRefreshCandidates = {},
                    onPairWith = {},
                    onConfirm = {},
                    onReject = {},
                    onScan = {},
                    onCancelScan = {},
                    onDismissResult = {},
                )
            }
        }

        compose.onNodeWithText("Add Pixel as Friends?").assertIsDisplayed()
        compose.onNodeWithText("123 456").assertIsDisplayed()
        compose.onNodeWithText("\uD83D\uDC15 \uD83C\uDF3F").assertIsDisplayed()
    }

    @Test
    fun resultOfThePairIsShown() {
        setContent(PairingState(result = PairingResult(paired = true, name = "Mac")))

        compose.onNodeWithText("Paired with Mac").assertIsDisplayed()
    }

    private fun setContent(state: PairingState) {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                AddScreen(
                    state = state,
                    onRoleChange = {},
                    onOpenWindow = {},
                    onExtendWindow = {},
                    onCloseWindow = {},
                    onRefreshCandidates = {},
                    onPairWith = {},
                    onConfirm = {},
                    onReject = {},
                    onScan = {},
                    onCancelScan = {},
                    onDismissResult = {},
                )
            }
        }
    }

    private companion object {
        const val WINDOW_MS = 180_000L
        const val CODE = "unison://pair?v=1&id=00&fp=00&t=00&a=192.168.1.5:41234&role=personal"
    }
}
