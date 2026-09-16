package com.omni.remote.receivers

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import com.omni.remote.services.ConsentNotifier
import com.omni.remote.services.ControlSessionManager

/**
 * Recibe las acciones "Permitir"/"Denegar" de la notificación de consentimiento.
 */
class ControlConsentReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent?) {
        when (intent?.action) {
            ACTION_ACCEPT -> {
                Log.i(TAG, "User APPROVED remote control request.")
                ConsentNotifier.cancel(context)
                ControlSessionManager.getInstance(context).approveControlRequest()
            }
            ACTION_DENY -> {
                Log.i(TAG, "User DENIED remote control request.")
                ConsentNotifier.cancel(context)
                ControlSessionManager.getInstance(context).denyControlRequest()
            }
        }
    }

    companion object {
        private const val TAG = "ControlConsentReceiver"
        const val ACTION_ACCEPT = "com.omni.remote.CONSENT_ACCEPT"
        const val ACTION_DENY = "com.omni.remote.CONSENT_DENY"
    }
}
