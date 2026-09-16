package com.omni.remote.data.local

import android.content.Context
import android.util.Log
import com.google.gson.Gson
import com.omni.remote.data.models.NotificationRecord
import com.omni.remote.data.models.SmsMessage
import java.io.File

/**
 * Cola local persistente de notificaciones y SMS capturados por el agente.
 *
 * Se usa como respaldo cuando no hay sesión WebSocket activa (Android en Doze)
 * o cuando falla el reenvío HTTP inmediato. Los elementos pendientes se vacían
 * al abrir una sesión de control o al ejecutarse el worker de telemetría.
 */
class OmniStore private constructor(context: Context) {

    private val gson = Gson()
    private val file = File(context.applicationContext.filesDir, FILE_NAME)

    private data class PendingData(
        val notifications: MutableList<NotificationRecord> = mutableListOf(),
        val sms: MutableList<SmsMessage> = mutableListOf()
    )

    @Synchronized
    fun addNotification(record: NotificationRecord) {
        val data = read()
        data.notifications.add(0, record)
        if (data.notifications.size > MAX_ITEMS) {
            data.notifications.subList(MAX_ITEMS, data.notifications.size).clear()
        }
        write(data)
    }

    @Synchronized
    fun addSms(message: SmsMessage) {
        val data = read()
        data.sms.add(0, message)
        if (data.sms.size > MAX_ITEMS) {
            data.sms.subList(MAX_ITEMS, data.sms.size).clear()
        }
        write(data)
    }

    @Synchronized
    fun notifications(): List<NotificationRecord> = read().notifications.toList()

    @Synchronized
    fun sms(): List<SmsMessage> = read().sms.toList()

    @Synchronized
    fun removeNotifications(externalIds: Collection<String>) {
        if (externalIds.isEmpty()) return
        val data = read()
        data.notifications.removeAll { it.externalId in externalIds }
        write(data)
    }

    @Synchronized
    fun removeSms(externalIds: Collection<String>) {
        if (externalIds.isEmpty()) return
        val data = read()
        data.sms.removeAll { it.externalId in externalIds }
        write(data)
    }

    @Synchronized
    fun pendingCount(): Int {
        val data = read()
        return data.notifications.size + data.sms.size
    }

    private fun read(): PendingData {
        return try {
            if (!file.exists()) return PendingData()
            val text = file.readText()
            if (text.isBlank()) PendingData() else gson.fromJson(text, PendingData::class.java)
        } catch (e: Exception) {
            Log.w(TAG, "Could not read pending store, starting fresh: ${e.message}")
            PendingData()
        }
    }

    private fun write(data: PendingData) {
        try {
            file.writeText(gson.toJson(data))
        } catch (e: Exception) {
            Log.e(TAG, "Could not persist pending store: ${e.message}")
        }
    }

    companion object {
        private const val TAG = "OmniStore"
        private const val FILE_NAME = "omni_pending_sync.json"
        private const val MAX_ITEMS = 500

        @Volatile
        private var instance: OmniStore? = null

        fun getInstance(context: Context): OmniStore {
            return instance ?: synchronized(this) {
                instance ?: OmniStore(context.applicationContext).also { instance = it }
            }
        }
    }
}
