package app.unison.core

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Wraps secrets with an AES-GCM master key held by the Android Keystore. The
 * key never leaves the keystore; a wrapped secret is a version byte, a random
 * IV and the ciphertext.
 */
object AndroidKeystore {
    private const val PROVIDER = "AndroidKeyStore"
    private const val ALIAS = "unison.master"
    private const val TRANSFORMATION = "AES/GCM/NoPadding"
    private const val IV_BYTES = 12
    private const val TAG_BITS = 128
    private const val VERSION: Byte = 1

    /** Encrypts plain with the master key. Throws when the keystore fails. */
    @Synchronized
    fun wrap(plain: ByteArray): ByteArray {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, masterKey())
        cipher.updateAAD(byteArrayOf(VERSION))
        val ciphertext = cipher.doFinal(plain)
        return byteArrayOf(VERSION) + cipher.iv + ciphertext
    }

    /** Decrypts a blob produced by [wrap]. Throws when the keystore fails. */
    @Synchronized
    fun unwrap(blob: ByteArray): ByteArray {
        require(blob.size > 1 + IV_BYTES) { "secret blob is too short" }
        require(blob[0] == VERSION) { "unsupported secret blob" }
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(
            Cipher.DECRYPT_MODE,
            masterKey(),
            GCMParameterSpec(TAG_BITS, blob, 1, IV_BYTES),
        )
        cipher.updateAAD(byteArrayOf(VERSION))
        return cipher.doFinal(blob, 1 + IV_BYTES, blob.size - 1 - IV_BYTES)
    }

    private fun masterKey(): SecretKey {
        val store = KeyStore.getInstance(PROVIDER).apply { load(null) }
        val existing = store.getEntry(ALIAS, null) as? KeyStore.SecretKeyEntry
        if (existing != null) {
            return existing.secretKey
        }

        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, PROVIDER)
        generator.init(
            KeyGenParameterSpec.Builder(
                ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }
}
