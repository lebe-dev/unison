@file:Suppress("TooGenericExceptionCaught")

package app.unison.updater

import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import androidx.annotation.StringRes
import app.unison.BuildConfig
import app.unison.R
import app.unison.core.mobile.Mobile
import app.unison.diagnostics.Journal
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import java.io.File

/** What the app shows about the current update. */
sealed interface UpdateStatus {
    data object Idle : UpdateStatus

    data object Checking : UpdateStatus

    data object UpToDate : UpdateStatus

    data object Available : UpdateStatus

    data class Downloading(
        val downloaded: Long,
        val total: Long,
    ) : UpdateStatus

    data object Verifying : UpdateStatus

    /** Android needs the user to allow installs from this app. */
    data object PermissionRequired : UpdateStatus

    data object Installing : UpdateStatus

    data class Failed(
        @StringRes val message: Int,
    ) : UpdateStatus
}

/** The whole update state of the process. */
data class UpdateUiState(
    val config: UpdateConfig = UpdateConfig.DEFAULT,
    val currentVersion: String = "",
    val status: UpdateStatus = UpdateStatus.Idle,
    val available: ReleaseInfo? = null,
    val dialogOpen: Boolean = false,
) {
    /** True during a download or an installation. */
    val busy: Boolean
        get() = status is UpdateStatus.Downloading || status == UpdateStatus.Verifying || status == UpdateStatus.Installing

    /** True when an update waits for the user and was not put away. */
    val showBanner: Boolean
        get() {
            if (busy) {
                return true
            }
            val release = available ?: return false
            return release.version != config.dismissedVersion
        }
}

/**
 * The single update controller of the process. It checks GitHub, downloads and
 * verifies an APK, and hands it to the system installer while the screens read
 * one [StateFlow] of the result.
 */
object UpdateManager {
    private const val CACHE_DIR = "updates"
    private const val DAY_MS = 24L * 60 * 60 * 1000
    private const val HOUR_MS = 60L * 60 * 1000

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val client = GitHubReleasesClient()

    private val _state =
        MutableStateFlow(
            UpdateUiState(currentVersion = BuildConfig.VERSION_NAME),
        )
    val state: StateFlow<UpdateUiState> = _state.asStateFlow()

    private val _pendingInstallIntent = MutableSharedFlow<Intent>(extraBufferCapacity = 1)
    val pendingInstallIntent: SharedFlow<Intent> = _pendingInstallIntent.asSharedFlow()

    private var checkJob: Job? = null
    private var installJob: Job? = null
    private var verifiedApk: File? = null

    /** Loads the stored settings and the release of the last background check. */
    fun init(context: Context) {
        val config = UpdateConfig.load(context)
        val available = config.cachedUpdate(BuildConfig.VERSION_NAME)
        _state.update {
            it.copy(
                config = config,
                available = available,
                status = if (available != null) UpdateStatus.Available else UpdateStatus.Idle,
            )
        }
        purgeStaleDownloads(context)
        UpdateCheckWorker.syncSchedule(context, config)
    }

    /** Turns the background checks on or off. */
    fun setEnabled(
        context: Context,
        enabled: Boolean,
    ) = applyConfig(context, _state.value.config.copy(enabled = enabled))

    /** Includes or excludes the pre-releases of GitHub. */
    fun setBetaChannel(
        context: Context,
        betaChannel: Boolean,
    ) = applyConfig(context, _state.value.config.withBetaChannel(betaChannel))

    /** Changes how often the background check runs. */
    fun setInterval(
        context: Context,
        interval: UpdateCheckInterval,
    ) = applyConfig(context, _state.value.config.copy(interval = interval))

    /** Stores [config] and brings the background work in line with it. */
    fun applyConfig(
        context: Context,
        config: UpdateConfig,
    ) {
        UpdateConfig.save(context, config)
        val available = config.cachedUpdate(BuildConfig.VERSION_NAME)
        if (available?.version != _state.value.available?.version) {
            verifiedApk = null
        }
        _state.update {
            it.copy(
                config = config,
                available = available,
                status = if (it.busy) it.status else statusOf(available),
            )
        }
        UpdateCheckWorker.syncSchedule(context, config)
    }

    /** Opens the dialog from a banner or a settings row. */
    fun openDialog() {
        _state.update { it.copy(dialogOpen = true) }
    }

    /** Closes the dialog unless an installation is already under way. */
    fun closeDialog() {
        if (_state.value.status == UpdateStatus.Verifying || _state.value.status == UpdateStatus.Installing) {
            return
        }
        _state.update { it.copy(dialogOpen = false) }
    }

    /** Puts the offered version away until a newer one appears. */
    fun dismiss(context: Context) {
        val release = _state.value.available ?: return
        val config = _state.value.config.copy(dismissedVersion = release.version)
        UpdateConfig.save(context, config)
        _state.update { it.copy(config = config) }
    }

    /** Runs a check the user asked for, even when the background checks are off. */
    fun checkNow(context: Context) {
        startCheck(context.applicationContext, manual = true)
    }

    /** Runs a check when the interval has passed since the last one. */
    fun checkIfDue(context: Context) {
        val config = _state.value.config
        if (!config.enabled || checkJob?.isActive == true) {
            return
        }
        val elapsed = System.currentTimeMillis() - config.lastCheckedMs
        if (config.lastCheckedMs == 0L || elapsed >= config.interval.hours * HOUR_MS) {
            startCheck(context.applicationContext, manual = false)
        }
    }

    /** Runs a background check; true when WorkManager should try again later. */
    suspend fun checkInBackground(context: Context): Boolean {
        val config = UpdateConfig.load(context)
        if (!config.enabled) {
            return false
        }
        return try {
            val outcome = client.check(config, BuildConfig.VERSION_NAME, manual = false)
            applyOutcome(context, config, outcome)
            false
        } catch (e: CancellationException) {
            throw e
        } catch (e: UpdateCheckException) {
            Journal.warn("the background update check failed", "code" to e.code, "error" to e.message)
            e.code == UpdateCheckCode.NETWORK
        } catch (e: Exception) {
            Journal.warn("the background update check failed", "error" to e.message)
            true
        }
    }

    /** Downloads, verifies and installs the offered release. */
    fun install(context: Context) {
        val app = context.applicationContext
        val release = _state.value.available ?: return
        if (installJob?.isActive == true) {
            return
        }
        _state.update { it.copy(dialogOpen = true) }
        installJob =
            scope.launch {
                try {
                    val apk =
                        verifiedApk?.takeIf { it.isFile } ?: download(app, release).also { verifiedApk = it }
                    if (!ApkInstaller.canInstall(app)) {
                        _state.update { it.copy(status = UpdateStatus.PermissionRequired) }
                        return@launch
                    }
                    startInstaller(app, apk)
                } catch (e: CancellationException) {
                    _state.update { it.copy(status = statusOf(it.available)) }
                    throw e
                } catch (e: UpdateVerificationException) {
                    Journal.warn("the update download failed", "error" to e.error, "message" to e.message)
                    _state.update { it.copy(status = UpdateStatus.Failed(messageOf(e.error))) }
                } catch (e: Exception) {
                    Journal.warn("the update failed", "error" to e.message)
                    _state.update { it.copy(status = UpdateStatus.Failed(R.string.updates_error_generic)) }
                }
            }
    }

    /** Stops a download the user no longer wants. */
    fun cancel() {
        val status = _state.value.status
        if (status !is UpdateStatus.Downloading && status != UpdateStatus.Verifying) {
            return
        }
        installJob?.cancel()
        installJob = null
        verifiedApk = null
        _state.update { it.copy(status = statusOf(it.available)) }
    }

    /** Continues the installation after the user granted the permission. */
    fun onInstallPermissionResult(context: Context) {
        if (_state.value.status != UpdateStatus.PermissionRequired || installJob?.isActive == true) {
            return
        }
        val apk = verifiedApk?.takeIf { it.isFile } ?: return
        val app = context.applicationContext
        if (!ApkInstaller.canInstall(app)) {
            return
        }
        installJob = scope.launch { startInstaller(app, apk) }
    }

    /** Handles the result the system installer reports to the receiver. */
    fun onInstallStatus(
        context: Context,
        status: Int,
        message: String?,
        confirmation: Intent?,
    ) {
        when (status) {
            PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                if (confirmation != null) {
                    _pendingInstallIntent.tryEmit(confirmation)
                }
            }
            PackageInstaller.STATUS_SUCCESS -> {
                verifiedApk?.delete()
                verifiedApk = null
                val config = _state.value.config.copy(cachedRelease = null, dismissedVersion = null)
                UpdateConfig.save(context, config)
                _state.update { it.copy(config = config, available = null, status = UpdateStatus.UpToDate, dialogOpen = false) }
                purgeStaleDownloads(context)
            }
            PackageInstaller.STATUS_FAILURE_ABORTED ->
                _state.update { it.copy(status = statusOf(it.available)) }
            else -> {
                Journal.warn("the system installer failed", "status" to status, "message" to message)
                _state.update { it.copy(status = UpdateStatus.Failed(R.string.updates_error_install)) }
            }
        }
    }

    /** Deletes the staged APKs of old versions and of checks older than a day. */
    fun purgeStaleDownloads(context: Context) {
        val root = File(context.cacheDir, CACHE_DIR)
        val children = root.listFiles() ?: return
        val now = System.currentTimeMillis()
        val current = SemVer.parse(BuildConfig.VERSION_NAME)
        children.forEach { child ->
            if (!child.isDirectory) {
                child.delete()
                return@forEach
            }
            val stale = now - child.lastModified() > DAY_MS
            val older = current != null && SemVer.parse(child.name)?.let { it <= current } == true
            if (stale || older) {
                child.deleteRecursively()
            }
        }
    }

    /** Downloads and verifies the APK of [release]. */
    private suspend fun download(
        context: Context,
        release: ReleaseInfo,
    ): File {
        _state.update { it.copy(status = UpdateStatus.Verifying) }
        val verifier =
            UpdateVerifier(
                workDir = File(context.cacheDir, CACHE_DIR),
                verifier = ReleaseSignatureVerifier(Mobile::verifyReleaseSignature),
                downloader = HttpsReleaseDownloader(),
            )
        return verifier.verify(release) { downloaded, total ->
            _state.update { it.copy(status = UpdateStatus.Downloading(downloaded, total)) }
        }
    }

    /** Checks the APK against the installed app and starts the system session. */
    private fun startInstaller(
        context: Context,
        apkFile: File,
    ) {
        val problem = ApkInstaller.check(context, apkFile)
        if (problem != null) {
            _state.update { it.copy(status = UpdateStatus.Failed(messageOf(problem))) }
            return
        }
        _state.update { it.copy(status = UpdateStatus.Installing) }
        try {
            ApkInstaller.install(context, apkFile)
        } catch (e: Exception) {
            Journal.warn("the install session failed", "error" to e.message)
            _state.update { it.copy(status = UpdateStatus.Failed(R.string.updates_error_install)) }
        }
    }

    /** Runs one check and stores its answer. */
    private fun startCheck(
        context: Context,
        manual: Boolean,
    ) {
        if (checkJob?.isActive == true) {
            return
        }
        checkJob =
            scope.launch {
                _state.update { it.copy(status = UpdateStatus.Checking) }
                val config = _state.value.config
                try {
                    val outcome = client.check(config, BuildConfig.VERSION_NAME, manual)
                    applyOutcome(context, config, outcome)
                } catch (e: CancellationException) {
                    throw e
                } catch (e: UpdateCheckException) {
                    Journal.warn("the update check failed", "code" to e.code, "error" to e.message)
                    _state.update { it.copy(status = UpdateStatus.Failed(messageOf(e.code))) }
                } catch (e: Exception) {
                    Journal.warn("the update check failed", "error" to e.message)
                    _state.update { it.copy(status = UpdateStatus.Failed(R.string.updates_error_generic)) }
                }
            }
    }

    /** Stores the answer of a check and offers its release. */
    private fun applyOutcome(
        context: Context,
        checkedConfig: UpdateConfig,
        outcome: CheckOutcome,
    ) {
        val current = _state.value.config
        // Drop the result if the user switched channels while the request was in flight.
        if (current.betaChannel != checkedConfig.betaChannel) {
            return
        }
        val release = outcome.release ?: current.cachedRelease
        val updated =
            current.copy(
                lastCheckedMs = System.currentTimeMillis(),
                lastEtag = outcome.etag ?: current.lastEtag,
                cachedRelease = release,
            )
        val available = updated.cachedUpdate(BuildConfig.VERSION_NAME)
        val persisted = updated.copy(cachedRelease = available)
        if (available?.version != _state.value.available?.version) {
            verifiedApk = null
        }
        UpdateConfig.save(context, persisted)
        _state.update {
            it.copy(
                config = persisted,
                available = available,
                status =
                    if (it.busy) {
                        it.status
                    } else if (available != null) {
                        UpdateStatus.Available
                    } else {
                        UpdateStatus.UpToDate
                    },
            )
        }
    }

    /** The status that follows a finished action. */
    private fun statusOf(available: ReleaseInfo?): UpdateStatus = if (available != null) UpdateStatus.Available else UpdateStatus.Idle

    /** Message of a failed check. */
    @StringRes
    private fun messageOf(code: UpdateCheckCode): Int =
        when (code) {
            UpdateCheckCode.NETWORK, UpdateCheckCode.UNTRUSTED_URL -> R.string.updates_error_network
            UpdateCheckCode.RATE_LIMITED -> R.string.updates_error_rate_limited
            UpdateCheckCode.STATUS -> R.string.updates_error_status
            UpdateCheckCode.INVALID_RESPONSE -> R.string.updates_error_response
            UpdateCheckCode.INVALID_VERSION -> R.string.updates_error_version
        }

    /** Message of a failed download. */
    @StringRes
    private fun messageOf(error: UpdateVerificationError): Int =
        when (error) {
            UpdateVerificationError.INVALID_KEY,
            UpdateVerificationError.SIGNATURE,
            -> R.string.updates_error_signature
            UpdateVerificationError.CHECKSUMS,
            UpdateVerificationError.CHECKSUM,
            -> R.string.updates_error_checksum
            UpdateVerificationError.TOO_LARGE -> R.string.updates_error_too_large
            UpdateVerificationError.DOWNLOAD -> R.string.updates_error_download
        }

    /** Message of an APK Android would refuse. */
    @StringRes
    private fun messageOf(problem: ApkInstallProblem): Int =
        when (problem) {
            ApkInstallProblem.SIGNATURE -> R.string.updates_error_signature_mismatch
            ApkInstallProblem.OLDER -> R.string.updates_error_older
            ApkInstallProblem.PACKAGE, ApkInstallProblem.ARCHIVE -> R.string.updates_error_archive
        }
}
