package app.unison.policy

/** Content kind of one policy cell. */
enum class PolicyKind {
    TEXT,
    IMAGE,
    FILES,
}

/** Network condition of one policy row. */
enum class PolicyCondition {
    UNMETERED,
    METERED,
    ROAMING,
    DATA_SAVER,
}

/** What happens to an item that arrives without an explicit request. */
enum class PolicyMode {
    /** The item is dropped and not recorded. */
    REJECT,

    /** The item is recorded and downloaded on request. */
    METADATA,

    /** The item is recorded and applied after a confirmation. */
    CONFIRM,

    /** The item is applied without asking. */
    AUTO,
}

/** One cell of the table: the outcome and the largest size fetched eagerly. */
data class PolicyCell(val mode: PolicyMode, val maxSizeBytes: Long)

/** One row of the table. */
data class PolicyRow(val cells: Map<PolicyKind, PolicyCell>, val relay: Boolean) {
    /** Cell of one kind. */
    operator fun get(kind: PolicyKind): PolicyCell = cells.getValue(kind)

    /** Copy with another cell of kind. */
    fun with(kind: PolicyKind, cell: PolicyCell): PolicyRow = copy(cells = cells + (kind to cell))
}

/** The policy table and the relay threshold. */
data class PolicyConfig(val rows: Map<PolicyCondition, PolicyRow>, val relayMaxSizeBytes: Long) {
    /** Row of one condition. */
    operator fun get(condition: PolicyCondition): PolicyRow = rows.getValue(condition)

    /** Copy with another row of condition. */
    fun with(condition: PolicyCondition, row: PolicyRow): PolicyConfig =
        copy(rows = rows + (condition to row))
}

/** Default table: text is applied on every network, images up to the limit, files only on request. */
object PolicyDefaults {
    /** Eager limit on an unmetered network. */
    const val LAN_MAX_SIZE = 10L shl 20

    /** Eager limit on a restricted network and over the relay. */
    const val RELAY_MAX_SIZE = 1L shl 20

    /** Table used until the user changes it. */
    val config: PolicyConfig = PolicyConfig(
        rows = mapOf(
            PolicyCondition.UNMETERED to PolicyRow(
                cells = mapOf(
                    PolicyKind.TEXT to PolicyCell(PolicyMode.AUTO, LAN_MAX_SIZE),
                    PolicyKind.IMAGE to PolicyCell(PolicyMode.AUTO, LAN_MAX_SIZE),
                    PolicyKind.FILES to PolicyCell(PolicyMode.METADATA, LAN_MAX_SIZE),
                ),
                relay = true,
            ),
            PolicyCondition.METERED to PolicyRow(
                cells = mapOf(
                    PolicyKind.TEXT to PolicyCell(PolicyMode.AUTO, RELAY_MAX_SIZE),
                    PolicyKind.IMAGE to PolicyCell(PolicyMode.AUTO, RELAY_MAX_SIZE),
                    PolicyKind.FILES to PolicyCell(PolicyMode.METADATA, RELAY_MAX_SIZE),
                ),
                relay = true,
            ),
            PolicyCondition.ROAMING to PolicyRow(
                cells = mapOf(
                    PolicyKind.TEXT to PolicyCell(PolicyMode.AUTO, RELAY_MAX_SIZE),
                    PolicyKind.IMAGE to PolicyCell(PolicyMode.METADATA, RELAY_MAX_SIZE),
                    PolicyKind.FILES to PolicyCell(PolicyMode.METADATA, RELAY_MAX_SIZE),
                ),
                relay = false,
            ),
            PolicyCondition.DATA_SAVER to PolicyRow(
                cells = mapOf(
                    PolicyKind.TEXT to PolicyCell(PolicyMode.AUTO, RELAY_MAX_SIZE),
                    PolicyKind.IMAGE to PolicyCell(PolicyMode.METADATA, RELAY_MAX_SIZE),
                    PolicyKind.FILES to PolicyCell(PolicyMode.METADATA, RELAY_MAX_SIZE),
                ),
                relay = false,
            ),
        ),
        relayMaxSizeBytes = RELAY_MAX_SIZE,
    )
}

/** Sizes of the table in the unit the screen edits. */
object PolicySize {
    /** Bytes in one megabyte. */
    const val MEGABYTE = 1L shl 20

    /** Bytes in whole megabytes, at least one. */
    fun toMegabytes(bytes: Long): Long = (bytes / MEGABYTE).coerceAtLeast(1)

    /** Bytes of whole megabytes. */
    fun ofMegabytes(megabytes: Long): Long = megabytes.coerceAtLeast(1) * MEGABYTE
}
