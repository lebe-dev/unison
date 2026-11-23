package app.unison.ui.state

/** Validation of the device name the core accepts. */
object DeviceNames {
    /** Longest accepted name in bytes. */
    const val MAX_BYTES = 128

    /** True when the core accepts [name]. */
    fun valid(name: String): Boolean {
        val trimmed = name.trim()
        return trimmed.isNotEmpty() && trimmed.toByteArray(Charsets.UTF_8).size <= MAX_BYTES
    }
}
