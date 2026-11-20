package app.unison.bridge

import android.content.Context
import app.unison.R
import app.unison.history.HistoryText
import unison.local.v1.Local

/** Turns history entries into the items the keyboard shows. */
object BridgeSummaries {
    /** Kind constant of the contract for an item kind. */
    fun kind(kind: Local.Kind): Int =
        when (kind) {
            Local.Kind.KIND_TEXT -> BridgeContract.KIND_TEXT
            Local.Kind.KIND_IMAGE -> BridgeContract.KIND_IMAGE
            Local.Kind.KIND_FILES -> BridgeContract.KIND_FILES
            else -> BridgeContract.KIND_UNSPECIFIED
        }

    /** Summary of an entry. The content itself stays behind openItem. */
    fun of(
        context: Context,
        item: Local.ClipItem,
        deviceName: String,
    ): ClipSummary {
        val preview =
            when (item.kind) {
                Local.Kind.KIND_FILES -> {
                    val names = HistoryText.fileNames(item)
                    val summary =
                        if (names.rest > 0) {
                            context.resources.getQuantityString(
                                R.plurals.names_more,
                                names.rest,
                                names.shown,
                                names.rest,
                            )
                        } else {
                            names.shown
                        }
                    HistoryText.preview(summary)
                }
                else -> HistoryText.text(item)
            }
        val mimeTypes =
            item.representationsList
                .map { it.mimeType }
                .filter { it.isNotEmpty() }
                .toTypedArray()
        return ClipSummary(
            item.id,
            deviceName,
            kind(item.kind),
            preview,
            mimeTypes,
            item.createdAtUnixMs,
        )
    }
}
