package com.omni.remote

import android.app.Application
import android.content.Context
import android.util.Log
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import com.omni.remote.services.ConsentNotifier
import com.omni.remote.services.ShizukuManager
import com.omni.remote.workers.TelemetryWorker
import java.util.concurrent.TimeUnit

class OmniApp : Application() {

    override fun onCreate() {
        super.onCreate()
        Log.i(TAG, "Initializing OmniApp...")
        ShizukuManager.init(this)
        ConsentNotifier.ensureChannel(this)
        scheduleTelemetry(this)
    }

    companion object {
        private const val TAG = "OmniApp"

        fun scheduleTelemetry(context: Context) {
            val constraints = Constraints.Builder()
                .setRequiredNetworkType(NetworkType.CONNECTED)
                .setRequiresBatteryNotLow(true)
                .build()

            // Intervalo de 1 hora (al igual que los clientes Linux y Windows)
            val telemetryWorkRequest = PeriodicWorkRequestBuilder<TelemetryWorker>(
                1, TimeUnit.HOURS,
                15, TimeUnit.MINUTES // Flex interval
            )
                .setConstraints(constraints)
                .build()

            WorkManager.getInstance(context).enqueueUniquePeriodicWork(
                TelemetryWorker.WORK_NAME,
                ExistingPeriodicWorkPolicy.UPDATE,
                telemetryWorkRequest
            )

            Log.i(TAG, "Periodic telemetry scheduled with WorkManager (1 hour interval, battery not low).")
        }
    }
}
