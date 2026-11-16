package app.unison.policy

import unison.local.v1.Local

/** Converts the policy table to the local API and back. */
object PolicyProto {

    /** Table of the settings, the defaults for rows or fields the core omits. */
    fun table(settings: Local.Settings): PolicyConfig {
        val defaults = PolicyDefaults.config
        val rows = PolicyCondition.entries.associateWith { condition ->
            val row = settings.row(condition) ?: return@associateWith defaults[condition]
            val defaultRow = defaults[condition]
            PolicyRow(
                cells = PolicyKind.entries.associateWith { kind ->
                    val cell = row.cell(kind)
                    val defaultCell = defaultRow[kind]
                    PolicyCell(
                        mode = cell?.takeIf { it.hasAction() }?.let { modeOf(it.action) }
                            ?: defaultCell.mode,
                        maxSizeBytes = cell?.takeIf { it.hasMaxSizeBytes() }?.maxSizeBytes
                            ?.takeIf { it > 0 }
                            ?: defaultCell.maxSizeBytes,
                    )
                },
                relay = if (row.hasRelay()) row.relay else defaultRow.relay,
            )
        }
        return PolicyConfig(
            rows = rows,
            relayMaxSizeBytes = settings.relayMaxSizeBytes.takeIf { it > 0 }
                ?: defaults.relayMaxSizeBytes,
        )
    }

    /** Request that stores the whole table. */
    fun request(table: PolicyConfig): Local.UpdateSettingsRequest {
        val request = Local.UpdateSettingsRequest.newBuilder()
            .setRelayMaxSizeBytes(table.relayMaxSizeBytes)
        for (condition in PolicyCondition.entries) {
            val row = table[condition]
            val message = Local.PolicyRow.newBuilder()
                .setText(cell(row[PolicyKind.TEXT]))
                .setImage(cell(row[PolicyKind.IMAGE]))
                .setFiles(cell(row[PolicyKind.FILES]))
                .setRelay(row.relay)
                .build()
            request.setRow(condition, message)
        }
        return request.build()
    }

    /** Request that drops the stored overrides. */
    fun reset(): Local.UpdateSettingsRequest =
        Local.UpdateSettingsRequest.newBuilder().setResetPolicy(true).build()

    /** Message of one cell. */
    private fun cell(cell: PolicyCell): Local.PolicyCell = Local.PolicyCell.newBuilder()
        .setAction(actionOf(cell.mode))
        .setMaxSizeBytes(cell.maxSizeBytes)
        .build()

    /** Request with the row of one condition replaced. */
    private fun Local.UpdateSettingsRequest.Builder.setRow(
        condition: PolicyCondition,
        row: Local.PolicyRow,
    ) {
        when (condition) {
            PolicyCondition.UNMETERED -> setUnmetered(row)
            PolicyCondition.METERED -> setMetered(row)
            PolicyCondition.ROAMING -> setRoaming(row)
            PolicyCondition.DATA_SAVER -> setDataSaver(row)
        }
    }

    /** Row of one condition, null when the core omits it. */
    private fun Local.Settings.row(condition: PolicyCondition): Local.PolicyRow? = when (condition) {
        PolicyCondition.UNMETERED -> if (hasUnmetered()) unmetered else null
        PolicyCondition.METERED -> if (hasMetered()) metered else null
        PolicyCondition.ROAMING -> if (hasRoaming()) roaming else null
        PolicyCondition.DATA_SAVER -> if (hasDataSaver()) dataSaver else null
    }

    /** Cell of one kind, null when the core omits it. */
    private fun Local.PolicyRow.cell(kind: PolicyKind): Local.PolicyCell? = when (kind) {
        PolicyKind.TEXT -> if (hasText()) text else null
        PolicyKind.IMAGE -> if (hasImage()) image else null
        PolicyKind.FILES -> if (hasFiles()) files else null
    }

    /** Message action of a mode. */
    private fun actionOf(mode: PolicyMode): Local.PolicyAction = when (mode) {
        PolicyMode.REJECT -> Local.PolicyAction.POLICY_ACTION_REJECT
        PolicyMode.METADATA -> Local.PolicyAction.POLICY_ACTION_METADATA
        PolicyMode.CONFIRM -> Local.PolicyAction.POLICY_ACTION_CONFIRM
        PolicyMode.AUTO -> Local.PolicyAction.POLICY_ACTION_AUTO
    }

    /** Mode of a message action, null for an action this app does not know. */
    private fun modeOf(action: Local.PolicyAction): PolicyMode? = when (action) {
        Local.PolicyAction.POLICY_ACTION_REJECT -> PolicyMode.REJECT
        Local.PolicyAction.POLICY_ACTION_METADATA -> PolicyMode.METADATA
        Local.PolicyAction.POLICY_ACTION_CONFIRM -> PolicyMode.CONFIRM
        Local.PolicyAction.POLICY_ACTION_AUTO -> PolicyMode.AUTO
        else -> null
    }
}
