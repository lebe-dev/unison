package app.unison.updater

import android.content.Context
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
        fun load(context: Context): UpdateConfig =
            UpdateConfig(
                enabled = UnisonSettings.updatesEnabled(context),
                betaChannel = UnisonSettings.updatesBetaChannel(context),
                interval = UpdateCheckInterval.fromValue(UnisonSettings.updatesInterval(context)),
                lastCheckedMs = UnisonSettings.updatesLastCheckedMs(context),
                dismissedVersion = UnisonSettings.updatesDismissedVersion(context),
                lastEtag = UnisonSettings.updatesLastEtag(context),
                cachedRelease =
                    UnisonSettings
                        .updatesCachedReleaseJson(context)
                        ?.let(ReleaseInfo::fromJson),
            )

        /** Stores the settings of the update checks. */
        fun save(
            context: Context,
            config: UpdateConfig,
        ) {
            UnisonSettings.setUpdatesEnabled(context, config.enabled)
            UnisonSettings.setUpdatesBetaChannel(context, config.betaChannel)
            UnisonSettings.setUpdatesInterval(context, config.interval.value)
            UnisonSettings.setUpdatesLastCheckedMs(context, config.lastCheckedMs)
            UnisonSettings.setUpdatesDismissedVersion(context, config.dismissedVersion)
            UnisonSettings.setUpdatesLastEtag(context, config.lastEtag)
            UnisonSettings.setUpdatesCachedReleaseJson(context, config.cachedRelease?.toJson())
        }
    }
}
