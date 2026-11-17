package app.unison.ui.screens

import android.text.format.DateUtils
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
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
import app.unison.ui.IconTile
import app.unison.ui.ScreenTitle
import app.unison.ui.groupItemColor
import app.unison.ui.sizeUnits
import app.unison.ui.state.RequestsState
import unison.local.v1.Local

/** File offers of Friends that wait for an answer. */
@Composable
fun RequestsScreen(
    requests: RequestsState,
    onRefresh: () -> Unit,
    onAccept: (Local.ShareRequest) -> Unit,
    onDecline: (Local.ShareRequest) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp),
    ) {
        ScreenTitle(title = stringResource(R.string.nav_requests)) {
            TextButton(onClick = onRefresh) {
                Text(text = stringResource(R.string.action_refresh))
            }
        }

        requests.failure?.let { failure ->
            Spacer(modifier = Modifier.height(16.dp))
            ErrorCard(message = stringResource(failure.message), onRetry = onRefresh)
        }

        if (requests.loading && !requests.loaded) {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(vertical = 32.dp),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator()
            }
            return@Column
        }

        if (requests.isEmpty) {
            EmptyRequests()
            return@Column
        }

        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            verticalArrangement = Arrangement.spacedBy(12.dp),
            contentPadding = PaddingValues(bottom = 16.dp),
        ) {
            items(requests.requests, key = { it.id }) { request ->
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

/** One offer with its files, its deadline and the answer buttons. */
@Composable
private fun RequestRow(
    request: Local.ShareRequest,
    answering: Boolean,
    onAccept: () -> Unit,
    onDecline: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(MaterialTheme.shapes.large)
            .background(groupItemColor)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
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
                    text = request.deviceName.ifBlank { request.deviceId.take(SHORT_ID_LENGTH) },
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
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(MaterialTheme.shapes.medium)
                    .background(MaterialTheme.colorScheme.surfaceContainer)
                    .padding(horizontal = 14.dp, vertical = 12.dp),
            )
        }

        if (request.expiresAtUnixMs > 0) {
            Text(
                text = stringResource(
                    R.string.requests_expires,
                    DateUtils.getRelativeTimeSpanString(
                        request.expiresAtUnixMs,
                        System.currentTimeMillis(),
                        DateUtils.MINUTE_IN_MILLIS,
                    ).toString(),
                ),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }

        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(
                onClick = onDecline,
                enabled = !answering,
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = 48.dp),
            ) {
                Text(text = stringResource(R.string.action_decline))
            }
            Button(
                onClick = onAccept,
                enabled = !answering,
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = 48.dp),
            ) {
                Text(text = stringResource(R.string.action_accept))
            }
        }
    }
}

/** Explanation shown when nothing waits for an answer. */
@Composable
private fun EmptyRequests() {
    Column(
        modifier = Modifier
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

/** Characters of a device ID shown when the name is unknown. */
private const val SHORT_ID_LENGTH = 8
