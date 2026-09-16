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

        // 10.0.2.2 es el host loopback en el Emulador de Android oficial
        const val DEFAULT_SERVER_URL = "http://10.0.2.2:8090"
    }
}
