package app.unison.ui.screens

import android.text.format.DateUtils
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.history.HistoryText
import app.unison.ui.ErrorCard
import app.unison.ui.FriendsDiscoveryChoice
import app.unison.ui.IconTile
import app.unison.ui.MediatedOfferDetails
import app.unison.ui.ScreenTitle
import app.unison.ui.groupItemColor
import app.unison.ui.mediatedStateLabel
import app.unison.ui.shortId
import app.unison.ui.sizeUnits
import app.unison.ui.state.MediatedState
import app.unison.ui.state.RequestsState
import app.unison.ui.state.isInProgress
import unison.local.v1.Local

/** Offers of Friends and introductions through a trusted device that wait for an answer. */
@Composable
fun RequestsScreen(
    requests: RequestsState,
    onRefresh: () -> Unit,
    onAccept: (Local.ShareRequest) -> Unit,
    onDecline: (Local.ShareRequest) -> Unit,
    modifier: Modifier = Modifier,
    mediated: MediatedState = MediatedState(),
    onAcceptMediated: (Local.MediatedPair, Boolean) -> Unit = { _, _ -> },
    onDeclineMediated: (Local.MediatedPair) -> Unit = {},
) {
    Column(
        modifier =
            modifier
                .fillMaxSize()
                .padding(horizontal = 16.dp),
    ) {
        ScreenTitle(title = stringResource(R.string.nav_requests)) {
            TextButton(onClick = onRefresh) {
                Text(text = stringResource(R.string.action_refresh))
            }
        }

        val failure = requests.failure ?: mediated.failure
        if (failure != null) {
            Spacer(modifier = Modifier.height(16.dp))
            ErrorCard(message = stringResource(failure.message), onRetry = onRefresh)
        }

        if (requests.loading && !requests.loaded && mediated.incoming.isEmpty()) {
            Box(
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .padding(vertical = 32.dp),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator()
            }
            return@Column
        }

        if (requests.isEmpty && mediated.incoming.isEmpty()) {
            if (requests.loaded || (mediated.loaded && !requests.loading)) {
                EmptyRequests()
                return@Column
            }
        }

        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            verticalArrangement = Arrangement.spacedBy(12.dp),
            contentPadding = PaddingValues(bottom = 16.dp),
        ) {
            items(mediated.incoming, key = { "mediated:${it.sessionId}" }) { pair ->
                MediatedOfferRow(
                    pair = pair,
                    answering = pair.sessionId in mediated.answering,
                    onAccept = { discovery -> onAcceptMediated(pair, discovery) },
                    onDecline = { onDeclineMediated(pair) },
                )
            }
            items(requests.requests, key = { "request:${it.id}" }) { request ->
                RequestRow(
                    request = request,
                    answering = request.id in requests.answering,
                    onAccept = { onAccept(request) },
                    onDecline = { onDecline(request) },
                )
            }
        }
    }
}

/** One introduction with its devices, its code and the answer buttons. */
@Composable
private fun MediatedOfferRow(
    pair: Local.MediatedPair,
    answering: Boolean,
    onAccept: (Boolean) -> Unit,
    onDecline: () -> Unit,
) {
    var discovery by rememberSaveable(pair.sessionId) {
        mutableStateOf(pair.role == Local.Role.ROLE_FRIENDS)
    }
    val waiting = pair.state == Local.MediatedPairState.MEDIATED_PAIR_STATE_WAITING
    val name = pair.peer.name.ifBlank { shortId(pair.peer.deviceId) }
    val mediator = pair.mediator.name.ifBlank { shortId(pair.mediator.deviceId) }

    OfferCard {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            IconTile(
                icon = R.drawable.ic_nav_devices,
                container = MaterialTheme.colorScheme.secondaryContainer,
                content = MaterialTheme.colorScheme.onSecondaryContainer,
                size = 40.dp,
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(text = stringResource(R.string.mediated_offer_title, name), style = MaterialTheme.typography.titleMedium)
                Text(
                    text = stringResource(R.string.mediated_offer_from, mediator),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (answering || pair.isInProgress) {
                CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
            }
        }

        MediatedOfferDetails(pair = pair)

        if (!waiting) {
            Text(
                text = mediatedStateLabel(pair.state),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            return@OfferCard
        }

        if (pair.role == Local.Role.ROLE_FRIENDS) {
            FriendsDiscoveryChoice(checked = discovery, onChange = { discovery = it })
        }

        OfferExpiryText(expiresAtUnixMs = pair.expiresAtUnixMs)
        OfferActionButtons(
            enabled = !answering,
            onDecline = onDecline,
            onAccept = { onAccept(discovery) },
        )
    }
}

/** One offer with its files, its deadline and the answer buttons. */
@Composable
private fun RequestRow(
    request: Local.ShareRequest,
    answering: Boolean,
    onAccept: () -> Unit,
    onDecline: () -> Unit,
) {
    OfferCard {
        val files = request.filesList
        val size = HistoryText.formatSize(request.totalBytes, sizeUnits())
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            IconTile(
                icon = R.drawable.ic_kind_file,
                container = MaterialTheme.colorScheme.tertiaryContainer,
                content = MaterialTheme.colorScheme.onTertiaryContainer,
                size = 40.dp,
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = request.deviceName.ifBlank { shortId(request.deviceId) },
                    style = MaterialTheme.typography.titleMedium,
                )
                Text(
                    text = pluralStringResource(R.plurals.request_files, files.size, files.size, size),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (answering) {
                CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
            }
        }

        val names = files.mapNotNull { it.name.ifBlank { null } }
        if (names.isNotEmpty()) {
            Text(
                text = names.joinToString(", "),
                style = MaterialTheme.typography.bodyMedium,
                maxLines = 3,
                overflow = TextOverflow.Ellipsis,
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .clip(MaterialTheme.shapes.medium)
                        .background(MaterialTheme.colorScheme.surfaceContainer)
                        .padding(horizontal = 14.dp, vertical = 12.dp),
            )
        }

        OfferExpiryText(expiresAtUnixMs = request.expiresAtUnixMs)
        OfferActionButtons(
            enabled = !answering,
            onDecline = onDecline,
            onAccept = onAccept,
        )
    }
}

/** Shared card container for file and introduction offers. */
@Composable
private fun OfferCard(content: @Composable ColumnScope.() -> Unit) {
    Column(
        modifier =
            Modifier
                .fillMaxWidth()
                .clip(MaterialTheme.shapes.large)
                .background(groupItemColor)
                .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
        content = content,
    )
}

/** Relative expiration label of an offer. */
@Composable
private fun OfferExpiryText(expiresAtUnixMs: Long) {
    if (expiresAtUnixMs <= 0) {
        return
    }
    Text(
        text =
            stringResource(
                R.string.requests_expires,
                DateUtils
                    .getRelativeTimeSpanString(
                        expiresAtUnixMs,
                        System.currentTimeMillis(),
                        DateUtils.MINUTE_IN_MILLIS,
                    ).toString(),
            ),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

/** Decline and accept button row of an offer card. */
@Composable
private fun OfferActionButtons(
    enabled: Boolean,
    onDecline: () -> Unit,
    onAccept: () -> Unit,
) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        OutlinedButton(
            onClick = onDecline,
            enabled = enabled,
            modifier =
                Modifier
                    .weight(1f)
                    .heightIn(min = 48.dp),
        ) {
            Text(text = stringResource(R.string.action_decline))
        }
        Button(
            onClick = onAccept,
            enabled = enabled,
            modifier =
                Modifier
                    .weight(1f)
                    .heightIn(min = 48.dp),
        ) {
            Text(text = stringResource(R.string.action_accept))
        }
    }
}

/** Explanation shown when nothing waits for an answer. */
@Composable
private fun EmptyRequests() {
    Column(
        modifier =
            Modifier
                .fillMaxSize()
                .padding(bottom = 48.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = stringResource(R.string.requests_empty),
            style = MaterialTheme.typography.titleMedium,
        )
        Text(
            text = stringResource(R.string.requests_empty_body),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 8.dp),
        )
    }
}
