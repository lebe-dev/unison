package app.unison.clipboard

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/** Clipboard items that wait for the user to apply them. */
class HeldItems {
    private val mutable = MutableStateFlow<Set<String>>(emptySet())

    /** IDs of the items that are not applied yet. */
    val ids: StateFlow<Set<String>> = mutable.asStateFlow()

    /** Marks an item as waiting for the user. */
    fun add(itemId: String) {
        if (itemId.isEmpty()) {
            return
        }
        mutable.update { it + itemId }
    }

    /** Forgets an item that was applied. */
    fun remove(itemId: String) {
        mutable.update { it - itemId }
    }

    companion object {
        /** Items of the running process. */
        val shared = HeldItems()
    }
}
