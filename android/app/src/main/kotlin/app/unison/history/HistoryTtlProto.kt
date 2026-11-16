package app.unison.history

import unison.local.v1.Local

/** Lifetimes of the core settings and the requests that change them. */
object HistoryTtlProto {
    /** Lifetimes reported by the core. */
    fun values(settings: Local.Settings): HistoryTtl = HistoryTtl(
        text = settings.historyTtlTextSeconds,
        image = settings.historyTtlImageSeconds,
        files = settings.historyTtlFilesSeconds,
    )

    /** Request that changes the lifetime of one kind. */
    fun request(kind: Local.Kind, seconds: Long): Local.UpdateSettingsRequest =
        Local.UpdateSettingsRequest.newBuilder()
            .apply {
                when (kind) {
                    Local.Kind.KIND_IMAGE -> setHistoryTtlImageSeconds(seconds)
                    Local.Kind.KIND_FILES -> setHistoryTtlFilesSeconds(seconds)
                    else -> setHistoryTtlTextSeconds(seconds)
                }
            }
            .build()

    /** Request that drops the stored lifetimes. */
    fun reset(): Local.UpdateSettingsRequest = Local.UpdateSettingsRequest.newBuilder()
        .setResetHistoryTtl(true)
        .build()
}
