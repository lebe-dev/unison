package app.unison.ui.screens

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.R
import app.unison.history.HistoryText
import app.unison.history.SizeUnits
import app.unison.testContext
import app.unison.testString
import app.unison.ui.state.CoreFailure
import app.unison.ui.state.HistoryState
import app.unison.ui.theme.UnisonTheme
import com.google.protobuf.ByteString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class HistoryScreenTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun entriesShowKindDeviceAndSize() {
        setContent(
            history =
                HistoryState(
                    loaded = true,
                    items =
                        listOf(
                            entry(id = "1", kind = Local.Kind.KIND_TEXT, origin = ORIGIN, size = 11),
                            entry(id = "2", kind = Local.Kind.KIND_FILES, origin = ORIGIN, size = 2048),
                        ),
                ),
        )

        compose.onNodeWithText("hello world").assertIsDisplayed()
        compose.onNodeWithText("notes.txt").assertIsDisplayed()
        val units = SizeUnits.of(testContext)
        compose
            .onNodeWithText(
                testString(R.string.history_item_detail, "Mac", HistoryText.formatSize(11, units)),
            ).assertIsDisplayed()
        compose
            .onNodeWithText(
                testString(R.string.history_item_detail, "Mac", HistoryText.formatSize(2048, units)),
            ).assertIsDisplayed()
    }

    @Test
    fun fileEntriesCannotBeCopied() {
        setContent(
            history =
                HistoryState(
                    loaded = true,
                    items =
                        listOf(
                            entry(id = "1", kind = Local.Kind.KIND_TEXT, origin = ORIGIN, size = 11),
                            entry(id = "2", kind = Local.Kind.KIND_FILES, origin = ORIGIN, size = 2048),
                        ),
                ),
        )

        compose.onAllNodesWithText(testString(R.string.action_copy)).assertCountEquals(1)
        compose.onAllNodesWithText(testString(R.string.action_download)).assertCountEquals(2)
        compose.onAllNodesWithText(testString(R.string.action_delete)).assertCountEquals(2)
    }

    @Test
    fun missingPayloadIsReported() {
        setContent(
            history =
                HistoryState(
                    loaded = true,
                    items = listOf(entry(id = "1", kind = Local.Kind.KIND_IMAGE, origin = ORIGIN, size = 0, complete = false)),
                ),
        )

        compose
            .onNodeWithText(
                testString(
                    R.string.history_item_detail_incomplete,
                    "Mac",
                    HistoryText.formatSize(0, SizeUnits.of(testContext)),
                ),
            ).assertIsDisplayed()
    }

    @Test
    fun heldEntryIsAppliedInsteadOfCopied() {
        var applied: Local.HistoryItem? = null
        var copied: Local.HistoryItem? = null
        setContent(
            history =
                HistoryState(
                    loaded = true,
                    items = listOf(entry(id = "1", kind = Local.Kind.KIND_TEXT, origin = ORIGIN, size = 11)),
                ),
            held = setOf("1"),
            onCopy = { copied = it },
            onApply = { applied = it },
        )

        compose.onNodeWithText(testString(R.string.history_held)).assertIsDisplayed()
        compose.onAllNodesWithText(testString(R.string.action_copy)).assertCountEquals(0)
        compose.onNodeWithText(testString(R.string.action_apply)).performClick()

        assertEquals("1", applied?.item?.id)
        assertNull(copied)
    }

    @Test
    fun actionsReportTheEntry() {
        var copied: Local.HistoryItem? = null
        var downloaded: Local.HistoryItem? = null
        var deleted: Local.HistoryItem? = null
        setContent(
            history =
                HistoryState(
                    loaded = true,
                    items = listOf(entry(id = "1", kind = Local.Kind.KIND_TEXT, origin = ORIGIN, size = 11)),
                ),
            onCopy = { copied = it },
            onDownload = { downloaded = it },
            onDelete = { deleted = it },
        )

        compose.onNodeWithText(testString(R.string.action_copy)).performClick()
        compose.onNodeWithText(testString(R.string.action_download)).performClick()
        compose.onNodeWithText(testString(R.string.action_delete)).performClick()

        assertEquals("1", copied?.item?.id)
        assertEquals("1", downloaded?.item?.id)
        assertEquals("1", deleted?.item?.id)
    }

    @Test
    fun clearAsksBeforeRemovingEverything() {
        var cleared = 0
        setContent(
            history =
                HistoryState(
                    loaded = true,
                    items = listOf(entry(id = "1", kind = Local.Kind.KIND_TEXT, origin = ORIGIN, size = 11)),
                ),
            onClear = { cleared++ },
        )

        compose.onNodeWithText(testString(R.string.history_clear)).performClick()
        compose.onNodeWithText(testString(R.string.history_clear_title)).assertIsDisplayed()
        assertEquals(0, cleared)

        compose.onNodeWithText(testString(R.string.history_clear_confirm)).performClick()
        assertEquals(1, cleared)
    }

    @Test
    fun clearIsCancelledWithoutRemovingAnything() {
        var cleared = 0
        setContent(
            history = HistoryState(loaded = true),
            onClear = { cleared++ },
        )

        compose.onNodeWithText(testString(R.string.history_clear)).performClick()
        compose.onNodeWithText(testString(R.string.action_cancel)).performClick()

        assertEquals(0, cleared)
    }

    @Test
    fun kindFilterIsReported() {
        var kind: Local.Kind? = null
        setContent(
            history = HistoryState(loaded = true),
            onKindChange = { kind = it },
        )

        compose.onNodeWithText(testString(R.string.history_filter_images)).performClick()

        assertEquals(Local.Kind.KIND_IMAGE, kind)
    }

    @Test
    fun deviceFilterListsTheDevices() {
        var device: String? = null
        setContent(
            history = HistoryState(loaded = true),
            onDeviceChange = { device = it },
        )

        compose
            .onNodeWithText(
                testString(
                    R.string.history_filter_device,
                    testString(R.string.history_all_devices),
                ),
            ).performClick()
        compose.onNodeWithText("Mac").performClick()

        assertEquals(ORIGIN, device)
    }

    @Test
    fun emptyHistoryIsExplained() {
        setContent(history = HistoryState(loaded = true))

        compose.onNodeWithText(testString(R.string.history_empty)).assertIsDisplayed()
    }

    @Test
    fun stoppedCoreShowsRetry() {
        var retried = false
        setContent(
            history = HistoryState(loaded = true, failure = CoreFailure.STOPPED),
            onRefresh = { retried = true },
        )

        compose.onNodeWithText(testString(R.string.error_core_stopped)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.action_retry)).performClick()

        assertTrue(retried)
    }

    private fun setContent(
        history: HistoryState,
        held: Set<String> = emptySet(),
        onRefresh: () -> Unit = {},
        onKindChange: (Local.Kind) -> Unit = {},
        onDeviceChange: (String) -> Unit = {},
        onCopy: (Local.HistoryItem) -> Unit = {},
        onApply: (Local.HistoryItem) -> Unit = {},
        onDownload: (Local.HistoryItem) -> Unit = {},
        onDelete: (Local.HistoryItem) -> Unit = {},
        onClear: () -> Unit = {},
    ) {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                HistoryScreen(
                    history = history,
                    status = status(),
                    held = held,
                    onRefresh = onRefresh,
                    onKindChange = onKindChange,
                    onDeviceChange = onDeviceChange,
                    onCopy = onCopy,
                    onApply = onApply,
                    onDownload = onDownload,
                    onDelete = onDelete,
                    onClear = onClear,
                )
            }
        }
    }

    private fun entry(
        id: String,
        kind: Local.Kind,
        origin: String,
        size: Long,
        complete: Boolean = true,
    ): Local.HistoryItem {
        val representation =
            when (kind) {
                Local.Kind.KIND_TEXT ->
                    Local.Representation
                        .newBuilder()
                        .setMimeType("text/plain")
                        .setSize(size)
                        .setInlineBytes(ByteString.copyFromUtf8("hello world"))
                        .build()
                Local.Kind.KIND_FILES ->
                    Local.Representation
                        .newBuilder()
                        .setMimeType("application/x-unison-file")
                        .setSize(size)
                        .setFileName("notes.txt")
                        .build()
                else ->
                    Local.Representation
                        .newBuilder()
                        .setMimeType("image/png")
                        .setSize(size)
                        .build()
            }
        return Local.HistoryItem
            .newBuilder()
            .setItem(
                Local.ClipItem
                    .newBuilder()
                    .setId(id)
                    .setOrigin(origin)
                    .setKind(kind)
                    .setCreatedAtUnixMs(System.currentTimeMillis() - 60_000)
                    .addRepresentations(representation),
            ).setComplete(complete)
            .build()
    }

    private fun status(): Local.GetStatusResponse =
        Local.GetStatusResponse
            .newBuilder()
            .setDeviceId("pixel-id")
            .setDeviceName("Pixel")
            .addDevices(
                Local.Device
                    .newBuilder()
                    .setDeviceId(ORIGIN)
                    .setName("Mac")
                    .setRole(Local.Role.ROLE_PERSONAL)
                    .build(),
            ).build()

    private companion object {
        const val ORIGIN = "mac-id"
    }
}
