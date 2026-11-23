package app.unison.bridge

import android.content.Context
import android.content.pm.PackageManager
import android.os.Process
import android.util.Log

/**
 * Checks that a binder caller is this app or an app signed with its key.
 *
 * The signature permission already keeps foreign apps away; this check does not
 * rely on the permission alone.
 */
class CallerVerifier(
    private val manager: PackageManager,
    private val ownPackage: String,
) {
    private val ownCertificates: List<ByteArray> by lazy(::loadOwnCertificates)

    /** Throws SecurityException unless the caller is signed with the key of this app. */
    fun verify(uid: Int) {
        if (isSignedLikeApp(uid)) {
            return
        }
        Log.w(TAG, "refused a caller with uid $uid")
        throw SecurityException("caller is not signed with the key of $ownPackage")
    }

    /** True when a package of the uid carries a certificate of this app. */
    fun isSignedLikeApp(uid: Int): Boolean {
        if (uid == Process.myUid()) {
            return true
        }
        val packages = manager.getPackagesForUid(uid) ?: return false
        for (pkg in packages) {
            for (certificate in ownCertificates) {
                val signed =
                    manager.hasSigningCertificate(
                        pkg,
                        certificate,
                        PackageManager.CERT_INPUT_RAW_X509,
                    )
                if (signed) {
                    return true
                }
            }
        }
        return false
    }

    /** Certificates of this app, empty when they cannot be read. */
    private fun loadOwnCertificates(): List<ByteArray> =
        try {
            val info = manager.getPackageInfo(ownPackage, PackageManager.GET_SIGNING_CERTIFICATES)
            info.signingInfo
                ?.apkContentsSigners
                ?.map { it.toByteArray() }
                .orEmpty()
        } catch (e: PackageManager.NameNotFoundException) {
            Log.w(TAG, "cannot read the signing key: ${e.message}")
            emptyList()
        }

    companion object {
        private const val TAG = "CallerVerifier"

        /** Verifier for the package of the context. */
        fun of(context: Context): CallerVerifier = CallerVerifier(context.packageManager, context.packageName)
    }
}
