package app.unison.ui.screens

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.rememberTransformableState
import androidx.compose.foundation.gestures.transformable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import app.unison.R
import app.unison.history.HistoryThumbnailLoader
import app.unison.history.HistoryThumbnails
import unison.local.v1.Local

/** Fullscreen viewer of an image with pinch zoom, pan and double tap. */
@Composable
internal fun DashboardImageViewer(
    entry: Local.HistoryItem,
    thumbnail: HistoryThumbnailLoader,
    busy: Boolean = false,
    onDownload: () -> Unit = {},
    onClose: () -> Unit,
) {
    var image by remember(entry.item.id) { mutableStateOf<ImageBitmap?>(null) }
    var loading by remember(entry.item.id, entry.complete) { mutableStateOf(entry.complete) }
    var scale by remember(entry.item.id) { mutableFloatStateOf(MIN_ZOOM) }
    var offset by remember(entry.item.id) { mutableStateOf(Offset.Zero) }
    var viewport by remember { mutableStateOf(IntSize.Zero) }

    LaunchedEffect(entry.item.id) {
        if (!entry.complete && !busy) {
            onDownload()
        }
    }

    LaunchedEffect(entry.item.id, entry.complete) {
        if (entry.complete) {
            image = thumbnail(entry.item.id, HistoryThumbnails.FULLSCREEN_SIDE)
        }
        loading = false
    }

    Dialog(
        onDismissRequest = onClose,
        properties =
            DialogProperties(
                usePlatformDefaultWidth = false,
                decorFitsSystemWindows = false,
            ),
    ) {
        Box(
            modifier =
                Modifier
                    .fillMaxSize()
                    .background(Color.Black)
                    .onSizeChanged { viewport = it }
                    .testTag(FULLSCREEN_TAG),
        ) {
            val transform =
                rememberTransformableState { _, zoomChange, panChange, _ ->
                    val zoomed = (scale * zoomChange).coerceIn(MIN_ZOOM, MAX_ZOOM)
                    scale = zoomed
                    offset = clampOffset(offset + panChange, zoomed, viewport)
                }
            Box(
                modifier =
                    Modifier
                        .fillMaxSize()
                        .transformable(transform)
                        .pointerInput(entry.item.id) {
                            detectTapGestures(
                                onDoubleTap = {
                                    if (scale > MIN_ZOOM) {
                                        scale = MIN_ZOOM
                                        offset = Offset.Zero
                                    } else {
                                        scale = DOUBLE_TAP_ZOOM
                                    }
                                },
                            )
                        },
                contentAlignment = Alignment.Center,
            ) {
                val bitmap = image
                when {
                    bitmap != null ->
                        Image(
                            bitmap = bitmap,
                            contentDescription = null,
                            contentScale = ContentScale.Fit,
                            modifier =
                                Modifier
                                    .fillMaxSize()
                                    .graphicsLayer(
                                        scaleX = scale,
                                        scaleY = scale,
                                        translationX = offset.x,
                                        translationY = offset.y,
                                    ),
                        )
                    loading || busy -> CircularProgressIndicator(color = Color.White)
                    else ->
                        Column(
                            horizontalAlignment = Alignment.CenterHorizontally,
                            verticalArrangement = Arrangement.spacedBy(12.dp),
                        ) {
                            Text(
                                text = stringResource(R.string.dashboard_not_downloaded),
                                style = MaterialTheme.typography.bodyMedium,
                                color = Color.White,
                            )
                            if (!entry.complete) {
                                Button(
                                    onClick = onDownload,
                                    modifier = Modifier.testTag(FULLSCREEN_DOWNLOAD_TAG),
                                ) {
                                    Text(text = stringResource(R.string.action_download))
                                }
                            }
                        }
                }
            }
            IconButton(
                onClick = onClose,
                modifier =
                    Modifier
                        .align(Alignment.TopEnd)
                        .statusBarsPadding()
                        .padding(8.dp)
                        .testTag(FULLSCREEN_CLOSE_TAG),
            ) {
                Icon(
                    painter = painterResource(R.drawable.ic_close),
                    contentDescription = stringResource(R.string.action_close),
                    tint = Color.White,
                )
            }
        }
    }
}

private fun clampOffset(
    candidate: Offset,
    scale: Float,
    viewport: IntSize,
): Offset {
    if (scale <= MIN_ZOOM) {
        return Offset.Zero
    }
    val maxX = viewport.width * (scale - MIN_ZOOM) / 2f
    val maxY = viewport.height * (scale - MIN_ZOOM) / 2f
    return Offset(
        x = candidate.x.coerceIn(-maxX, maxX),
        y = candidate.y.coerceIn(-maxY, maxY),
    )
}

/** Tag of the fullscreen viewer. */
internal const val FULLSCREEN_TAG = "dashboard.fullscreen"

/** Tag of the close button of the fullscreen viewer. */
internal const val FULLSCREEN_CLOSE_TAG = "dashboard.fullscreen.close"

/** Tag of the download button of the fullscreen viewer. */
internal const val FULLSCREEN_DOWNLOAD_TAG = "dashboard.fullscreen.download"

/** Zoom limits of the fullscreen viewer. */
private const val MIN_ZOOM = 1f
private const val MAX_ZOOM = 4f

/** Zoom of a double tap in the fullscreen viewer. */
private const val DOUBLE_TAP_ZOOM = 2.5f
