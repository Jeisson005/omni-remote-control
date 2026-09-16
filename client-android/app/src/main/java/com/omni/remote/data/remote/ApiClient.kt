package com.omni.remote.data.remote

import android.content.Context
import android.util.Log
import com.google.gson.Gson
import com.omni.remote.data.local.OmniStore
import com.omni.remote.data.models.NotificationRecord
import com.omni.remote.data.models.SmsMessage
import com.omni.remote.data.prefs.PreferencesManager
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/**
 * Cliente HTTP para el reenvío directo (best-effort e inmediato) de
 * notificaciones y SMS al servidor Omni, incluso sin sesión WebSocket.
 *
 * Si el envío falla, el elemento permanece en [OmniStore] y se reintenta al
 * abrir una sesión de control o al ejecutarse el worker de telemetría.
 */
class ApiClient private constructor(context: Context) {

    private val appContext = context.applicationContext
    private val prefs = PreferencesManager(appContext)
    private val store = OmniStore.getInstance(appContext)
    private val gson = Gson()
    private val executor = Executors.newSingleThreadExecutor()

    private val httpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(10, TimeUnit.SECONDS)
        .build()

    fun enqueueNotification(record: NotificationRecord) {
        store.addNotification(record)
        executor.execute {
            if (postNotification(record)) {
                store.removeNotifications(listOf(record.externalId))
            }
        }
    }

    fun enqueueSms(message: SmsMessage) {
        store.addSms(message)
        executor.execute {
            if (postSms(message)) {
                store.removeSms(listOf(message.externalId))
            }
        }
    }

    fun flushPending() {
        executor.execute { flushBlocking() }
    }

    /**
     * Registra el token FCM del dispositivo por HTTP, de modo que el servidor
     * pueda despertarlo por push incluso si nunca ha abierto una sesión WS
     * (necesario para el modo consentimiento).
     */
    fun registerFcmToken(token: String) {
        if (token.isBlank()) return
        executor.execute {
            val json = gson.toJson(mapOf("token" to token))
            post("${prefs.serverUrl}/api/v1/devices/${prefs.deviceId}/fcm-token", json, "fcm_token")
        }
    }

    /**
     * Reenvía todos los elementos pendientes. Devuelve true si todos fueron
     * entregados correctamente.
     */
    fun flushBlocking(): Boolean {
        var allDelivered = true

        for (record in store.notifications()) {
            if (postNotification(record)) {
                store.removeNotifications(listOf(record.externalId))
            } else {
                allDelivered = false
            }
        }

        for (message in store.sms()) {
            if (postSms(message)) {
                store.removeSms(listOf(message.externalId))
            } else {
                allDelivered = false
            }
        }

        return allDelivered
    }

    private fun postNotification(record: NotificationRecord): Boolean {
        return post("${prefs.serverUrl}/api/v1/notifications", gson.toJson(record), "notification")
    }

    private fun postSms(message: SmsMessage): Boolean {
        return post("${prefs.serverUrl}/api/v1/sms", gson.toJson(message), "sms")
    }

    private fun post(url: String, json: String, kind: String): Boolean {
        return try {
            val body = json.toRequestBody("application/json; charset=utf-8".toMediaType())
            val request = Request.Builder().url(url).post(body).build()
            httpClient.newCall(request).execute().use { response ->
                if (response.isSuccessful) {
                    Log.d(TAG, "Delivered $kind to server (HTTP ${response.code})")
                    true
                } else {
                    Log.w(TAG, "Failed to deliver $kind: HTTP ${response.code}")
                    false
                }
            }
        } catch (e: Exception) {
            Log.w(TAG, "Error delivering $kind: ${e.message}")
            false
        }
    }

    companion object {
        private const val TAG = "OmniApiClient"

        @Volatile
        private var instance: ApiClient? = null

        fun getInstance(context: Context): ApiClient {
            return instance ?: synchronized(this) {
                instance ?: ApiClient(context.applicationContext).also { instance = it }
            }
        }
    }
}
