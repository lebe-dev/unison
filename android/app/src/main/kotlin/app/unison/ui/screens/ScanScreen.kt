package app.unison.ui.screens

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.util.Log
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.mlkit.vision.MlKitAnalyzer
import androidx.camera.view.PreviewView
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleOwner
import app.unison.R
import app.unison.ui.ScreenTitle
import com.google.mlkit.vision.barcode.BarcodeScanner
import com.google.mlkit.vision.barcode.BarcodeScannerOptions
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Camera scanner of pairing codes. The camera permission is asked when the
 * screen opens and the preview takes its place once the permission is granted.
 */
@Composable
fun ScanScreen(
    onCode: (String) -> Unit,
    onClose: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    var granted by remember { mutableStateOf(hasCameraPermission(context)) }
    var unavailable by remember { mutableStateOf(false) }
    val askPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {
        granted = it
    }

    LaunchedEffect(Unit) {
        if (!granted) {
            askPermission.launch(Manifest.permission.CAMERA)
        }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp),
    ) {
        ScreenTitle(title = stringResource(R.string.pairing_scan_title)) {
            TextButton(onClick = onClose) {
                Text(text = stringResource(R.string.action_cancel))
            }
        }
        Text(
            text = stringResource(R.string.pairing_scan_body),
            style = MaterialTheme.typography.bodyMedium,
        )
        Spacer(modifier = Modifier.height(16.dp))
        when {
            !granted -> CameraCard(
                title = stringResource(R.string.pairing_camera_title),
                body = stringResource(R.string.pairing_camera_body),
                action = stringResource(R.string.pairing_camera_allow),
                onAction = { askPermission.launch(Manifest.permission.CAMERA) },
            )
            unavailable -> CameraCard(
                title = stringResource(R.string.pairing_camera_title),
                body = stringResource(R.string.pairing_camera_denied),
            )
            else -> CameraPreview(
                onCode = onCode,
                onUnavailable = { unavailable = true },
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f)
                    .clip(RoundedCornerShape(16.dp)),
            )
        }
    }
}

/** Explanation of the camera permission, with an action when it can be asked again. */
@Composable
private fun CameraCard(
    title: String,
    body: String,
    action: String? = null,
    onAction: (() -> Unit)? = null,
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surfaceVariant,
        ),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(text = title, style = MaterialTheme.typography.titleMedium)
            Spacer(modifier = Modifier.height(8.dp))
            Text(text = body, style = MaterialTheme.typography.bodyMedium)
            if (action != null && onAction != null) {
                TextButton(onClick = onAction) {
                    Text(text = action)
                }
            }
        }
    }
}

/** Camera preview that reports the first pairing code it reads. */
@Composable
private fun CameraPreview(
    onCode: (String) -> Unit,
    onUnavailable: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val owner = context as? LifecycleOwner
    val previewView = remember {
        PreviewView(context).apply { scaleType = PreviewView.ScaleType.FILL_CENTER }
    }
    val reported = remember { AtomicBoolean(false) }
    val currentOnCode by rememberUpdatedState(onCode)
    val currentOnUnavailable by rememberUpdatedState(onUnavailable)

    DisposableEffect(owner) {
        if (owner == null) {
            currentOnUnavailable()
            return@DisposableEffect onDispose {}
        }
        val executor = ContextCompat.getMainExecutor(context)
        val future = ProcessCameraProvider.getInstance(context)
        var provider: ProcessCameraProvider? = null
        var scanner: BarcodeScanner? = null
        var disposed = false

        future.addListener({
            if (disposed) {
                return@addListener
            }
            try {
                val cameraProvider = future.get()
                provider = cameraProvider
                if (!cameraProvider.hasCamera(CameraSelector.DEFAULT_BACK_CAMERA)) {
                    currentOnUnavailable()
                    return@addListener
                }
                val preview = Preview.Builder().build()
                preview.setSurfaceProvider(previewView.surfaceProvider)
                val barcode = BarcodeScanning.getClient(
                    BarcodeScannerOptions.Builder()
                        .setBarcodeFormats(Barcode.FORMAT_QR_CODE)
                        .build(),
                )
                scanner = barcode
                val analysis = ImageAnalysis.Builder()
                    .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                    .build()
                analysis.setAnalyzer(
                    executor,
                    MlKitAnalyzer(
                        listOf(barcode),
                        ImageAnalysis.COORDINATE_SYSTEM_ORIGINAL,
                        executor,
                    ) { result ->
                        val code = result?.getValue(barcode)?.firstOrNull()?.rawValue
                        if (!code.isNullOrEmpty() && reported.compareAndSet(false, true)) {
                            currentOnCode(code)
                        }
                    },
                )
                cameraProvider.unbindAll()
                cameraProvider.bindToLifecycle(
                    owner,
                    CameraSelector.DEFAULT_BACK_CAMERA,
                    preview,
                    analysis,
                )
            } catch (e: Exception) {
                Log.w(TAG, "camera could not start", e)
                currentOnUnavailable()
            }
        }, executor)

        onDispose {
            disposed = true
            provider?.unbindAll()
            scanner?.close()
        }
    }

    AndroidView(factory = { previewView }, modifier = modifier)
}

/** Whether the app may open the camera. */
private fun hasCameraPermission(context: Context): Boolean =
    ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) ==
        PackageManager.PERMISSION_GRANTED

private const val TAG = "ScanScreen"
