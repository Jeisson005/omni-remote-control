package com.omni.remote.data.prefs

import android.content.Context
import android.content.SharedPreferences
import android.os.Build
import android.provider.Settings
import java.security.MessageDigest
import java.util.UUID

class PreferencesManager(private val context: Context) {

    private val prefs: SharedPreferences = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    var deviceId: String
        get() {
            var id = prefs.getString(KEY_DEVICE_ID, null)
            if (id.isNullOrEmpty()) {
                id = generateStableDeviceId()
                prefs.edit().putString(KEY_DEVICE_ID, id).apply()
            }
            return id
        }
        set(value) {
            prefs.edit().putString(KEY_DEVICE_ID, value).apply()
        }

    var serverUrl: String
        get() = prefs.getString(KEY_SERVER_URL, DEFAULT_SERVER_URL) ?: DEFAULT_SERVER_URL
        set(value) {
            prefs.edit().putString(KEY_SERVER_URL, value.trim().trimEnd('/')).apply()
        }

    val serverWsUrl: String
        get() {
            val base = serverUrl
            val wsBase = if (base.startsWith("https://")) {
                base.replaceFirst("https://", "wss://")
            } else {
                base.replaceFirst("http://", "ws://")
            }
            return "$wsBase/ws/devices"
        }

    var deviceName: String
        get() = prefs.getString(KEY_DEVICE_NAME, "${Build.MANUFACTURER} ${Build.MODEL}") ?: "Android Device"
        set(value) {
            prefs.edit().putString(KEY_DEVICE_NAME, value).apply()
        }

    var fcmToken: String?
        get() = prefs.getString(KEY_FCM_TOKEN, null)
        set(value) {
            prefs.edit().putString(KEY_FCM_TOKEN, value).apply()
        }

    /**
     * Modo de control:
     *  - [CONTROL_MODE_CONSENT]: el usuario debe aceptar cada toma de control desde una notificación.
     *  - [CONTROL_MODE_AUTO]: control totalmente desatendido.
     * Por defecto se usa consentimiento.
     */
    var controlMode: String
        get() = prefs.getString(KEY_CONTROL_MODE, CONTROL_MODE_CONSENT) ?: CONTROL_MODE_CONSENT
        set(value) {
            val normalized = if (value == CONTROL_MODE_AUTO) CONTROL_MODE_AUTO else CONTROL_MODE_CONSENT
            prefs.edit().putString(KEY_CONTROL_MODE, normalized).apply()
        }

    /** Si false, el servidor no puede cambiar el modo remota mente. */
    var allowRemoteModeChange: Boolean
        get() = prefs.getBoolean(KEY_ALLOW_REMOTE_MODE_CHANGE, false)
        set(value) {
            prefs.edit().putBoolean(KEY_ALLOW_REMOTE_MODE_CHANGE, value).apply()
        }

    /**
     * Estrategia de desbloqueo:
     *  - [UNLOCK_STRATEGY_KEYGUARD_DISABLE]: deshabilitar el keyguard (Device Owner/Shizuku).
     *  - [UNLOCK_STRATEGY_PATTERN]: reproducir el patrón guardado localmente.
     *  - [UNLOCK_STRATEGY_NONE]: solo despertar / descartar keyguard no seguro.
     */
    var unlockStrategy: String
        get() = prefs.getString(KEY_UNLOCK_STRATEGY, UNLOCK_STRATEGY_NONE) ?: UNLOCK_STRATEGY_NONE
        set(value) {
            prefs.edit().putString(KEY_UNLOCK_STRATEGY, value).apply()
        }

    /** Calibración del tablero de patrón: fracción vertical del centro y escala del tablero. */
    var patternCenterY: Float
        get() = prefs.getFloat(KEY_PATTERN_CENTER_Y, 0.48f)
        set(value) {
            prefs.edit().putFloat(KEY_PATTERN_CENTER_Y, value).apply()
        }

    var patternScale: Float
        get() = prefs.getFloat(KEY_PATTERN_SCALE, 0.72f)
        set(value) {
            prefs.edit().putFloat(KEY_PATTERN_SCALE, value).apply()
        }

    private fun generateStableDeviceId(): String {
        return try {
            val androidId = Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID)
            val combined = "${Build.BOARD}-${Build.BRAND}-${Build.DEVICE}-${androidId}"
            val md5 = MessageDigest.getInstance("MD5").digest(combined.toByteArray())
            val hex = md5.joinToString("") { "%02x".format(it) }
            "android-$hex"
        } catch (e: Exception) {
            "android-${UUID.randomUUID().toString().replace("-", "").take(16)}"
        }
    }

    companion object {
        private const val PREFS_NAME = "omni_remote_prefs"
        private const val KEY_DEVICE_ID = "key_device_id"
        private const val KEY_SERVER_URL = "key_server_url"
        private const val KEY_DEVICE_NAME = "key_device_name"
        private const val KEY_FCM_TOKEN = "key_fcm_token"
        private const val KEY_CONTROL_MODE = "key_control_mode"
        private const val KEY_ALLOW_REMOTE_MODE_CHANGE = "key_allow_remote_mode_change"
        private const val KEY_UNLOCK_STRATEGY = "key_unlock_strategy"
        private const val KEY_PATTERN_CENTER_Y = "key_pattern_center_y"
        private const val KEY_PATTERN_SCALE = "key_pattern_scale"

        const val CONTROL_MODE_CONSENT = "consent"
        const val CONTROL_MODE_AUTO = "auto"

        const val UNLOCK_STRATEGY_NONE = "none"
        const val UNLOCK_STRATEGY_KEYGUARD_DISABLE = "keyguard_disable"
        const val UNLOCK_STRATEGY_PATTERN = "pattern"

        // 10.0.2.2 es el host loopback en el Emulador de Android oficial
        const val DEFAULT_SERVER_URL = "http://10.0.2.2:8090"
    }
}
