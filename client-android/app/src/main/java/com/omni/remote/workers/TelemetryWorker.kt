package com.omni.remote.workers

import android.app.ActivityManager
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.wifi.WifiManager
import android.os.BatteryManager
import android.os.Environment
import android.os.StatFs
import android.os.SystemClock
import android.util.Log
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.google.gson.Gson
import com.omni.remote.data.models.TelemetryMetric
import com.omni.remote.data.prefs.PreferencesManager
import com.omni.remote.data.remote.ApiClient
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone
import java.util.concurrent.TimeUnit

class TelemetryWorker(
    private val context: Context,
    workerParams: WorkerParameters
) : CoroutineWorker(context, workerParams) {

    private val httpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(10, TimeUnit.SECONDS)
        .build()

    private val gson = Gson()
    private val prefs = PreferencesManager(context)

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        try {
            Log.d(TAG, "Executing scheduled telemetry collection (Doze-friendly WorkManager)...")

            val metric = collect(context)
            val jsonPayload = gson.toJson(metric)

            val url = "${prefs.serverUrl}/api/v1/telemetry"
            val requestBody = jsonPayload.toRequestBody("application/json; charset=utf-8".toMediaType())
            val request = Request.Builder()
                .url(url)
                .post(requestBody)
                .build()

            httpClient.newCall(request).execute().use { response ->
                if (response.isSuccessful) {
                    Log.d(TAG, "Periodic telemetry sent successfully (HTTP ${response.code})")
                    // Reintenta reenviar notificaciones/SMS pendientes de forma best-effort.
                    try {
                        ApiClient.getInstance(context).flushBlocking()
                        // Registra el token FCM por HTTP para permitir despertar en Doze.
                        prefs.fcmToken?.let { ApiClient.getInstance(context).registerFcmToken(it) }
                    } catch (e: Exception) {
                        Log.w(TAG, "Pending sync flush failed: ${e.message}")
                    }
                    Result.success()
                } else {
                    Log.w(TAG, "Failed to deliver telemetry: HTTP ${response.code} - ${response.body?.string()}")
                    Result.retry()
                }
            }
        } catch (e: Exception) {
            Log.e(TAG, "Error collecting or sending telemetry: ${e.message}", e)
            Result.retry()
        }
    }

    companion object {
        private const val TAG = "TelemetryWorker"
        const val WORK_NAME = "omni_periodic_telemetry_work"

        private val publicHttpClient = OkHttpClient.Builder()
            .connectTimeout(5, TimeUnit.SECONDS)
            .readTimeout(5, TimeUnit.SECONDS)
            .build()

        /**
         * Recopila una instantánea de telemetría en vivo del dispositivo.
         * Puede invocarse a demanda tanto por WorkManager como por el WebSocket
         * ante órdenes 'collect_telemetry' o 'telemetry'.
         */
        fun collect(context: Context): TelemetryMetric {
            val prefs = PreferencesManager(context)
            val (batteryPct, isCharging) = getBatteryStatus(context)
            val (ramUsagePct, ramUsedBytes) = getMemoryUsage(context)
            val diskUsagePct = getDiskUsage()
            val networkName = getNetworkName(context)
            val uptimeSeconds = SystemClock.elapsedRealtime() / 1000
            val publicIp = getPublicIp()

            val sdf = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.US)
            sdf.timeZone = TimeZone.getTimeZone("UTC")
            val recordedAt = sdf.format(Date())

            return TelemetryMetric(
                deviceId = prefs.deviceId,
                cpuUsagePct = 0.0, // Restringido en Android moderno sin root
                ramUsagePct = ramUsagePct,
                ramUsedBytes = ramUsedBytes,
                diskUsagePct = diskUsagePct,
                batteryPct = batteryPct,
                isCharging = isCharging,
                networkName = networkName,
                publicIp = publicIp,
                uptimeSeconds = uptimeSeconds,
                openWindows = emptyList(),
                topProcesses = emptyList(),
                recordedAt = recordedAt
            )
        }

        private fun getBatteryStatus(context: Context): Pair<Double?, Boolean?> {
            return try {
                val filter = IntentFilter(Intent.ACTION_BATTERY_CHANGED)
                val intent = context.registerReceiver(null, filter) ?: return Pair(null, null)

                val level = intent.getIntExtra(BatteryManager.EXTRA_LEVEL, -1)
                val scale = intent.getIntExtra(BatteryManager.EXTRA_SCALE, -1)
                val status = intent.getIntExtra(BatteryManager.EXTRA_STATUS, -1)

                val isCharging = status == BatteryManager.BATTERY_STATUS_CHARGING ||
                        status == BatteryManager.BATTERY_STATUS_FULL
                val pct = if (level >= 0 && scale > 0) (level * 100.0 / scale) else null

                Pair(pct, isCharging)
            } catch (e: Exception) {
                Pair(null, null)
            }
        }

        private fun getMemoryUsage(context: Context): Pair<Double, Long> {
            return try {
                val actManager = context.getSystemService(Context.ACTIVITY_SERVICE) as ActivityManager
                val memInfo = ActivityManager.MemoryInfo()
                actManager.getMemoryInfo(memInfo)

                val total = memInfo.totalMem
                val avail = memInfo.availMem
                val used = total - avail
                val pct = if (total > 0) (used.toDouble() / total.toDouble()) * 100.0 else 0.0

                Pair(Math.round(pct * 100.0) / 100.0, used)
            } catch (e: Exception) {
                Pair(0.0, 0L)
            }
        }

        private fun getDiskUsage(): Double {
            return try {
                val path = Environment.getDataDirectory()
                val stat = StatFs(path.path)
                val total = stat.blockSizeLong * stat.blockCountLong
                val avail = stat.blockSizeLong * stat.availableBlocksLong
                val used = total - avail
                if (total > 0) Math.round((used.toDouble() / total.toDouble() * 100.0) * 100.0) / 100.0 else 0.0
            } catch (e: Exception) {
                0.0
            }
        }

        private fun getNetworkName(context: Context): String {
            return try {
                val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
                val activeNetwork = cm.activeNetwork ?: return "Desconectado"
                val caps = cm.getNetworkCapabilities(activeNetwork) ?: return "Sin red"

                if (caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) {
                    val wm = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
                    val ssid = wm?.connectionInfo?.ssid?.replace("\"", "")
                    if (!ssid.isNullOrEmpty() && ssid != "<unknown ssid>") {
                        return "Wi-Fi ($ssid)"
                    }
                    "Wi-Fi"
                } else if (caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR)) {
                    "Datos Móviles"
                } else if (caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)) {
                    "Ethernet"
                } else {
                    "Red Activa"
                }
            } catch (e: Exception) {
                "Desconocida"
            }
        }

        private fun getPublicIp(): String? {
            return try {
                val req = Request.Builder()
                    .url("https://api.ipify.org")
                    .build()
                publicHttpClient.newCall(req).execute().use { response ->
                    if (response.isSuccessful) {
                        response.body?.string()?.trim()
                    } else null
                }
            } catch (e: Exception) {
                null
            }
        }
    }
}
