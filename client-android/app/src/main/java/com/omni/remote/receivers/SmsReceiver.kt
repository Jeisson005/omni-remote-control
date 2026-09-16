package com.omni.remote.receivers

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.provider.Telephony
import android.util.Log
import com.omni.remote.data.models.SmsMessage
import com.omni.remote.data.prefs.PreferencesManager
import com.omni.remote.data.remote.ApiClient
import com.omni.remote.util.TimeUtils

/**
 * Intercepta los SMS entrantes en el momento exacto en que llegan al módem
 * (permiso RECEIVE_SMS) y los reenvía al servidor Omni.
 */
class SmsReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent?) {
        if (intent?.action != Telephony.Sms.Intents.SMS_RECEIVED_ACTION) return

        try {
            val parts = Telephony.Sms.Intents.getMessagesFromIntent(intent) ?: return
            if (parts.isEmpty()) return

            val deviceId = PreferencesManager(context).deviceId
            val apiClient = ApiClient.getInstance(context)

            // Agrupa las partes de un SMS multiparte (misma dirección y fecha).
            data class GroupKey(val address: String, val timestamp: Long)

            val grouped = linkedMapOf<GroupKey, StringBuilder>()

            for (part in parts) {
                val address = part.originatingAddress ?: "unknown"
                val key = GroupKey(address, part.timestampMillis)
                grouped.getOrPut(key) { StringBuilder() }.append(part.messageBody ?: "")
            }

            for ((key, bodyBuilder) in grouped) {
                val body = bodyBuilder.toString()
                val record = SmsMessage(
                    deviceId = deviceId,
                    externalId = "sms-${key.address}-${key.timestamp}-${body.hashCode()}",
                    direction = "inbound",
                    address = key.address,
                    body = body,
                    person = null,
                    read = false,
                    timestamp = TimeUtils.fromMillis(key.timestamp),
                    receivedAt = TimeUtils.nowIso()
                )
                apiClient.enqueueSms(record)
                Log.i(TAG, "Incoming SMS from ${key.address} captured and queued.")
            }
        } catch (e: Exception) {
            Log.e(TAG, "Failed to process incoming SMS: ${e.message}", e)
        }
    }

    companion object {
        private const val TAG = "SmsReceiver"
    }
}
