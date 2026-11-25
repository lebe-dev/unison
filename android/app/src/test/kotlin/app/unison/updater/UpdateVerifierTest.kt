package app.unison.updater

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.ByteArrayInputStream
import java.io.File
import java.io.InputStream
import java.security.KeyFactory
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.MessageDigest
import java.security.Signature
import java.security.spec.X509EncodedKeySpec
import java.util.Base64
import kotlin.coroutines.cancellation.CancellationException

/** Tests of the download and of the signature and checksum checks. */
internal class UpdateVerifierTest {
    @get:Rule
    val folder = TemporaryFolder()

    private val keys: KeyPair = KeyPairGenerator.getInstance("Ed25519").generateKeyPair()
    private val apk = "the content of the apk".toByteArray()
    private val sumUrl = "https://github.com/lebe-dev/unison/releases/download/0.3.0/SHA256SUMS"
    private val signatureUrl = "https://github.com/lebe-dev/unison/releases/download/0.3.0/SHA256SUMS.sig"
    private val apkUrl = "https://github.com/lebe-dev/unison/releases/download/0.3.0/unison-v0.3.0-android.apk"
    private val release = release()

    @Test
    fun `a signed manifest and a matching apk are accepted`() {
        val manifest = manifest(apk)
        val downloader = mapDownloader(manifest, signature(manifest), apk)
        val verifier = verifier(downloader)

        runTest { verifier.verify(release) }

        val file = verifier.verifiedFile(release)
        assertTrue(file.isFile)
        assertArrayEquals(apk, file.readBytes())
        assertFalse("the part file stays after the download", File(file.parentFile, "${file.name}.part").exists())
    }

    @Test
    fun `a bad signature stops the download`() {
        val manifest = manifest(apk)
        val downloader = mapDownloader(manifest, signature(manifest(apk + "!".toByteArray())), apk)

        val error = failure { verifier(downloader).verify(release) }

        assertEquals(UpdateVerificationError.SIGNATURE, error.error)
        assertFalse("the APK was read before the signature was checked", apkUrl in downloader.opened)
    }

    @Test
    fun `a tampered apk is deleted`() {
        val manifest = manifest(apk)
        val downloader = mapDownloader(manifest, signature(manifest), "another apk".toByteArray())

        val error = failure { verifier(downloader).verify(release) }

        assertEquals(UpdateVerificationError.CHECKSUM, error.error)
        val staged = File(folder.root, "updates/0.3.0")
        assertEquals(0, staged.listFiles()?.size ?: 0)
    }

    @Test
    fun `a cancellation deletes the partial file`() {
        val manifest = manifest(apk)
        val downloader = cancellingDownloader(manifest, signature(manifest))

        try {
            runTest { verifier(downloader).verify(release) }
            error("the download did not stop")
        } catch (_: CancellationException) {
            // Expected.
        }

        val staged = File(folder.root, "updates/0.3.0")
        assertEquals(0, staged.listFiles()?.size ?: 0)
    }

    @Test
    fun `an apk above the limit is refused`() {
        val manifest = manifest(apk)
        val downloader =
            object : ReleaseDownloader {
                override fun open(url: String): DownloadStream =
                    when (url) {
                        sumUrl -> stream(manifest)
                        signatureUrl -> stream(signature(manifest))
                        else -> DownloadStream(UpdateLimits.MAX_APK_BYTES + 1, ByteArrayInputStream(ByteArray(1)))
                    }
            }

        val error = failure { verifier(downloader).verify(release) }

        assertEquals(UpdateVerificationError.TOO_LARGE, error.error)
    }

    @Test
    fun `an empty key fails closed`() {
        val manifest = manifest(apk)
        val downloader = mapDownloader(manifest, signature(manifest), apk)
        val verifier = UpdateVerifier(folder.newFolder("updates"), JcaVerifier(), downloader, publicKey = "")

        val error = failure { verifier.verify(release) }

        assertEquals(UpdateVerificationError.INVALID_KEY, error.error)
        assertTrue(downloader.opened.isEmpty())
    }

    @Test
    fun `a manifest without the apk fails`() {
        val manifest = "00".repeat(32).toByteArray() + "  other.apk\n".toByteArray()
        val downloader = mapDownloader(manifest, signature(manifest), apk)

        val error = failure { verifier(downloader).verify(release) }

        assertEquals(UpdateVerificationError.CHECKSUMS, error.error)
        assertFalse(apkUrl in downloader.opened)
    }

    @Test
    fun `an already verified apk is not downloaded again`() {
        val manifest = manifest(apk)
        val downloader = mapDownloader(manifest, signature(manifest), apk)
        val verifier = verifier(downloader)

        runTest {
            verifier.verify(release)
            verifier.verify(release)
        }

        assertEquals(1, downloader.opened.count { it == apkUrl })
    }

    /** The update of a test. */
    private fun release(): ReleaseInfo =
        ReleaseInfo(
            version = "0.3.0",
            tagName = "0.3.0",
            name = "Unison 0.3.0",
            body = "Notes",
            publishedAt = "2026-11-24T10:20:30Z",
            prerelease = false,
            htmlUrl = "https://github.com/lebe-dev/unison/releases/tag/0.3.0",
            assetName = "unison-v0.3.0-android.apk",
            assetUrl = apkUrl,
            assetSize = apk.size.toLong(),
            checksumsUrl = sumUrl,
            signatureUrl = signatureUrl,
        )

    /** The verifier of a test with the public key of the test keypair. */
    private fun verifier(downloader: ReleaseDownloader): UpdateVerifier =
        UpdateVerifier(
            workDir = folder.newFolder("updates"),
            verifier = JcaVerifier(),
            downloader = downloader,
            publicKey =
                Base64.getEncoder().encodeToString(
                    keys.public.encoded
                        .takeLast(32)
                        .toByteArray(),
                ),
        )

    /** A downloader that serves the files of a test. */
    private fun mapDownloader(
        manifest: ByteArray,
        signature: ByteArray,
        artifact: ByteArray,
    ): MapDownloader =
        MapDownloader(
            mapOf(
                sumUrl to manifest,
                signatureUrl to signature,
                apkUrl to artifact,
            ),
        )

    /** A downloader whose artifact stops with a cancellation. */
    private fun cancellingDownloader(
        manifest: ByteArray,
        signature: ByteArray,
    ): ReleaseDownloader =
        object : ReleaseDownloader {
            override fun open(url: String): DownloadStream =
                when (url) {
                    sumUrl -> stream(manifest)
                    signatureUrl -> stream(signature)
                    else ->
                        DownloadStream(
                            apk.size.toLong(),
                            object : InputStream() {
                                override fun read(): Int = throw CancellationException("the download stopped")
                            },
                        )
                }
        }

    /** The manifest of [artifact]. */
    private fun manifest(artifact: ByteArray): ByteArray {
        val digest =
            MessageDigest
                .getInstance("SHA-256")
                .digest(artifact)
                .joinToString("") { byte -> "%02x".format(byte) }
        return "$digest  ${release.assetName}\n".toByteArray()
    }

    /** The signature file of [manifest]. */
    private fun signature(manifest: ByteArray): ByteArray {
        val signature = Signature.getInstance("Ed25519")
        signature.initSign(keys.private)
        signature.update(SIGNATURE_HEAD)
        signature.update(manifest)
        return Base64.getEncoder().encodeToString(signature.sign()).toByteArray()
    }

    private fun stream(bytes: ByteArray): DownloadStream = DownloadStream(bytes.size.toLong(), ByteArrayInputStream(bytes))

    /** The failure of a verification. */
    private fun failure(block: suspend () -> Unit): UpdateVerificationException =
        try {
            runTest { block() }
            error("the verification did not fail")
        } catch (e: UpdateVerificationException) {
            e
        }

    /** A downloader over a map of files. */
    private class MapDownloader(
        private val files: Map<String, ByteArray>,
    ) : ReleaseDownloader {
        val opened = mutableListOf<String>()

        override fun open(url: String): DownloadStream {
            opened += url
            val bytes = files[url] ?: throw UpdateCheckException(UpdateCheckCode.NETWORK, "no file $url")
            return DownloadStream(bytes.size.toLong(), ByteArrayInputStream(bytes))
        }
    }

    /** A verifier over the Ed25519 implementation of the JDK. */
    private class JcaVerifier : ReleaseSignatureVerifier {
        override fun verify(
            publicKey: ByteArray,
            payload: ByteArray,
            signature: ByteArray,
        ): Boolean {
            val key =
                KeyFactory
                    .getInstance("Ed25519")
                    .generatePublic(X509EncodedKeySpec(X509_PREFIX + publicKey))
            val verifier = Signature.getInstance("Ed25519")
            verifier.initVerify(key)
            verifier.update(SIGNATURE_HEAD)
            verifier.update(payload)
            return verifier.verify(signature)
        }
    }

    private companion object {
        val SIGNATURE_HEAD = "unison/1 release\u0000".toByteArray()

        /** DER prefix of a raw Ed25519 public key. */
        val X509_PREFIX =
            byteArrayOf(
                0x30,
                0x2a,
                0x30,
                0x05,
                0x06,
                0x03,
                0x2b,
                0x65,
                0x70,
                0x03,
                0x21,
                0x00,
            )
    }
}
