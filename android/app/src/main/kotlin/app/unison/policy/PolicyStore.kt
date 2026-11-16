package app.unison.policy

import android.content.Context
import androidx.core.content.edit

/** Keeps the policy table of this device. */
class PolicyStore(context: Context) {
    private val prefs = context.applicationContext
        .getSharedPreferences(FILE, Context.MODE_PRIVATE)

    /** Reads the stored table, every missing or broken value falls back to the default. */
    fun load(): PolicyConfig {
        val defaults = PolicyDefaults.config
        val rows = PolicyCondition.entries.associateWith { condition ->
            val row = defaults[condition]
            val cells = PolicyKind.entries.associateWith { kind ->
                val cell = row[kind]
                PolicyCell(
                    mode = PolicyMode.fromKey(prefs.getString(PolicyKeys.mode(condition, kind), null))
                        ?: cell.mode,
                    maxSizeBytes = prefs.getLong(PolicyKeys.maxSize(condition, kind), cell.maxSizeBytes)
                        .takeIf { it > 0 } ?: cell.maxSizeBytes,
                )
            }
            PolicyRow(
                cells = cells,
                relay = prefs.getBoolean(PolicyKeys.relay(condition), row.relay),
            )
        }
        return PolicyConfig(
            rows = rows,
            relayMaxSizeBytes = prefs.getLong(PolicyKeys.RELAY_MAX_SIZE, defaults.relayMaxSizeBytes)
                .takeIf { it > 0 } ?: defaults.relayMaxSizeBytes,
        )
    }

    /** Stores the whole table. */
    fun save(config: PolicyConfig) {
        prefs.edit {
            for (condition in PolicyCondition.entries) {
                val row = config[condition]
                putBoolean(PolicyKeys.relay(condition), row.relay)
                for (kind in PolicyKind.entries) {
                    val cell = row[kind]
                    putString(PolicyKeys.mode(condition, kind), cell.mode.key)
                    putLong(PolicyKeys.maxSize(condition, kind), cell.maxSizeBytes)
                }
            }
            putLong(PolicyKeys.RELAY_MAX_SIZE, config.relayMaxSizeBytes)
        }
    }

    /** Removes the stored table and returns the defaults. */
    fun reset(): PolicyConfig {
        prefs.edit {
            for (key in PolicyKeys.all()) {
                remove(key)
            }
        }
        return PolicyDefaults.config
    }

    private companion object {
        const val FILE = "unison.policy"
    }
}
