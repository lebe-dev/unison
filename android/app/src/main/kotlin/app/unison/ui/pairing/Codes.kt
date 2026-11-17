package app.unison.ui.pairing

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.unison.R
import app.unison.ui.state.PairingRequest
import com.google.zxing.WriterException
import com.google.zxing.qrcode.decoder.ErrorCorrectionLevel
import com.google.zxing.qrcode.encoder.Encoder
import unison.local.v1.Local

/** Quiet zone of the code, in modules. */
private const val QUIET_ZONE = 2

/** Test tag of the drawn code. */
const val PAIRING_CODE_TAG = "pairing-code"

/** Modules of a pairing code, true for a dark module, null when the text does not encode. */
internal fun pairingCodeModules(text: String): Array<BooleanArray>? = try {
    val matrix = Encoder.encode(text, ErrorCorrectionLevel.M).matrix
    Array(matrix.height) { y -> BooleanArray(matrix.width) { x -> matrix.get(x, y).toInt() == 1 } }
} catch (_: WriterException) {
    null
}

/** Square of a pairing code for another device to scan. */
@Composable
fun PairingCode(text: String, modifier: Modifier = Modifier) {
    val modules = remember(text) { pairingCodeModules(text) }
    if (modules == null) {
        Text(
            text = stringResource(R.string.pairing_code_failed),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.error,
        )
        return
    }
    Canvas(
        modifier = modifier
            .fillMaxWidth()
            .aspectRatio(1f)
            .testTag(PAIRING_CODE_TAG),
    ) {
        drawRect(color = Color.White, size = size)
        val total = modules.size + QUIET_ZONE * 2
        val module = size.minDimension / total
        val left = (size.width - module * total) / 2 + module * QUIET_ZONE
        val top = (size.height - module * total) / 2 + module * QUIET_ZONE
        for (y in modules.indices) {
            for (x in modules[y].indices) {
                if (!modules[y][x]) {
                    continue
                }
                drawRect(
                    color = Color.Black,
                    topLeft = Offset(left + x * module, top + y * module),
                    size = Size(module, module),
                )
            }
        }
    }
}

/** Digits of a code in groups of three. */
internal fun formatSasDigits(digits: String): String =
    digits.chunked(SAS_GROUP).joinToString(" ")

/** Short authentication string of an exchange: six digits and four emoji. */
@Composable
fun SasCode(sas: Local.Sas, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = formatSasDigits(sas.digits),
            style = MaterialTheme.typography.displaySmall,
            fontFamily = FontFamily.Monospace,
            letterSpacing = 4.sp,
            textAlign = TextAlign.Center,
            modifier = Modifier
                .clip(MaterialTheme.shapes.large)
                .background(MaterialTheme.colorScheme.surfaceContainerHighest)
                .padding(horizontal = 20.dp, vertical = 12.dp),
        )
        if (sas.emojiCount > 0) {
            Text(
                text = sas.emojiList.joinToString(" "),
                style = MaterialTheme.typography.headlineSmall,
                textAlign = TextAlign.Center,
            )
        }
    }
}

/** Body of the confirmation dialog of a pairing request. */
@Composable
fun PairingRequestBody(request: PairingRequest, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            text = stringResource(
                if (request.sas != null) {
                    R.string.pairing_add_body_sas
                } else {
                    R.string.pairing_add_body_qr
                },
            ),
            style = MaterialTheme.typography.bodyMedium,
        )
        request.sas?.let { SasCode(it) }
    }
}

/** Digits of one group of a short authentication string. */
private const val SAS_GROUP = 3
