package app.unison.ui.screens

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.R
import app.unison.testString
import app.unison.ui.pairing.PAIRING_CODE_TAG
import app.unison.ui.state.PairingLinkOffer
import app.unison.ui.state.PairingRequest
import app.unison.ui.state.PairingResult
import app.unison.ui.state.PairingState
import app.unison.ui.theme.UnisonTheme
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local
import java.util.concurrent.atomic.AtomicBoolean

@RunWith(AndroidJUnit4::class)
class AddScreenTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun closedWindowOffersTheActions() {
        setContent(PairingState())

        compose.onNodeWithText(testString(R.string.add_title)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.pairing_scan_action)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.pairing_open_window)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.pairing_role_personal)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.pairing_role_friends)).assertIsDisplayed()
    }

    @Test
    fun openWindowShowsTheCodeAndTheTimeLeft() {
        val expiresAt = System.currentTimeMillis() + WINDOW_MS
        setContent(
            PairingState(
                window =
                    Local.PairingWindow
                        .newBuilder()
                        .setOpen(true)
                        .setAddr("192.168.1.5:41234")
                        .setExpiresAtUnixMs(expiresAt)
                        .build(),
                qr =
                    Local.PairingQr
                        .newBuilder()
                        .setUri(CODE)
                        .setExpiresAtUnixMs(expiresAt)
                        .build(),
            ),
        )

        compose.onNodeWithText(testString(R.string.pairing_code_title)).assertIsDisplayed()
        compose.onNodeWithTag(PAIRING_CODE_TAG).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.pairing_extend)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.pairing_close)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.pairing_nearby_empty)).assertIsDisplayed()
    }

    @Test
    fun candidateStartsTheExchange() {
        val paired = AtomicBoolean(false)
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                AddScreen(
                    state =
                        PairingState(
                            window =
                                Local.PairingWindow
                                    .newBuilder()
                                    .setOpen(true)
                                    .setExpiresAtUnixMs(System.currentTimeMillis() + WINDOW_MS)
                                    .build(),
                            candidates =
                                listOf(
                                    Local.PairingCandidate
                                        .newBuilder()
                                        .setAddr("192.168.1.7:41234")
                                        .build(),
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
        compose.onNodeWithText(testString(R.string.pairing_pair)).performClick()

        assertTrue(paired.get())
    }

    @Test
    fun linkedCodeAsksBeforePairing() {
        val accepted = AtomicBoolean(false)
        val dismissed = AtomicBoolean(false)
        setLinkContent(
            link = PairingLinkOffer(uri = CODE, role = Local.Role.ROLE_PERSONAL),
            onAccept = { accepted.set(true) },
            onDismiss = { dismissed.set(true) },
        )

        compose.onNodeWithTag(PAIRING_LINK_TAG).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.pairing_link_title)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.pairing_link_body_personal)).assertIsDisplayed()
        assertFalse(accepted.get())

        compose.onNodeWithText(testString(R.string.pairing_pair)).performClick()
        assertTrue(accepted.get())
        assertFalse(dismissed.get())
    }

    @Test
    fun linkedCodeCanBeRefused() {
        val accepted = AtomicBoolean(false)
        val dismissed = AtomicBoolean(false)
        setLinkContent(
            link = PairingLinkOffer(uri = CODE, role = Local.Role.ROLE_FRIENDS),
            onAccept = { accepted.set(true) },
            onDismiss = { dismissed.set(true) },
        )

        compose.onNodeWithText(testString(R.string.pairing_link_body_friends)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.action_cancel)).performClick()
        assertTrue(dismissed.get())
        assertFalse(accepted.get())
    }

    private fun setLinkContent(
        link: PairingLinkOffer,
        onAccept: () -> Unit,
        onDismiss: () -> Unit,
    ) {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                AddScreen(
                    state = PairingState(link = link),
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
                    onAcceptLink = onAccept,
                    onDismissLink = onDismiss,
                )
            }
        }
    }

    @Test
    fun scannedCodeAsksToAddTheDevice() {
        val confirmed = AtomicBoolean(false)
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                AddScreen(
                    state =
                        PairingState(
                            request =
                                PairingRequest(
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

        compose
            .onNodeWithText(
                testString(R.string.pairing_add_title, "Mac", testString(R.string.pairing_role_personal)),
            ).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.pairing_add_body_qr)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.pairing_confirm)).performClick()

        assertTrue(confirmed.get())
    }

    @Test
    fun comparedCodeShowsTheDigitsAndTheEmoji() {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                AddScreen(
                    state =
                        PairingState(
                            request =
                                PairingRequest(
                                    deviceId = "abcd",
                                    name = "Pixel",
                                    role = Local.Role.ROLE_FRIENDS,
                                    platform = Local.Platform.PLATFORM_ANDROID,
                                    sas =
                                        Local.Sas
                                            .newBuilder()
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

        compose
            .onNodeWithText(
                testString(R.string.pairing_add_title, "Pixel", testString(R.string.pairing_role_friends)),
            ).assertIsDisplayed()
        compose.onNodeWithText("123 456").assertIsDisplayed()
        compose.onNodeWithText("\uD83D\uDC15 \uD83C\uDF3F").assertIsDisplayed()
    }

    @Test
    fun resultOfThePairIsShown() {
        setContent(PairingState(result = PairingResult(paired = true, name = "Mac")))

        compose.onNodeWithText(testString(R.string.pairing_paired, "Mac")).assertIsDisplayed()
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
