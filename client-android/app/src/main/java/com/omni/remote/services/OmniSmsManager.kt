package com.omni.remote.services

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.telephony.SmsManager
import android.util.Log
import androidx.core.content.ContextCompat
import com.omni.remote.data.models.SmsMessage
import com.omni.remote.data.prefs.PreferencesManager
import com.omni.remote.util.OpResult
import com.omni.remote.util.TimeUtils

/**
 * Utilidades de SMS: lectura del proveedor de contenido interno
 * (`content://sms/inbox`, requiere READ_SMS) y envío (requiere SEND_SMS).
 */
object OmniSmsManager {

    private const val TAG = "OmniSmsManager"

    fun hasReadPermission(context: Context): Boolean =
        ContextCompat.checkSelfPermission(context, Manifest.permission.READ_SMS) ==
                PackageManager.PERMISSION_GRANTED

    fun hasSendPermission(context: Context): Boolean =
        ContextCompat.checkSelfPermission(context, Manifest.permission.SEND_SMS) ==
                PackageManager.PERMISSION_GRANTED

    fun hasReceivePermission(context: Context): Boolean =
        ContextCompat.checkSelfPermission(context, Manifest.permission.RECEIVE_SMS) ==
                PackageManager.PERMISSION_GRANTED

    /**
     * Extrae el historial de la bandeja de entrada de SMS.
     */
    fun readInbox(context: Context, limit: Int): List<SmsMessage> {
        if (!hasReadPermission(context)) {
            Log.w(TAG, "READ_SMS permission not granted; cannot read inbox.")
            return emptyList()
        }

        val deviceId = PreferencesManager(context).deviceId
        val messages = mutableListOf<SmsMessage>()
        val projection = arrayOf(
            "_id",
            "address",
            "body",
            "date",
            "person",
            "read"
        )

        try {
            context.contentResolver.query(
                android.provider.Telephony.Sms.Inbox.CONTENT_URI,
                projection,
                null,
                null,
                "date DESC"
            )?.use { cursor ->
                val idIdx = cursor.getColumnIndexOrThrow("_id")
                val addressIdx = cursor.getColumnIndexOrThrow("address")
                val bodyIdx = cursor.getColumnIndexOrThrow("body")
                val dateIdx = cursor.getColumnIndexOrThrow("date")
                val personIdx = cursor.getColumnIndex("person")
                val readIdx = cursor.getColumnIndex("read")

                while (cursor.moveToNext() && messages.size < limit) {
                    val id = cursor.getLong(idIdx)
                    val date = cursor.getLong(dateIdx)
                    val read = if (readIdx >= 0) cursor.getInt(readIdx) == 1 else false
                    val person = if (personIdx >= 0) cursor.getString(personIdx) else null

                    messages.add(
                        SmsMessage(
                            deviceId = deviceId,
                            externalId = "sms-$id",
                            direction = "inbound",
                            address = cursor.getString(addressIdx) ?: "",
                            body = cursor.getString(bodyIdx) ?: "",
                            person = person,
                            read = read,
                            timestamp = TimeUtils.fromMillis(date),
                            receivedAt = TimeUtils.nowIso()
                        )
                    )
                }
            }
        } catch (e: Exception) {
            Log.w(TAG, "Failed to read SMS inbox: ${e.message}")
        }

        return messages
    }

    /**
     * Envía un SMS. Requiere el permiso SEND_SMS.
     */
    fun send(context: Context, address: String, body: String): OpResult {
        if (!hasSendPermission(context)) {
            return OpResult(false, "SEND_SMS permission not granted")
        }
        if (address.isBlank() || body.isBlank()) {
            return OpResult(false, "address and body are required")
        }

        return try {
            val smsManager: SmsManager = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                context.getSystemService(SmsManager::class.java)
            } else {
                @Suppress("DEPRECATION")
                SmsManager.getDefault()
            }

            val parts = smsManager.divideMessage(body)
            if (parts.size > 1) {
                smsManager.sendMultipartTextMessage(address, null, parts, null, null)
            } else {
                smsManager.sendTextMessage(address, null, body, null, null)
            }
            OpResult(true, "SMS sent to $address")
        } catch (e: Exception) {
            Log.w(TAG, "Failed to send SMS: ${e.message}")
            OpResult(false, "Failed to send SMS: ${e.message}")
        }
    }
}
