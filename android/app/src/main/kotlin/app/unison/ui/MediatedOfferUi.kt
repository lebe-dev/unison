package app.unison.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import app.unison.R
import app.unison.ui.pairing.SasCode
import unison.local.v1.Local

/** Device, role, SAS code and warning details of a mediated introduction offer. */
@Composable
internal fun MediatedOfferDetails(
    pair: Local.MediatedPair,
    showSasHint: Boolean = false,
) {
    val peer = pair.peer
    Text(
        text =
            stringResource(
                R.string.mediated_offer_device,
                platformLabel(peer.platform),
                shortId(peer.deviceId),
            ),
        style = MaterialTheme.typography.bodyMedium,
    )
    roleLabel(pair.role)?.let { role ->
        Text(
            text = stringResource(R.string.mediated_offer_role, role),
            style = MaterialTheme.typography.bodyMedium,
        )
    }
    if (pair.hasSas()) {
        if (showSasHint) {
            Text(
                text = stringResource(R.string.mediated_offer_sas_hint),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        SasCode(sas = pair.sas)
    }
    if (pair.alreadyPaired) {
        Text(
            text = stringResource(R.string.mediated_offer_already_paired),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.error,
        )
    }
}

/**
 * Choice to turn the search for Friends devices on after a Friends
 * introduction. On by default, as the introduction needs it to connect.
 */
@Composable
internal fun FriendsDiscoveryChoice(
    checked: Boolean,
    onChange: (Boolean) -> Unit,
) {
    Row(
        modifier =
            Modifier
                .fillMaxWidth()
                .toggleable(value = checked, role = Role.Checkbox, onValueChange = onChange),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Checkbox(checked = checked, onCheckedChange = null)
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = stringResource(R.string.mediated_offer_friends_discovery),
                style = MaterialTheme.typography.bodyMedium,
            )
            Text(
                text = stringResource(R.string.mediated_offer_friends_discovery_hint),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** First characters of a device ID. */
internal fun shortId(deviceId: String): String = deviceId.take(SHORT_ID_LENGTH)

/** Characters of a device ID shown when the name is empty. */
private const val SHORT_ID_LENGTH = 8
