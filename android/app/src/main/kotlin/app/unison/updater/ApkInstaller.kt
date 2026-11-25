@file:Suppress("NestedBlockDepth", "TooGenericExceptionCaught")

package app.unison.updater

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInfo
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.provider.Settings
import java.io.File
import java.security.MessageDigest

/** Why Android would refuse a verified APK before the installation starts. */
enum class ApkInstallProblem {
    ARCHIVE,
    PACKAGE,
    OLDER,
    SIGNATURE,
}

/**
 * Checks a verified APK against the installed app and hands it to the session
 * of the system package installer.
 */
object ApkInstaller {
    /** True when Android lets this app ask the user to install an APK. */
    fun canInstall(context: Context): Boolean = context.packageManager.canRequestPackageInstalls()

    /** The system setting that allows installs from this app. */
    fun unknownSourcesIntent(context: Context): Intent =
        Intent(
            Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
            Uri.parse("package:${context.packageName}"),
        )

    /** The reason Android would refuse [apkFile], null when it would accept it. */
    fun check(
        context: Context,
        apkFile: File,
    ): ApkInstallProblem? {
        val archive =
            context.packageManager.getPackageArchiveInfo(apkFile.absolutePath, PackageManager.GET_SIGNING_CERTIFICATES)
                ?: return ApkInstallProblem.ARCHIVE
        if (archive.packageName != context.packageName) {
            return ApkInstallProblem.PACKAGE
        }
        val installed =
            runCatching {
                context.packageManager.getPackageInfo(context.packageName, PackageManager.GET_SIGNING_CERTIFICATES)
            }.getOrNull() ?: return ApkInstallProblem.PACKAGE
        if (archive.longVersionCode < installed.longVersionCode) {
            return ApkInstallProblem.OLDER
        }
        if (!sameSigner(archive, installed)) {
            return ApkInstallProblem.SIGNATURE
        }
        return null
    }

    /** Starts a full install session of [apkFile]. */
    fun install(
        context: Context,
        apkFile: File,
    ) {
        val installer = context.packageManager.packageInstaller
        val params =
            PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
                setAppPackageName(context.packageName)
                setSize(apkFile.length())
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                    setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED)
                }
            }
        val sessionId = installer.createSession(params)
        try {
            val session = installer.openSession(sessionId)
            try {
                writeSession(session, apkFile)
                session.commit(confirmation(context).intentSender)
            } finally {
                session.close()
            }
        } catch (e: Exception) {
            installer.abandonSession(sessionId)
            throw e
        }
    }

    /** Copies the APK into the stream of the session. */
    private fun writeSession(
        session: PackageInstaller.Session,
        apkFile: File,
    ) {
        apkFile.inputStream().use { input ->
            session.openWrite(SESSION_FILE, 0, apkFile.length()).use { output ->
                input.copyTo(output)
                session.fsync(output)
            }
        }
    }

    /** The broadcast that receives the result of the install session. */
    private fun confirmation(context: Context): PendingIntent {
        val flags =
            PendingIntent.FLAG_UPDATE_CURRENT or
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE else 0
        val intent = Intent(context, UpdateInstallReceiver::class.java)
        return PendingIntent.getBroadcast(context, REQUEST_CODE, intent, flags)
    }

    /** True when the archive is signed with the certificates of the installed app. */
    private fun sameSigner(
        archive: PackageInfo,
        installed: PackageInfo,
    ): Boolean {
        val archiveDigests = digests(archive)
        val installedDigests = digests(installed)
        return archiveDigests.isNotEmpty() && archiveDigests.all { it in installedDigests }
    }

    /** SHA-256 of every signing certificate of a package. */
    private fun digests(info: PackageInfo): Set<String> {
        val signing = info.signingInfo ?: return emptySet()
        val certificates =
            if (signing.hasMultipleSigners()) {
                signing.apkContentsSigners
            } else {
                signing.signingCertificateHistory
            }
        return certificates.mapTo(mutableSetOf()) { signature ->
            MessageDigest
                .getInstance("SHA-256")
                .digest(signature.toByteArray())
                .joinToString("") { byte -> "%02x".format(byte) }
        }
    }

    /** Name of the APK inside the session. */
    private const val SESSION_FILE = "unison-update.apk"

    /** Request code of the result broadcast. */
    private const val REQUEST_CODE = 41
}
