package app.unison.ui.screens

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.isToggleable
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.policy.PolicyCondition
import app.unison.policy.PolicyConfig
import app.unison.policy.PolicyDefaults
import app.unison.policy.PolicyKind
import app.unison.policy.PolicyMode
import app.unison.ui.theme.UnisonTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class PolicySectionTest {

    @get:Rule
    val compose = createComposeRule()

    @Test
    fun cellModeIsChanged() {
        var saved: PolicyConfig? = null
        setContent(onChange = { saved = it })

        compose.onAllNodesWithText("Automatic")[0].performScrollTo().performClick()
        compose.onNodeWithText("Ask first").performClick()

        val text = saved?.get(PolicyCondition.UNMETERED)?.get(PolicyKind.TEXT)
        assertEquals(PolicyMode.CONFIRM, text?.mode)
    }

    @Test
    fun relayCanBeTurnedOff() {
        var saved: PolicyConfig? = null
        setContent(onChange = { saved = it })

        compose.onAllNodes(isToggleable())[0].performScrollTo().performClick()

        assertFalse(saved?.get(PolicyCondition.UNMETERED)?.relay ?: true)
    }

    @Test
    fun resetIsRequested() {
        var reset = false
        setContent(onReset = { reset = true })

        compose.onNodeWithText("Reset to defaults").performScrollTo().performClick()

        assertTrue(reset)
    }

    @Test
    fun currentConditionIsShown() {
        setContent(
            network = Local.NetworkState.newBuilder()
                .setOnline(true)
                .setMetered(true)
                .setDataSaver(true)
                .build(),
        )

        compose.onNodeWithText("Now: Data Saver").assertExists()
    }

    private fun setContent(
        network: Local.NetworkState = Local.NetworkState.getDefaultInstance(),
        onChange: (PolicyConfig) -> Unit = {},
        onReset: () -> Unit = {},
    ) {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
                    PolicySection(
                        policy = PolicyDefaults.config,
                        network = network,
                        onPolicyChange = onChange,
                        onReset = onReset,
                    )
                }
            }
        }
    }
}
