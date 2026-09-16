package com.omni.remote.services

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import com.omni.remote.receivers.ControlConsentReceiver

/**
 * Notificación de consentimiento: en modo `consent`, toda toma de control
 * remota debe ser aprobada explícitamente por el usuario del dispositivo.
 */
object ConsentNotifier {

    private const val TAG = "ConsentNotifier"
    private const val CHANNEL_ID = "omni_control_consent"
    private const val NOTIFICATION_ID = 9101

    fun ensureChannel(context: Context) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val manager = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (manager.getNotificationChannel(CHANNEL_ID) != null) return

        val channel = NotificationChannel(
            CHANNEL_ID,
            "Solicitudes de control remoto",
            NotificationManager.IMPORTANCE_HIGH
        ).apply {
            description = "Pide autorización antes de permitir que un operador controle el dispositivo."
            setShowBadge(true)
        }
        manager.createNotificationChannel(channel)
    }

    fun show(context: Context, reason: String) {
        ensureChannel(context)

        val acceptIntent = Intent(context, ControlConsentReceiver::class.java).apply {
            action = ControlConsentReceiver.ACTION_ACCEPT
        }
        val denyIntent = Intent(context, ControlConsentReceiver::class.java).apply {
            action = ControlConsentReceiver.ACTION_DENY
        }

        val flags = PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        val acceptPi = PendingIntent.getBroadcast(context, 101, acceptIntent, flags)
        val denyPi = PendingIntent.getBroadcast(context, 102, denyIntent, flags)

        val notification = NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setContentTitle("Solicitud de control remoto")
            .setContentText("Un operador quiere tomar el control ($reason). ¿Permitir?")
            .setStyle(NotificationCompat.BigTextStyle().bigText(
                "Un operador remoto solicita tomar el control del dispositivo ($reason). " +
                        "Al aceptar se podrá desbloquear y controlar el dispositivo."
            ))
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_ALARM)
            .setAutoCancel(true)
            .setOngoing(true)
            .addAction(0, "Denegar", denyPi)
            .addAction(0, "Permitir", acceptPi)
            .build()

        try {
            NotificationManagerCompat.from(context).notify(NOTIFICATION_ID, notification)
        } catch (e: SecurityException) {
            // Falta POST_NOTIFICATIONS (Android 13+): sin permiso no se puede pedir consentimiento.
            Log.w(TAG, "No se pudo mostrar la notificación de consentimiento: ${e.message}")
        }
    }

    fun cancel(context: Context) {
        NotificationManagerCompat.from(context).cancel(NOTIFICATION_ID)
    }
}
