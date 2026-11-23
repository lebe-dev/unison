package app.unison.ui.screens

import android.content.ClipData
import android.content.ClipboardManager
import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.diagnostics.Journal
import app.unison.diagnostics.JournalDisplay
import app.unison.diagnostics.JournalExport
import app.unison.diagnostics.JournalRecord
import app.unison.policy.PolicyConfig
import app.unison.ui.SectionTitle
import app.unison.ui.state.CoreState
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import unison.local.v1.Local

/** The application log: its switch, the display level, the export and the records. */
@Composable
internal fun JournalSection(
    enabled: Boolean,
    onEnabledChange: (Boolean) -> Unit,
    state: CoreState,
    policy: PolicyConfig,
) {
    val context = LocalContext.current
    val clipboard = context.getSystemService(ClipboardManager::class.java)
    val scope = rememberCoroutineScope()
    var display by rememberSaveable { mutableStateOf(JournalDisplay.INFO) }
    var records by remember { mutableStateOf(emptyList<JournalRecord>()) }
    val clipLabel = stringResource(R.string.settings_page_journal)

    LaunchedEffect(enabled, display) {
        while (true) {
            records =
                withContext(Dispatchers.IO) {
                    Journal
                        .records(context, backups = false)
                        .filter { display.shows(it.level) }
                        .asReversed()
                        .take(MAX_RECORDS)
                }
            delay(REFRESH_MS)
        }
    }

    SectionTitle(text = stringResource(R.string.settings_page_journal))
    SettingsSwitch(
        title = stringResource(R.string.settings_journal),
        hint = stringResource(R.string.settings_journal_hint),
        checked = enabled,
        tag = JOURNAL_TOGGLE_TAG,
        onChange = onEnabledChange,
    )
    if (!enabled) {
        Text(
            text = stringResource(R.string.journal_disabled),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }

    Text(
        text = stringResource(R.string.journal_level),
        style = MaterialTheme.typography.bodyMedium,
        modifier = Modifier.padding(top = 12.dp, bottom = 4.dp),
    )
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(
            selected = display == JournalDisplay.INFO,
            onClick = { display = JournalDisplay.INFO },
            label = { Text(text = stringResource(R.string.journal_level_info)) },
            modifier = Modifier.testTag(JOURNAL_INFO_TAG),
        )
        FilterChip(
            selected = display == JournalDisplay.DEBUG,
            onClick = { display = JournalDisplay.DEBUG },
            label = { Text(text = stringResource(R.string.journal_level_debug)) },
            modifier = Modifier.testTag(JOURNAL_DEBUG_TAG),
        )
    }

    Row(
        modifier =
            Modifier
                .fillMaxWidth()
                .padding(top = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        OutlinedButton(
            onClick = {
                scope.launch {
                    try {
                        val text =
                            withContext(Dispatchers.IO) {
                                JournalExport.text(
                                    context = context,
                                    status = state.status,
                                    settings = state.settings ?: Local.Settings.getDefaultInstance(),
                                    policy = policy,
                                    relay = state.relay ?: Local.Relay.getDefaultInstance(),
                                )
                            }
                        val manager =
                            clipboard
                                ?: throw IllegalStateException("clipboard is not available")
                        manager.setPrimaryClip(
                            ClipData.newPlainText(clipLabel, text),
                        )
                        Toast.makeText(context, R.string.journal_copied, Toast.LENGTH_SHORT).show()
                    } catch (e: Exception) {
                        Journal.warn("copy of the application log failed", "error" to e.message)
                        Toast.makeText(context, R.string.journal_copy_failed, Toast.LENGTH_SHORT).show()
                    }
                }
            },
            modifier = Modifier.testTag(JOURNAL_COPY_TAG),
        ) {
            Text(text = stringResource(R.string.action_copy))
        }
        OutlinedButton(
            onClick = {
                scope.launch {
                    try {
                        val file =
                            withContext(Dispatchers.IO) {
                                val text =
                                    JournalExport.text(
                                        context = context,
                                        status = state.status,
                                        settings = state.settings ?: Local.Settings.getDefaultInstance(),
                                        policy = policy,
                                        relay = state.relay ?: Local.Relay.getDefaultInstance(),
                                    )
                                JournalExport.write(context, text)
                            }
                        context.startActivity(JournalExport.shareIntent(context, file))
                    } catch (e: Exception) {
                        Journal.warn("share of the application log failed", "error" to e.message)
                        Toast.makeText(context, R.string.journal_share_failed, Toast.LENGTH_SHORT).show()
                    }
                }
            },
            modifier = Modifier.testTag(JOURNAL_SHARE_TAG),
        ) {
            Text(text = stringResource(R.string.journal_share))
        }
    }

    if (records.isEmpty()) {
        Text(
            text = stringResource(R.string.journal_empty),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 12.dp),
        )
    } else {
        SelectionContainer {
            Column(
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .padding(top = 12.dp)
                        .testTag(JOURNAL_LIST_TAG),
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                for (record in records) {
                    Text(
                        text = record.text,
                        style = MaterialTheme.typography.bodySmall,
                        fontFamily = FontFamily.Monospace,
                    )
                }
            }
        }
    }
}

/** Tag of the switch of the application log. */
internal const val JOURNAL_TOGGLE_TAG = "settings.journal"

/** Tag of the display level that hides the debug records. */
internal const val JOURNAL_INFO_TAG = "settings.journal.info"

/** Tag of the display level that shows every record. */
internal const val JOURNAL_DEBUG_TAG = "settings.journal.debug"

/** Tag of the button that copies the log. */
internal const val JOURNAL_COPY_TAG = "settings.journal.copy"

/** Tag of the button that shares the log. */
internal const val JOURNAL_SHARE_TAG = "settings.journal.share"

/** Tag of the list of the records. */
internal const val JOURNAL_LIST_TAG = "settings.journal.records"

/** Records the page renders at most, the newest first. */
private const val MAX_RECORDS = 500

/** Time between two reads while the page is visible. */
private const val REFRESH_MS = 2_000L
