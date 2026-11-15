package app.unison.ui.screens

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.ui.theme.UnisonTheme
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class OnboardingScreenTest {

    @get:Rule
    val compose = createComposeRule()

    @Test
    fun switchStartsTheCore() {
        var enabled = false
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                OnboardingScreen(onEnable = { enabled = true })
            }
        }

        compose.onNodeWithText("Welcome to Unison").assertIsDisplayed()
        compose.onNodeWithText("Turn on").performClick()

        assertTrue(enabled)
    }
}
