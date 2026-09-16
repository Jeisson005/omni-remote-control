package com.omni.remote.data.security

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/**
 * Almacenamiento cifrado con Android Keystore para secretos del dispositivo.
 *
 * El patrón de desbloqueo NUNCA sale del dispositivo: no se envía al servidor,
 * no se registra en logs y no se respalda. Si el Keystore no está disponible,
 * las operaciones se comportan como no-op.
 */
object SecurePrefs {

    private const val TAG = "SecurePrefs"
    private const val FILE_NAME = "omni_secure_prefs"
    private const val KEY_UNLOCK_PATTERN = "unlock_pattern"

    @Volatile
    private var prefs: SharedPreferences? = null

    @Volatile
    private var initFailed = false

    private fun get(context: Context): SharedPreferences? {
        prefs?.let { return it }
        if (initFailed) return null

        synchronized(this) {
            prefs?.let { return it }
            return try {
                val masterKey = MasterKey.Builder(context.applicationContext)
                    .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                    .build()

                val encrypted = EncryptedSharedPreferences.create(
                    context.applicationContext,
                    FILE_NAME,
                    masterKey,
                    EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                    EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
                )
                prefs = encrypted
                encrypted
            } catch (e: Exception) {
                Log.e(TAG, "EncryptedSharedPreferences unavailable: ${e.message}")
                initFailed = true
                null
            }
        }
    }

    fun isAvailable(context: Context): Boolean = get(context) != null

    fun setUnlockPattern(context: Context, pattern: String) {
        get(context)?.edit()?.putString(KEY_UNLOCK_PATTERN, pattern)?.apply()
    }

    fun getUnlockPattern(context: Context): String? =
        get(context)?.getString(KEY_UNLOCK_PATTERN, null)

    fun clearUnlockPattern(context: Context) {
        get(context)?.edit()?.remove(KEY_UNLOCK_PATTERN)?.apply()
    }
}
