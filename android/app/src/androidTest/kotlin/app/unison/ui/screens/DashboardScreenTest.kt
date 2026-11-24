package app.unison.ui.screens

import android.graphics.Bitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.R
import app.unison.history.HistoryText
import app.unison.history.HistoryThumbnailLoader
import app.unison.history.SizeUnits
import app.unison.testContext
import app.unison.testString
import app.unison.ui.state.CoreFailure
import app.unison.ui.state.DashboardState
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
class DashboardScreenTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun tabsAndLimitAreShown() {
        setContent(dashboard = DashboardState(loaded = true, limit = 24))

        DashboardTab.entries.forEach { tab ->
            compose.onNodeWithTag(tabTag(tab)).assertIsDisplayed()
        }
        compose.onNodeWithText(testString(R.string.dashboard_newest, 24)).assertIsDisplayed()
    }

    @Test
    fun imagesAreShownInTheGrid() {
        var loaded = 0
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    images =
                        listOf(
                            entry(id = "1", kind = Local.Kind.KIND_IMAGE),
                            entry(id = "2", kind = Local.Kind.KIND_IMAGE),
                        ),
                ),
            thumbnail = { _, _ ->
                loaded++
                Bitmap.createBitmap(4, 4, Bitmap.Config.ARGB_8888).asImageBitmap()
            },
        )

        compose.onNodeWithTag(itemTag(image(id = "1"))).assertIsDisplayed()
        compose.onNodeWithTag(itemTag(image(id = "2"))).assertIsDisplayed()
        assertEquals(2, loaded)
    }

    @Test
    fun missingImagePayloadIsReported() {
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    images = listOf(entry(id = "1", kind = Local.Kind.KIND_IMAGE, complete = false)),
                ),
        )

        compose.onNodeWithText(testString(R.string.dashboard_not_downloaded)).assertIsDisplayed()
    }

    @Test
    fun filesAreShownWithNamesAndSizes() {
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    files = listOf(entry(id = "1", kind = Local.Kind.KIND_FILES, size = 2048)),
                ),
        )

        compose.onNodeWithTag(tabTag(DashboardTab.FILES)).performClick()

        compose.onNodeWithText("notes.txt").assertIsDisplayed()
        compose
            .onNodeWithText(HistoryText.formatSize(2048, SizeUnits.of(testContext)))
            .assertIsDisplayed()
    }

    @Test
    fun textEntriesShowTheirSnippet() {
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    text = listOf(entry(id = "1", kind = Local.Kind.KIND_TEXT)),
                ),
        )

        compose.onNodeWithTag(tabTag(DashboardTab.TEXT)).performClick()

        compose.onNodeWithText("hello world").assertIsDisplayed()
    }

    @Test
    fun emptyTabsAreExplained() {
        setContent(dashboard = DashboardState(loaded = true))

        compose.onNodeWithText(testString(R.string.dashboard_empty_images)).assertIsDisplayed()

        compose.onNodeWithTag(tabTag(DashboardTab.FILES)).performClick()
        compose.onNodeWithText(testString(R.string.dashboard_empty_files)).assertIsDisplayed()

        compose.onNodeWithTag(tabTag(DashboardTab.TEXT)).performClick()
        compose.onNodeWithText(testString(R.string.dashboard_empty_text)).assertIsDisplayed()
    }

    @Test
    fun textPreviewCopiesTheItem() {
        var copied: Local.HistoryItem? = null
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    text = listOf(entry(id = "1", kind = Local.Kind.KIND_TEXT)),
                ),
            onCopy = { copied = it },
        )

        compose.onNodeWithTag(tabTag(DashboardTab.TEXT)).performClick()
        compose.onNodeWithTag(itemTag(entry(id = "1", kind = Local.Kind.KIND_TEXT).item)).performClick()

        compose.onNodeWithTag(PREVIEW_TAG).assertIsDisplayed()
        compose.onNodeWithTag(PREVIEW_COPY_TAG).performClick()

        assertEquals("1", copied?.item?.id)
    }

    @Test
    fun imagePreviewCopiesTheItem() {
        var copied: Local.HistoryItem? = null
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    images = listOf(entry(id = "1", kind = Local.Kind.KIND_IMAGE)),
                ),
            onCopy = { copied = it },
        )

        compose.onNodeWithTag(itemTag(image(id = "1"))).performClick()

        compose.onNodeWithTag(PREVIEW_TAG).assertIsDisplayed()
        compose.onNodeWithTag(PREVIEW_COPY_TAG).performClick()
        assertEquals("1", copied?.item?.id)
        compose.onAllNodesWithTag(PREVIEW_DOWNLOAD_TAG).assertCountEquals(0)
    }

    @Test
    fun filePreviewOffersCopyAndDownload() {
        var downloaded: Local.HistoryItem? = null
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    files = listOf(entry(id = "1", kind = Local.Kind.KIND_FILES)),
                ),
            onDownload = { downloaded = it },
        )

        compose.onNodeWithTag(tabTag(DashboardTab.FILES)).performClick()
        compose.onNodeWithTag(itemTag(file(id = "1"))).performClick()
        compose.onNodeWithTag(PREVIEW_DOWNLOAD_TAG).performClick()

        assertEquals("1", downloaded?.item?.id)
    }

    @Test
    fun heldItemIsAppliedFromThePreview() {
        var applied: Local.HistoryItem? = null
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    text = listOf(entry(id = "1", kind = Local.Kind.KIND_TEXT)),
                ),
            held = setOf("1"),
            onApply = { applied = it },
        )

        compose.onNodeWithTag(tabTag(DashboardTab.TEXT)).performClick()
        compose.onNodeWithTag(itemTag(entry(id = "1", kind = Local.Kind.KIND_TEXT).item)).performClick()
        compose.onNodeWithTag(PREVIEW_APPLY_TAG).performClick()

        assertEquals("1", applied?.item?.id)
    }

    @Test
    fun deleteAsksBeforeRemovingTheItem() {
        var deleted: Local.HistoryItem? = null
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    text = listOf(entry(id = "1", kind = Local.Kind.KIND_TEXT)),
                ),
            onDelete = { deleted = it },
        )

        compose.onNodeWithTag(tabTag(DashboardTab.TEXT)).performClick()
        compose.onNodeWithTag(itemTag(entry(id = "1", kind = Local.Kind.KIND_TEXT).item)).performClick()
        compose.onNodeWithText(testString(R.string.action_delete)).performClick()

        compose.onNodeWithText(testString(R.string.dashboard_delete_title)).assertIsDisplayed()
        assertNull(deleted)

        compose.onNodeWithText(testString(R.string.dashboard_delete_confirm)).performClick()

        assertEquals("1", deleted?.item?.id)
    }

    @Test
    fun stoppedCoreShowsRetry() {
        var retried = false
        setContent(
            dashboard = DashboardState(loaded = false, failure = CoreFailure.STOPPED),
            onRefresh = { retried = true },
        )

        compose.onNodeWithText(testString(R.string.error_core_stopped)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.action_retry)).performClick()

        assertTrue(retried)
    }

    @Test
    fun firstLoadShowsASpinner() {
        setContent(dashboard = DashboardState(loading = true))

        compose.onNodeWithTag(LOADING_TAG).assertIsDisplayed()
    }

    private fun setContent(
        dashboard: DashboardState,
        held: Set<String> = emptySet(),
        busy: Set<String> = emptySet(),
        thumbnail: HistoryThumbnailLoader = { _, _ -> null },
        onRefresh: () -> Unit = {},
        onCopy: (Local.HistoryItem) -> Unit = {},
        onApply: (Local.HistoryItem) -> Unit = {},
        onDownload: (Local.HistoryItem) -> Unit = {},
        onDelete: (Local.HistoryItem) -> Unit = {},
    ) {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                DashboardScreen(
                    panel =
                        DashboardPanel(
                            state = dashboard,
                            thumbnail = thumbnail,
                            onRefresh = onRefresh,
                            onCopy = onCopy,
                            onApply = onApply,
                            onDownload = onDownload,
                            onDelete = onDelete,
                        ),
                    held = held,
                    busy = busy,
                )
            }
        }
    }

    private fun entry(
        id: String,
        kind: Local.Kind,
        size: Long = 11,
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
                    .setKind(kind)
                    .setCreatedAtUnixMs(System.currentTimeMillis() - 60_000)
                    .addRepresentations(representation),
            ).setComplete(complete)
            .build()
    }

    private fun image(id: String): Local.ClipItem = entry(id = id, kind = Local.Kind.KIND_IMAGE).item

    private fun file(id: String): Local.ClipItem = entry(id = id, kind = Local.Kind.KIND_FILES).item
}
