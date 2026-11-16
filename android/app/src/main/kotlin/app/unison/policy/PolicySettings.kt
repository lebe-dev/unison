package app.unison.policy

/** Content kind of one policy cell. */
enum class PolicyKind(val key: String) {
    TEXT("text"),
    IMAGE("image"),
    FILES("files"),
}

/** Network condition of one policy row. */
enum class PolicyCondition(val key: String) {
    UNMETERED("unmetered"),
    METERED("metered"),
    ROAMING("roaming"),
    DATA_SAVER("data_saver"),
}

/** What happens to an item that arrives without an explicit request. */
enum class PolicyMode(val key: String) {
    /** The item is dropped and not recorded. */
    REJECT("reject"),

    /** The item is recorded and downloaded on request. */
    METADATA("metadata"),

    /** The item is recorded and applied after a confirmation. */
    CONFIRM("confirm"),

    /** The item is applied without asking. */
    AUTO("auto");

    companion object {
        /** Mode of a stored value, null when the value is unknown. */
        fun fromKey(value: String?): PolicyMode? = entries.firstOrNull { it.key == value }
    }
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

/** Settings keys of the policy table. */
object PolicyKeys {
    /** Largest item fetched eagerly over the relay. */
    const val RELAY_MAX_SIZE = "policy.relay.max_size"

    /** Key of the outcome of kind under condition. */
    fun mode(condition: PolicyCondition, kind: PolicyKind): String =
        "policy.network.${condition.key}.${kind.key}.mode"

    /** Key of the eager size limit of kind under condition. */
    fun maxSize(condition: PolicyCondition, kind: PolicyKind): String =
        "policy.network.${condition.key}.${kind.key}.max_size"

    /** Key of the relay permission of condition. */
    fun relay(condition: PolicyCondition): String = "policy.network.${condition.key}.relay"

    /** Every key of the table. */
    fun all(): List<String> = buildList {
        for (condition in PolicyCondition.entries) {
            add(relay(condition))
            for (kind in PolicyKind.entries) {
                add(mode(condition, kind))
                add(maxSize(condition, kind))
            }
        }
        add(RELAY_MAX_SIZE)
    }
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
