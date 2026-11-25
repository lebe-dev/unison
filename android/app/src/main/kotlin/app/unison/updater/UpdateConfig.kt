package app.unison.updater

import android.content.Context
import androidx.core.content.edit
import app.unison.service.UnisonSettings

/** How often the app looks for a new release in the background. */
enum class UpdateCheckInterval(
    val value: String,
    val hours: Long,
) {
    SIX_HOURS("six_hours", 6L),
    DAILY("daily", 24L),
    WEEKLY("weekly", 168L),
    ;

    companion object {
        val DEFAULT = DAILY

        /** The interval of a stored value, or the default one. */
        fun fromValue(value: String?): UpdateCheckInterval = entries.firstOrNull { it.value == value } ?: DEFAULT
    }
}

/** The update behavior of this device and the state of the last check. */
data class UpdateConfig(
    val enabled: Boolean = false,
    val betaChannel: Boolean = false,
    val interval: UpdateCheckInterval = UpdateCheckInterval.DEFAULT,
    val lastCheckedMs: Long = 0L,
    val dismissedVersion: String? = null,
    val lastEtag: String? = null,
    val cachedRelease: ReleaseInfo? = null,
) {
    /**
     * The same settings on the other channel. The cached answer of the old
     * channel is dropped, so the next check reads the new one from scratch.
     */
    fun withBetaChannel(value: Boolean): UpdateConfig {
        if (value == betaChannel) {
            return this
        }
        return copy(
            betaChannel = value,
            dismissedVersion = null,
            lastEtag = null,
            cachedRelease = null,
        )
    }

    /** The cached release when it is newer than [currentVersion]. */
    fun cachedUpdate(currentVersion: String): ReleaseInfo? {
        val release = cachedRelease ?: return null
        val candidate = SemVer.parse(release.version) ?: return null
        val current = SemVer.parse(currentVersion) ?: return null
        return release.takeIf { candidate > current }
    }

    companion object {
        val DEFAULT = UpdateConfig()

        /** The stored settings of the update checks. */
        fun load(context: Context): UpdateConfig {
            val prefs = context.getSharedPreferences(UnisonSettings.FILE, Context.MODE_PRIVATE)
            return UpdateConfig(
                enabled = prefs.getBoolean(UnisonSettings.KEY_UPDATES_ENABLED, false),
                betaChannel = prefs.getBoolean(UnisonSettings.KEY_UPDATES_BETA_CHANNEL, false),
                interval = UpdateCheckInterval.fromValue(prefs.getString(UnisonSettings.KEY_UPDATES_INTERVAL, null)),
                lastCheckedMs = prefs.getLong(UnisonSettings.KEY_UPDATES_LAST_CHECKED_MS, 0L),
                dismissedVersion = prefs.getString(UnisonSettings.KEY_UPDATES_DISMISSED_VERSION, null),
                lastEtag = prefs.getString(UnisonSettings.KEY_UPDATES_LAST_ETAG, null),
                cachedRelease =
                    prefs
                        .getString(UnisonSettings.KEY_UPDATES_CACHED_RELEASE, null)
                        ?.let(ReleaseInfo::fromJson),
            )
        }

        /** Stores the settings of the update checks atomically. */
        fun save(
            context: Context,
            config: UpdateConfig,
        ) {
            context.getSharedPreferences(UnisonSettings.FILE, Context.MODE_PRIVATE).edit {
                putBoolean(UnisonSettings.KEY_UPDATES_ENABLED, config.enabled)
                putBoolean(UnisonSettings.KEY_UPDATES_BETA_CHANNEL, config.betaChannel)
                putString(UnisonSettings.KEY_UPDATES_INTERVAL, config.interval.value)
                putLong(UnisonSettings.KEY_UPDATES_LAST_CHECKED_MS, config.lastCheckedMs)
                putOrRemove(UnisonSettings.KEY_UPDATES_DISMISSED_VERSION, config.dismissedVersion)
                putOrRemove(UnisonSettings.KEY_UPDATES_LAST_ETAG, config.lastEtag)
                putOrRemove(UnisonSettings.KEY_UPDATES_CACHED_RELEASE, config.cachedRelease?.toJson())
            }
        }

        private fun android.content.SharedPreferences.Editor.putOrRemove(
            key: String,
            value: String?,
        ) {
            if (value == null) remove(key) else putString(key, value)
        }
    }
}
