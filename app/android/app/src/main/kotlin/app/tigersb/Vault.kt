package app.tigersb

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import android.util.Log
import java.security.KeyStore
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Keeps the key that encrypts the saved subscription. The key itself lies in
 * the app's preferences wrapped by an AES key that never leaves the Android
 * Keystore, so a copy of the app's files does not open the subscription.
 */
object Vault {
    private const val TAG = "TigerSB"
    private const val ALIAS = "tigersb-state"
    private const val PREF = "wrappedStateKey"

    /** The 32-byte state key, made on first use; null when the Keystore fails. */
    fun stateKey(context: Context): ByteArray? = try {
        val prefs = context.applicationContext.getSharedPreferences("vault", Context.MODE_PRIVATE)
        val wrapped = prefs.getString(PREF, null)?.let { Base64.decode(it, Base64.NO_WRAP) }
        wrapped?.let { unwrap(it) } ?: ByteArray(32).also {
            // First run, or the Keystore lost its key: the old state cannot be
            // read any more and the user adds the key again.
            SecureRandom().nextBytes(it)
            prefs.edit().putString(PREF, Base64.encodeToString(wrap(it), Base64.NO_WRAP)).apply()
        }
    } catch (e: Exception) {
        Log.e(TAG, "keystore", e)
        null
    }

    private fun keystoreKey(): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getKey(ALIAS, null) as? SecretKey)?.let { return it }
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        gen.init(
            KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return gen.generateKey()
    }

    private fun wrap(plain: ByteArray): ByteArray {
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.ENCRYPT_MODE, keystoreKey())
        return c.iv + c.doFinal(plain)
    }

    private fun unwrap(b: ByteArray): ByteArray? = try {
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.DECRYPT_MODE, keystoreKey(), GCMParameterSpec(128, b, 0, 12))
        c.doFinal(b, 12, b.size - 12)
    } catch (e: Exception) {
        Log.w(TAG, "state key unreadable", e)
        null
    }
}
