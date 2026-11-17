package app.unison.service

/** Device names in the form the core accepts. */
object DeviceNames {
    /** Longest name the core takes at start, in UTF-8 bytes. */
    const val MAX_BYTES = 128

    /**
     * Trims the name and cuts it to [MAX_BYTES] on a character boundary. The
     * core refuses to start with a longer name, so a stored one must never
     * exceed the limit.
     */
    fun fit(name: String): String {
        val trimmed = name.trim()
        var bytes = 0
        var end = 0
        while (end < trimmed.length) {
            val codePoint = trimmed.codePointAt(end)
            val size = utf8Size(codePoint)
            if (bytes + size > MAX_BYTES) {
                break
            }
            bytes += size
            end += Character.charCount(codePoint)
        }
        return trimmed.substring(0, end).trimEnd()
    }

    private fun utf8Size(codePoint: Int): Int = when {
        codePoint < 0x80 -> 1
        codePoint < 0x800 -> 2
        codePoint < 0x10000 -> 3
        else -> 4
    }
}
