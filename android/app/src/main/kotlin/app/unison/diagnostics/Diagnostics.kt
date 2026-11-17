package app.unison.diagnostics

import android.content.Context
import android.util.Log
import app.unison.BuildConfig
import app.unison.service.UnisonSettings
import io.sentry.DataCollection
import io.sentry.KeyValueCollectionBehavior
import io.sentry.Sentry
import io.sentry.SentryEvent
import io.sentry.SentryOptions
import io.sentry.android.core.SentryAndroid

/**
 * Reports crashes and handled failures to the developer.
 *
 * Nothing leaves the device until the user allows it. The build carries the
 * DSN, an empty one keeps the SDK stopped.
 */
object Diagnostics {
    private const val TAG = "Diagnostics"

    /** Tag of the binary that reports the events. */
    private const val COMPONENT = "android"

    private const val COMPONENT_TAG = "component"

    /** Consent of the user, mirrored in [UnisonSettings]. */
    @Volatile
    private var allowed = false

    /** True when the build has a DSN and the SDK started. */
    @Volatile
    private var started = false

    /** Starts the SDK when the build has a DSN. */
    fun start(context: Context) {
        allowed = UnisonSettings.sentryEnabled(context)
        val dsn = BuildConfig.SENTRY_DSN
        if (dsn.isEmpty()) {
            return
        }
        try {
            SentryAndroid.init(context) { options ->
                options.dsn = dsn
                options.release = BuildConfig.RELEASE
                options.environment = BuildConfig.ENVIRONMENT
                options.isAttachServerName = false
                options.isSendClientReports = false
                options.isEnableAutoSessionTracking = false
                options.isEnableNdk = false

                // Uncaught exceptions and ANRs are the events this app reports.
                options.isEnableUncaughtExceptionHandler = true
                options.isAnrEnabled = true

                // Only the stack, the release, the environment and the tags remain.
                options.maxBreadcrumbs = 0
                options.isEnableAppLifecycleBreadcrumbs = false
                options.isEnableActivityLifecycleBreadcrumbs = false
                options.isEnableSystemEventBreadcrumbs = false
                options.isEnableUserInteractionBreadcrumbs = false
                options.isEnableNetworkEventBreadcrumbs = false

                // No tracing or performance collection.
                options.tracesSampleRate = null
                options.isEnableAutoActivityLifecycleTracing = false
                options.isEnableUserInteractionTracing = false

                options.dataCollection = noDataCollection()
                options.beforeSend = SentryOptions.BeforeSendCallback { event, _ -> beforeSend(event) }
            }
            started = true
        } catch (e: Exception) {
            Log.w(TAG, "the SDK did not start: ${e.message}")
        }
    }

    /** Grants or withdraws the consent and stores it for the next start. */
    fun setConsent(context: Context, enabled: Boolean) {
        UnisonSettings.setSentryEnabled(context, enabled)
        allowed = enabled
    }

    /** Reports a handled failure when the build reports and the user allows it. */
    fun capture(error: Throwable) {
        if (!started || !allowed) {
            return
        }
        Sentry.captureException(error)
    }

    /** Drops the events without consent and removes the data the app never sends. */
    private fun beforeSend(event: SentryEvent): SentryEvent? {
        if (!allowed) {
            return null
        }
        event.user = null
        event.serverName = null
        event.request = null
        event.breadcrumbs = null
        event.extras = null
        event.contexts.remove("device")
        event.message?.let { message ->
            message.formatted = message.formatted?.let(::redactPaths)
            message.message = message.message?.let(::redactPaths)
        }
        event.exceptions?.forEach { exception ->
            exception.value = exception.value?.let(::redactPaths)
        }
        event.setTag(COMPONENT_TAG, COMPONENT)
        return event
    }

    /** Turns off the cookies, headers, query parameters and bodies of the automatic collection. */
    private fun noDataCollection(): DataCollection = DataCollection().apply {
        userInfo = false
        filePaths = false
        databaseQueryData = false
        cookies = KeyValueCollectionBehavior.off()
        urlQueryParams = KeyValueCollectionBehavior.off()
        httpBodies = emptySet()
        httpHeaders.request = KeyValueCollectionBehavior.off()
        httpHeaders.response = KeyValueCollectionBehavior.off()
    }
}
