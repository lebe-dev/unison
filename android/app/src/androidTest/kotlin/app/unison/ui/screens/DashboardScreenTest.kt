package app.unison.ui.screens

import android.graphics.Bitmap
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.doubleClick
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.longClick
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.pinch
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
    fun tabsAreShown() {
        setContent(dashboard = DashboardState(loaded = true))

        DashboardTab.entries.forEach { tab ->
            compose.onNodeWithTag(tabTag(tab)).assertIsDisplayed()
        }
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
    fun imageTapOpensTheFullscreenViewerAndCloses() {
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    images = listOf(entry(id = "1", kind = Local.Kind.KIND_IMAGE)),
                ),
            thumbnail = { _, _ -> Bitmap.createBitmap(4, 4, Bitmap.Config.ARGB_8888).asImageBitmap() },
        )

        compose.onNodeWithTag(itemTag(image(id = "1"))).performClick()

        compose.onNodeWithTag(FULLSCREEN_TAG).assertIsDisplayed()
        compose.onNodeWithTag(FULLSCREEN_CLOSE_TAG).performClick()
        compose.onNodeWithTag(FULLSCREEN_TAG).assertDoesNotExist()
    }

    @Test
    fun missingImageCanBeDownloadedFromFullscreenViewer() {
        var downloaded: Local.HistoryItem? = null
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    images = listOf(entry(id = "1", kind = Local.Kind.KIND_IMAGE, complete = false)),
                ),
            onDownload = { downloaded = it },
        )

        compose.onNodeWithTag(itemTag(image(id = "1"))).performClick()
        compose.onNodeWithTag(FULLSCREEN_DOWNLOAD_TAG).assertIsDisplayed().performClick()

        assertEquals("1", downloaded?.item?.id)
    }

    @Test
    fun fullscreenViewerSupportsPinchAndDoubleTap() {
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    images = listOf(entry(id = "1", kind = Local.Kind.KIND_IMAGE)),
                ),
            thumbnail = { _, _ -> Bitmap.createBitmap(4, 4, Bitmap.Config.ARGB_8888).asImageBitmap() },
        )
        compose.onNodeWithTag(itemTag(image(id = "1"))).performClick()

        compose.onNodeWithTag(FULLSCREEN_TAG).performTouchInput {
            pinch(
                start0 = center - Offset(80f, 0f),
                end0 = center - Offset(160f, 0f),
                start1 = center + Offset(80f, 0f),
                end1 = center + Offset(160f, 0f),
            )
            doubleClick()
        }

        compose.onNodeWithTag(FULLSCREEN_TAG).assertIsDisplayed()
        compose.onNodeWithTag(FULLSCREEN_CLOSE_TAG).performClick()
        compose.onNodeWithTag(FULLSCREEN_TAG).assertDoesNotExist()
    }

    @Test
    fun imageLongPressOpensTheMenuInsteadOfTheViewer() {
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    images = listOf(entry(id = "1", kind = Local.Kind.KIND_IMAGE)),
                ),
        )

        compose.onNodeWithTag(itemTag(image(id = "1"))).performTouchInput { longClick() }

        compose.onNodeWithTag(MENU_SEND_TAG).assertIsDisplayed()
        compose.onNodeWithTag(FULLSCREEN_TAG).assertDoesNotExist()
    }

    @Test
    fun longPressShowsTheMenuWithoutThePreview() {
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    text = listOf(entry(id = "1", kind = Local.Kind.KIND_TEXT)),
                ),
            devices = listOf(device(id = "mac", name = "Mac")),
        )
        compose.onNodeWithTag(tabTag(DashboardTab.TEXT)).performClick()

        compose.onNodeWithTag(itemTag(text(id = "1"))).performTouchInput { longClick() }

        compose.onNodeWithTag(MENU_SEND_TAG).assertIsDisplayed()
        compose.onNodeWithTag(MENU_COPY_TAG).assertIsDisplayed()
        compose.onNodeWithTag(MENU_DELETE_TAG).assertIsDisplayed()
        compose.onNodeWithTag(PREVIEW_TAG).assertDoesNotExist()
    }

    @Test
    fun menuCopiesTheItem() {
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

        compose.onNodeWithTag(itemTag(text(id = "1"))).performTouchInput { longClick() }
        compose.onNodeWithTag(MENU_COPY_TAG).performClick()

        assertEquals("1", copied?.item?.id)
    }

    @Test
    fun menuSendsTheItemToADevice() {
        var sent: Pair<String, String>? = null
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    text = listOf(entry(id = "1", kind = Local.Kind.KIND_TEXT)),
                ),
            devices = listOf(device(id = "mac", name = "Mac")),
            onSend = { item, deviceId -> sent = item.item.id to deviceId },
        )
        compose.onNodeWithTag(tabTag(DashboardTab.TEXT)).performClick()

        compose.onNodeWithTag(itemTag(text(id = "1"))).performTouchInput { longClick() }
        compose.onNodeWithTag(MENU_SEND_TAG).performClick()
        compose.onNodeWithText(testString(R.string.dashboard_send_to)).assertIsDisplayed()
        compose.onNodeWithTag(deviceMenuTag("mac")).performClick()

        assertEquals("1" to "mac", sent)
    }

    @Test
    fun menuSaysWhenNoDeviceIsOnline() {
        setContent(
            dashboard =
                DashboardState(
                    loaded = true,
                    text = listOf(entry(id = "1", kind = Local.Kind.KIND_TEXT)),
                ),
        )
        compose.onNodeWithTag(tabTag(DashboardTab.TEXT)).performClick()

        compose.onNodeWithTag(itemTag(text(id = "1"))).performTouchInput { longClick() }
        compose.onNodeWithTag(MENU_SEND_TAG).performClick()

        compose.onNodeWithText(testString(R.string.dashboard_no_devices)).assertIsDisplayed()
    }

    @Test
    fun menuDeletesAfterConfirmation() {
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

        compose.onNodeWithTag(itemTag(text(id = "1"))).performTouchInput { longClick() }
        compose.onNodeWithTag(MENU_DELETE_TAG).performClick()

        compose.onNodeWithText(testString(R.string.dashboard_delete_title)).assertIsDisplayed()
        assertNull(deleted)

        compose.onNodeWithText(testString(R.string.dashboard_delete_confirm)).performClick()

        assertEquals("1", deleted?.item?.id)
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
        devices: List<Local.Device> = emptyList(),
        thumbnail: HistoryThumbnailLoader = { _, _ -> null },
        onRefresh: () -> Unit = {},
        onCopy: (Local.HistoryItem) -> Unit = {},
        onSend: (Local.HistoryItem, String) -> Unit = { _, _ -> },
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
                            devices = devices,
                            onRefresh = onRefresh,
                            onCopy = onCopy,
                            onSend = onSend,
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

    private fun text(id: String): Local.ClipItem = entry(id = id, kind = Local.Kind.KIND_TEXT).item

    private fun device(
        id: String,
        name: String,
    ): Local.Device =
        Local.Device
            .newBuilder()
            .setDeviceId(id)
            .setName(name)
            .setOnline(true)
            .setRole(Local.Role.ROLE_PERSONAL)
            .build()
}
