package app.unison.service

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * Keeps a set of offer notifications in step with the core, coalescing
 * overlapping refreshes and polling while offers remain active.
 */
internal class OfferNotificationSync<T>(
    private val scope: CoroutineScope,
    private val refreshIntervalMs: Long,
    private val fetch: suspend () -> List<T>?,
    private val keyOf: (T) -> String,
    private val post: (T) -> Unit,
    private val cancel: (String) -> Unit,
) {
    private var job: Job? = null
    private var timer: Job? = null
    private var dirty = false
    private val active = mutableSetOf<String>()

    /** Keys of offers that currently have an active notification. */
    val notifiedIds: Set<String>
        get() = active

    /** Reads the offers from the core and updates the notifications. */
    fun refresh() {
        if (job?.isActive == true) {
            dirty = true
            return
        }
        dirty = false
        job =
            scope
                .launch {
                    val items = fetch()
                    if (items != null) {
                        reconcile(items)
                        scheduleRefresh()
                    }
                }.also { running ->
                    running.invokeOnCompletion { cause ->
                        if (cause == null && dirty) {
                            scope.launch { refresh() }
                        }
                    }
                }
    }

    /** Removes one notification immediately after the user answers it. */
    fun remove(id: String) {
        cancel(id)
        active -= id
    }

    /** Cancels every active notification and clears the tracked set. */
    fun clear() {
        timer?.cancel()
        timer = null
        for (id in active) {
            cancel(id)
        }
        active.clear()
    }

    private fun reconcile(items: List<T>) {
        val current = items.map(keyOf).toSet()
        for (gone in active - current) {
            cancel(gone)
            active -= gone
        }
        for (item in items) {
            active += keyOf(item)
            post(item)
        }
    }

    private fun scheduleRefresh() {
        timer?.cancel()
        if (active.isEmpty()) {
            return
        }
        timer =
            scope.launch {
                delay(refreshIntervalMs)
                refresh()
            }
    }
}
