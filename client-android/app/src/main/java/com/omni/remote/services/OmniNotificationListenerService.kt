package com.omni.remote.services

import android.app.Notification
import android.app.RemoteInput
import android.content.Intent
import android.os.Bundle
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import android.util.Log
import com.google.gson.Gson
import com.omni.remote.data.models.NotificationAction
import com.omni.remote.data.models.NotificationRecord
import com.omni.remote.data.prefs.PreferencesManager
import com.omni.remote.data.remote.ApiClient
import com.omni.remote.util.OpResult
import com.omni.remote.util.TimeUtils

/**
 * Lee, en tiempo real, las notificaciones publicadas por cualquier aplicación
 * (WhatsApp, correo, bancos, etc.) mediante el NotificationListenerService
 * nativo de Android. Requiere que el usuario otorgue manualmente el permiso
 * "Acceso a notificaciones" en Ajustes > Notificaciones.
 */
class OmniNotificationListenerService : NotificationListenerService() {

    private val gson = Gson()

    override fun onListenerConnected() {
        super.onListenerConnected()
        instance = this
        Log.i(TAG, "NotificationListenerService connected.")
    }

    override fun onListenerDisconnected() {
        super.onListenerDisconnected()
        if (instance == this) instance = null
        Log.i(TAG, "NotificationListenerService disconnected.")
    }

    override fun onDestroy() {
        super.onDestroy()
        if (instance == this) instance = null
    }

    override fun onNotificationPosted(sbn: StatusBarNotification?) {
        if (sbn == null) return
        try {
            val record = buildRecord(sbn)
            ApiClient.getInstance(this).enqueueNotification(record)
            Log.d(TAG, "Captured notification from ${record.packageName}: ${record.title}")
        } catch (e: Exception) {
            Log.w(TAG, "Failed to process posted notification: ${e.message}")
        }
    }

    /**
     * Devuelve las notificaciones activas actualmente en el panel, serializadas
     * como JSON para responder al servidor ante un comando `get_notifications`.
     */
    fun getActiveNotificationsJson(limit: Int): String {
        return try {
            val active = activeNotifications ?: return "[]"
            val records = active
                .sortedByDescending { it.postTime }
                .take(limit.coerceAtLeast(1))
                .map { buildRecord(it) }
            gson.toJson(records)
        } catch (e: Exception) {
            Log.w(TAG, "Failed to read active notifications: ${e.message}")
            "[]"
        }
    }

    /**
     * Ejecuta el botón de acción de una notificación (por ejemplo "Responder" o
     * "Marcar como leído"), con soporte opcional de respuesta por RemoteInput.
     */
    fun triggerAction(key: String, actionIndex: Int, replyText: String?): OpResult {
        return try {
            val active = activeNotifications
                ?: return OpResult(false, "Notification listener not connected")
            val sbn = active.firstOrNull { it.key == key }
                ?: return OpResult(false, "Notification not found for key: $key")

            val actions = sbn.notification.actions
            if (actions.isNullOrEmpty()) {
                return OpResult(false, "Notification has no actions")
            }
            if (actionIndex < 0 || actionIndex >= actions.size) {
                return OpResult(false, "Invalid action index $actionIndex (available: ${actions.size})")
            }

            val action = actions[actionIndex]
            val intent = Intent()

            if (!replyText.isNullOrEmpty()) {
                val remoteInputs = action.remoteInputs
                if (!remoteInputs.isNullOrEmpty()) {
                    val results = Bundle()
                    for (input in remoteInputs) {
                        results.putCharSequence(input.resultKey, replyText)
                    }
                    @Suppress("DEPRECATION")
                    RemoteInput.addResultsToIntent(remoteInputs, intent, results)
                }
            }

            action.actionIntent.send(this, 0, intent)
            OpResult(true, "Action '${action.title}' executed on notification $key")
        } catch (e: Exception) {
            Log.w(TAG, "Failed to trigger notification action: ${e.message}")
            OpResult(false, "Failed to execute notification action: ${e.message}")
        }
    }

    private fun buildRecord(sbn: StatusBarNotification): NotificationRecord {
        val extras = sbn.notification.extras
        val title = extras.getCharSequence(Notification.EXTRA_TITLE)?.toString().orEmpty()
        val text = extras.getCharSequence(Notification.EXTRA_TEXT)?.toString().orEmpty()
        val bigText = extras.getCharSequence(Notification.EXTRA_BIG_TEXT)?.toString().orEmpty()
        val subText = extras.getCharSequence(Notification.EXTRA_SUB_TEXT)?.toString()

        val displayText = if (bigText.isNotBlank()) bigText else text

        val actions = sbn.notification.actions?.mapIndexedNotNull { index, action ->
            val actionTitle = action.title?.toString()
            if (actionTitle.isNullOrBlank()) null else NotificationAction(actionTitle, index)
        }.orEmpty()

        return NotificationRecord(
            deviceId = PreferencesManager(applicationContext).deviceId,
            externalId = sbn.key,
            packageName = sbn.packageName,
            appName = resolveAppName(sbn.packageName),
            title = title,
            text = displayText,
            subText = subText,
            category = sbn.notification.category,
            isOngoing = sbn.isOngoing,
            isClearable = sbn.isClearable,
            actions = actions,
            postedAt = TimeUtils.fromMillis(sbn.postTime),
            receivedAt = TimeUtils.nowIso()
        )
    }

    private fun resolveAppName(packageName: String): String? {
        return try {
            val pm = packageManager
            val info = pm.getApplicationInfo(packageName, 0)
            pm.getApplicationLabel(info).toString()
        } catch (e: Exception) {
            null
        }
    }

    companion object {
        private const val TAG = "OmniNotifListener"

        @Volatile
        var instance: OmniNotificationListenerService? = null
            private set
    }
}
