package com.omni.remote.receivers

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import com.omni.remote.OmniApp

class BootReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent?) {
        if (intent?.action == Intent.ACTION_BOOT_COMPLETED ||
            intent?.action == "android.intent.action.QUICKBOOT_POWERON"
        ) {
            Log.i(TAG, "Device boot completed. Ensuring periodic telemetry worker is scheduled...")
            OmniApp.scheduleTelemetry(context)
        }
    }

    companion object {
        private const val TAG = "BootReceiver"
    }
}
