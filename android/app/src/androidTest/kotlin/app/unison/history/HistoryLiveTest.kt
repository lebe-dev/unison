package app.unison.history

import android.content.Context
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasScrollToIndexAction
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performScrollToIndex
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.core.CoreClient
import app.unison.core.CoreStarter
import app.unison.core.coreEvents
import app.unison.history.HistorySender
import app.unison.ui.screens.HistoryScreen
import app.unison.ui.state.AppState
import app.unison.ui.theme.UnisonTheme
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

/**
 * Runs the history against the live core while the screen stays on: an item
 * captured after the first list appeared has to reach the screen without a
 * manual refresh and without recreating the screen.
 */
@RunWith(AndroidJUnit4::class)
class HistoryLiveTest {
    @get:Rule
    val compose = createComposeRule()

    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Test
    fun itemCapturedWhileTheScreenIsOpenAppears() {
        assertTrue("the core did not start", runBlocking { CoreStarter.await(context, START_TIMEOUT_MS) })
        val core = CoreClient()
        val state = AppState(core, coreEvents.events, HistoryDownloads(context), HistorySender { _, _ -> })
        try {
            state.refreshHistory()
            compose.waitUntil(TIMEOUT_MS) { state.history.value.loaded }

            val history = state.history
            compose.setContent {
                val current by history.collectAsState()
                UnisonTheme(dynamicColor = false) {
                    HistoryScreen(
                        history = current,
                        status = null,
                        held = emptySet(),
                        onRefresh = state::refreshHistory,
                        onKindChange = state::setHistoryKind,
                        onDeviceChange = state::setHistoryDevice,
                        onCopy = { state.copyHistoryItem(it.item.id) },
                        onApply = { state.applyHistoryItem(it.item.id) },
                        onDownload = { state.downloadHistoryItem(it.item.id) },
                        onDelete = { state.deleteHistoryItem(it.item.id) },
                        onClear = state::clearHistory,
                    )
                }
            }
            compose.waitForIdle()

            val text = "live probe ${System.currentTimeMillis()}"
            runBlocking {
                core.call(
                    method = "SetClip",
                    request =
                        Local.SetClipRequest
                            .newBuilder()
                            .setText(text)
                            .build(),
                    parse = Local.SetClipResponse::parseFrom,
                )
            }

            compose.waitUntil(TIMEOUT_MS) {
                state.history.value.items.any { entry ->
                    entry.item.representationsList.any { it.inlineBytes.toStringUtf8() == text }
                }
            }
            compose.waitUntil(TIMEOUT_MS) {
                compose.onAllNodesWithText(text).fetchSemanticsNodes().isNotEmpty()
            }
            compose.onNodeWithText(text).assertIsDisplayed()

            // A second entry has to appear on the screen that is already on.
            val next = "live probe two ${System.currentTimeMillis()}"
            runBlocking {
                core.call(
                    method = "SetClip",
                    request =
                        Local.SetClipRequest
                            .newBuilder()
                            .setText(next)
                            .build(),
                    parse = Local.SetClipResponse::parseFrom,
                )
            }
            compose.waitUntil(TIMEOUT_MS) {
                compose.onAllNodesWithText(next).fetchSemanticsNodes().isNotEmpty()
            }
            compose.onNodeWithText(next).assertIsDisplayed()
        } finally {
            state.close()
        }
    }

    @Test
    fun entryDeletedInTheCoreLeavesTheScreen() {
        assertTrue("the core did not start", runBlocking { CoreStarter.await(context, START_TIMEOUT_MS) })
        val core = CoreClient()
        val state = AppState(core, coreEvents.events, HistoryDownloads(context), HistorySender { _, _ -> })
        try {
            val text = "delete probe ${System.currentTimeMillis()}"
            val id =
                runBlocking {
                    core
                        .call(
                            method = "SetClip",
                            request =
                                Local.SetClipRequest
                                    .newBuilder()
                                    .setText(text)
                                    .build(),
                            parse = Local.SetClipResponse::parseFrom,
                        ).item.id
                }
            assertTrue("SetClip returned no id", id.isNotEmpty())

            state.refreshHistory()
            compose.waitUntil(TIMEOUT_MS) { state.history.value.loaded }

            val history = state.history
            compose.setContent {
                val current by history.collectAsState()
                UnisonTheme(dynamicColor = false) {
                    HistoryScreen(
                        history = current,
                        status = null,
                        held = emptySet(),
                        onRefresh = state::refreshHistory,
                        onKindChange = state::setHistoryKind,
                        onDeviceChange = state::setHistoryDevice,
                        onCopy = { state.copyHistoryItem(it.item.id) },
                        onApply = { state.applyHistoryItem(it.item.id) },
                        onDownload = { state.downloadHistoryItem(it.item.id) },
                        onDelete = { state.deleteHistoryItem(it.item.id) },
                        onClear = state::clearHistory,
                    )
                }
            }
            compose.waitUntil(TIMEOUT_MS) { compose.onAllNodesWithText(text).fetchSemanticsNodes().isNotEmpty() }

            // The entry goes away in the core, not through the screen.
            runBlocking { core.deleteHistoryItem(id) }

            compose.waitUntil(TIMEOUT_MS) {
                compose.onAllNodesWithText(text).fetchSemanticsNodes().isEmpty()
            }
        } finally {
            state.close()
        }
    }

    @Test
    fun scrollPositionIsKeptWhenAnEntryArrives() {
        assertTrue("the core did not start", runBlocking { CoreStarter.await(context, START_TIMEOUT_MS) })
        val core = CoreClient()
        val state = AppState(core, coreEvents.events, HistoryDownloads(context), HistorySender { _, _ -> })
        try {
            val stamp = System.currentTimeMillis()
            for (index in 1..LIST_SIZE) {
                runBlocking { submit(core, "scroll probe $index $stamp") }
            }
            state.refreshHistory()
            compose.waitUntil(TIMEOUT_MS) { state.history.value.loaded }

            val history = state.history
            compose.setContent {
                val current by history.collectAsState()
                UnisonTheme(dynamicColor = false) {
                    HistoryScreen(
                        history = current,
                        status = null,
                        held = emptySet(),
                        onRefresh = state::refreshHistory,
                        onKindChange = state::setHistoryKind,
                        onDeviceChange = state::setHistoryDevice,
                        onCopy = { state.copyHistoryItem(it.item.id) },
                        onApply = { state.applyHistoryItem(it.item.id) },
                        onDownload = { state.downloadHistoryItem(it.item.id) },
                        onDelete = { state.deleteHistoryItem(it.item.id) },
                        onClear = state::clearHistory,
                    )
                }
            }
            compose.waitUntil(TIMEOUT_MS) {
                compose.onAllNodesWithText("scroll probe $LIST_SIZE $stamp").fetchSemanticsNodes().isNotEmpty()
            }

            // Scroll a few entries down, then let a new entry arrive.
            val anchor = "scroll probe ${LIST_SIZE - SCROLLED_BY} $stamp"
            compose.onNode(hasScrollToIndexAction()).performScrollToIndex(SCROLLED_BY)
            compose.waitForIdle()
            compose.onNodeWithText(anchor).assertIsDisplayed()

            runBlocking { submit(core, "scroll probe top $stamp") }
            compose.waitUntil(TIMEOUT_MS) {
                state.history.value.items.firstOrNull()?.item?.id?.let { id ->
                    state.history.value.items
                        .any { it.item.id == id }
                } == true
            }
            Thread.sleep(SETTLE_MS)

            compose.onNodeWithText(anchor).assertIsDisplayed()
        } finally {
            state.close()
        }
    }

    /** Records one text entry through the local API. */
    private suspend fun submit(
        core: CoreClient,
        text: String,
    ) = core.call(
        method = "SetClip",
        request =
            Local.SetClipRequest
                .newBuilder()
                .setText(text)
                .build(),
        parse = Local.SetClipResponse::parseFrom,
    )

    private companion object {
        const val TIMEOUT_MS = 20_000L
        const val START_TIMEOUT_MS = 20_000L

        /** Entries recorded to make the list longer than the screen. */
        const val LIST_SIZE = 12

        /** Entries the list is scrolled down before a new one arrives. */
        const val SCROLLED_BY = 4

        /** Time the screen gets to react to the new entry. */
        const val SETTLE_MS = 1_500L
    }
}
