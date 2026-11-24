package app.unison.updater

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import java.util.concurrent.TimeUnit

/** Periodic check for a newer release, run by WorkManager. */
class UpdateCheckWorker(
    appContext: Context,
    params: WorkerParameters,
) : CoroutineWorker(appContext, params) {
    override suspend fun doWork(): Result {
        val config = UpdateConfig.load(applicationContext)
        if (!config.enabled) {
            return Result.success()
        }
        val retry = UpdateManager.checkInBackground(applicationContext)
        return if (retry && runAttemptCount < MAX_RETRIES) Result.retry() else Result.success()
    }

    companion object {
        /** Name of the single periodic work of the app. */
        const val UNIQUE_NAME = "unison.update_check"

        /** Number of retries after a transient network failure. */
        private const val MAX_RETRIES = 3

        /** Starts or stops the periodic check to match [config]. */
        fun syncSchedule(
            context: Context,
            config: UpdateConfig,
        ) {
            val work = WorkManager.getInstance(context)
            if (!config.enabled) {
                work.cancelUniqueWork(UNIQUE_NAME)
                return
            }
            val request =
                PeriodicWorkRequestBuilder<UpdateCheckWorker>(config.interval.hours, TimeUnit.HOURS)
                    .setConstraints(
                        Constraints
                            .Builder()
                            .setRequiredNetworkType(NetworkType.CONNECTED)
                            .build(),
                    ).setBackoffCriteria(BackoffPolicy.EXPONENTIAL, MIN_BACKOFF, TimeUnit.MINUTES)
                    .build()
            work.enqueueUniquePeriodicWork(UNIQUE_NAME, ExistingPeriodicWorkPolicy.UPDATE, request)
        }

        /** First delay of a retry after a failed check. */
        private const val MIN_BACKOFF = 30L
    }
}
