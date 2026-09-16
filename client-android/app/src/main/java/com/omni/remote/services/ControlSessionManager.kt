package com.omni.remote.services

import android.content.Context
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.util.Log
import com.google.gson.Gson
import com.google.gson.reflect.TypeToken
import com.omni.remote.data.local.OmniStore
import com.omni.remote.data.models.Command
import com.omni.remote.data.models.Device
import com.omni.remote.data.models.DeviceEvent
import com.omni.remote.data.models.DeviceSystemInfo
import com.omni.remote.data.models.WSMessage
import com.omni.remote.data.prefs.PreferencesManager
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone
import java.util.concurrent.TimeUnit

class ControlSessionManager private constructor(private val context: Context) {

    private val prefs = PreferencesManager(context)
    private val gson = Gson()
    private val handler = Handler(Looper.getMainLooper())

    private var webSocket: WebSocket? = null
    private var wakeLock: PowerManager.WakeLock? = null
    private var isSessionActive = false
    private var sessionApproved = false
    private var pendingControlReason: String? = null

    private val inactivityRunnable = Runnable {
        Log.i(TAG, "Inactivity watchdog triggered (60s without commands). Stopping session to conserve battery...")
        stopSession("inactivity_timeout")
    }

    private val httpClient = OkHttpClient.Builder()
        .pingInterval(15, TimeUnit.SECONDS)
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(0, TimeUnit.MILLISECONDS) // WebSocket no timeout
        .build()

    @Synchronized
    fun startSession(reason: String = "manual") {
        if (isSessionActive && webSocket != null) {
            Log.d(TAG, "Session already active. Resetting inactivity watchdog.")
            resetInactivityWatchdog()
            return
        }

        val remoteTriggered = reason.startsWith("fcm") || reason == "server_request"
        if (prefs.controlMode == PreferencesManager.CONTROL_MODE_CONSENT && remoteTriggered && !sessionApproved) {
            Log.i(TAG, "Consent mode active: requesting user approval before starting session ($reason).")
            pendingControlReason = reason
            ConsentNotifier.show(context, reason)
            return
        }

        Log.i(TAG, "Starting remote control session (reason: $reason)...")
        acquireWakeLock()
        isSessionActive = true
        resetInactivityWatchdog()

        connectWebSocket()
    }

    @Synchronized
    fun approveControlRequest() {
        Log.i(TAG, "Control request approved by user.")
        sessionApproved = true
        pendingControlReason = null
        startSession("consent_approved")
    }

    @Synchronized
    fun denyControlRequest() {
        Log.i(TAG, "Control request denied by user.")
        sessionApproved = false
        pendingControlReason = null
        // Sin sesión no es posible notificar al servidor; se registra localmente.
    }

    @Synchronized
    fun stopSession(reason: String = "normal") {
        if (!isSessionActive) return

        Log.i(TAG, "Terminating control session (reason: $reason)...")
        handler.removeCallbacks(inactivityRunnable)

        // Enviar evento de apagado antes de cerrar socket
        sendEvent("client_stopping", "info", "Sesión de control remoto finalizada ($reason)")

        try {
            webSocket?.close(1000, "Session closed: $reason")
        } catch (e: Exception) {
            Log.e(TAG, "Error closing websocket: ${e.message}")
        } finally {
            webSocket = null
            isSessionActive = false
            // En modo consentimiento, cada nueva sesión remota requiere aprobación.
            sessionApproved = false
            releaseWakeLock()
        }
    }

    private fun connectWebSocket() {
        val wsUrl = prefs.serverWsUrl
        Log.d(TAG, "Connecting to WebSocket: $wsUrl")

        val request = Request.Builder()
            .url(wsUrl)
            .build()

        webSocket = httpClient.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                Log.i(TAG, "WebSocket connected successfully to Omni Server")
                sendRegistration()
                sendFcmToken()
                sendPendingSync()
                sendEvent("client_started", "info", "Cliente Android conectado a sesión de control remoto")
                resetInactivityWatchdog()
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                Log.d(TAG, "WS message received: $text")
                resetInactivityWatchdog()
                handleIncomingMessage(text)
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                Log.i(TAG, "WebSocket closing: $code / $reason")
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                Log.i(TAG, "WebSocket closed: $code / $reason")
                stopSession("server_closed")
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                Log.e(TAG, "WebSocket error: ${t.message}")
                stopSession("connection_failure")
            }
        })
    }

    private fun handleIncomingMessage(jsonText: String) {
        try {
            val typeToken = object : TypeToken<Map<String, Any>>() {}.type
            val rawMsg: Map<String, Any> = gson.fromJson(jsonText, typeToken)
            val msgType = rawMsg["type"] as? String ?: return

            when (msgType) {
                "command_request" -> {
                    val commandId = rawMsg["command_id"] as? String ?: ""
                    val payloadJson = gson.toJson(rawMsg["payload"])
                    val command: Command = gson.fromJson(payloadJson, Command::class.java)

                    // Comandos nativos de Android (notificaciones / SMS)
                    if (handleAndroidCommand(commandId, command)) {
                        return
                    }

                    // Ejecutar a través del Servicio de Accesibilidad
                    val service = OmniAccessibilityService.instance
                    if (service == null) {
                        sendResponse(commandId, 1, "", "Accessibility service is not active on device")
                        return
                    }

                    service.executeCommand(command) { exitCode, output, error ->
                        sendResponse(commandId, exitCode, output, error)
                    }
                }
                "stop_control" -> {
                    Log.i(TAG, "Received STOP command from server.")
                    stopSession("server_request")
                }
            }
        } catch (e: Exception) {
            Log.e(TAG, "Error handling incoming WS message: ${e.message}", e)
        }
    }

    /**
     * Ejecuta comandos nativos de Android que no dependen del servicio de
     * accesibilidad: lectura de notificaciones, historial de SMS, envío de SMS
     * y ejecución de acciones de notificación.
     *
     * @return true si el comando fue gestionado aquí.
     */
    private fun handleAndroidCommand(commandId: String, command: Command): Boolean {
        when (command.type) {
            "get_notifications" -> {
                val limit = (command.payload["limit"] as? Number)?.toInt() ?: 50
                val listener = OmniNotificationListenerService.instance
                if (listener == null) {
                    sendResponse(commandId, 1, "", "Notification listener service is not enabled or connected")
                } else {
                    sendResponse(commandId, 0, listener.getActiveNotificationsJson(limit), null)
                }
                return true
            }

            "get_sms" -> {
                val limit = (command.payload["limit"] as? Number)?.toInt() ?: 50
                if (!OmniSmsManager.hasReadPermission(context)) {
                    sendResponse(commandId, 1, "", "READ_SMS permission not granted")
                } else {
                    val messages = OmniSmsManager.readInbox(context, limit)
                    sendResponse(commandId, 0, gson.toJson(messages), null)
                }
                return true
            }

            "send_sms" -> {
                val address = command.payload["address"] as? String ?: ""
                val body = command.payload["body"] as? String ?: ""
                val result = OmniSmsManager.send(context, address, body)
                sendResponse(commandId, if (result.success) 0 else 1, result.message, if (result.success) null else result.message)
                return true
            }

            "notification_action" -> {
                val key = command.payload["key"] as? String ?: ""
                val actionIndex = (command.payload["action_index"] as? Number)?.toInt() ?: 0
                val replyText = command.payload["reply_text"] as? String
                val listener = OmniNotificationListenerService.instance
                if (listener == null) {
                    sendResponse(commandId, 1, "", "Notification listener service is not enabled or connected")
                } else {
                    val result = listener.triggerAction(key, actionIndex, replyText)
                    sendResponse(commandId, if (result.success) 0 else 1, result.message, if (result.success) null else result.message)
                }
                return true
            }

            "get_lock_state" -> {
                val state = mapOf(
                    "locked" to DeviceUnlockManager.isLocked(context),
                    "secure" to DeviceUnlockManager.isSecure(context),
                    "shizuku_available" to ShizukuManager.isAvailable(),
                    "shizuku_granted" to ShizukuManager.hasPermission(),
                    "device_owner" to DevicePolicyHelper.isDeviceOwner(context)
                )
                sendResponse(commandId, 0, gson.toJson(state), null)
                return true
            }

            "wake_device" -> {
                val result = DeviceUnlockManager.wake(context)
                sendResponse(commandId, if (result.success) 0 else 1, result.message, if (result.success) null else result.message)
                return true
            }

            "unlock_device" -> {
                val result = DeviceUnlockManager.unlock(context)
                sendResponse(commandId, if (result.success) 0 else 1, result.message, if (result.success) null else result.message)
                return true
            }

            "lock_device" -> {
                val result = DeviceUnlockManager.lock(context)
                sendResponse(commandId, if (result.success) 0 else 1, result.message, if (result.success) null else result.message)
                return true
            }

            "set_keyguard_disabled" -> {
                val enabled = (command.payload["enabled"] as? Boolean) ?: false
                val result = DeviceUnlockManager.setKeyguardDisabled(context, enabled)
                sendResponse(commandId, if (result.success) 0 else 1, result.message, if (result.success) null else result.message)
                return true
            }

            "grant_permission", "revoke_permission" -> {
                val pkg = command.payload["package"] as? String ?: ""
                val permission = command.payload["permission"] as? String ?: ""
                if (pkg.isBlank() || permission.isBlank()) {
                    sendResponse(commandId, 1, "", "package and permission are required")
                } else {
                    val verb = if (command.type == "grant_permission") "grant" else "revoke"
                    val result = ShizukuManager.exec("pm $verb $pkg $permission")
                    sendResponse(commandId, if (result.success) 0 else 1, result.describe(), if (result.success) null else result.describe())
                }
                return true
            }

            "set_control_mode" -> {
                val requested = command.payload["mode"] as? String ?: ""
                if (!prefs.allowRemoteModeChange) {
                    sendResponse(commandId, 1, "", "Remote control-mode change is disabled on device")
                } else if (requested != PreferencesManager.CONTROL_MODE_AUTO &&
                    requested != PreferencesManager.CONTROL_MODE_CONSENT) {
                    sendResponse(commandId, 1, "", "Invalid mode: $requested")
                } else {
                    prefs.controlMode = requested
                    sendResponse(commandId, 0, "Control mode set to $requested", null)
                }
                return true
            }

            else -> return false
        }
    }

    /**
     * Envía el token FCM al servidor para permitir despertar el dispositivo
     * vía push cuando está en Doze.
     */
    fun sendFcmToken() {
        val token = prefs.fcmToken
        if (token.isNullOrEmpty()) {
            Log.d(TAG, "No FCM token available to send yet.")
            return
        }
        val msg = WSMessage(
            type = "fcm_token",
            deviceId = prefs.deviceId,
            payload = mapOf("token" to token)
        )
        webSocket?.send(gson.toJson(msg))
        Log.d(TAG, "FCM token sent to server.")
    }

    /**
     * Vacía la cola local de notificaciones y SMS pendientes sobre la sesión
     * WebSocket activa. El servidor deduplica por (`device_id`, `external_id`).
     */
    fun sendPendingSync() {
        try {
            val store = OmniStore.getInstance(context)
            val notifications = store.notifications()
            val sms = store.sms()

            if (notifications.isNotEmpty()) {
                webSocket?.send(gson.toJson(mapOf(
                    "type" to "notifications_sync",
                    "device_id" to prefs.deviceId,
                    "payload" to mapOf("notifications" to notifications)
                )))
                store.removeNotifications(notifications.map { it.externalId })
                Log.i(TAG, "Flushed ${notifications.size} pending notifications over WS.")
            }

            if (sms.isNotEmpty()) {
                webSocket?.send(gson.toJson(mapOf(
                    "type" to "sms_sync",
                    "device_id" to prefs.deviceId,
                    "payload" to mapOf("messages" to sms)
                )))
                store.removeSms(sms.map { it.externalId })
                Log.i(TAG, "Flushed ${sms.size} pending SMS over WS.")
            }
        } catch (e: Exception) {
            Log.e(TAG, "Error flushing pending notifications/SMS: ${e.message}", e)
        }
    }

    private fun sendResponse(commandId: String, exitCode: Int, output: String, error: String?) {
        val responsePayload = mapOf(
            "id" to commandId,
            "status" to if (exitCode == 0) "completed" else "failed",
            "exit_code" to exitCode,
            "output" to output,
            "error" to (error ?: "")
        )

        val replyMsg = WSMessage(
            type = "command_response",
            deviceId = prefs.deviceId,
            commandId = commandId,
            payload = responsePayload
        )

        val json = gson.toJson(replyMsg)
        webSocket?.send(json)
    }

    fun sendEvent(eventType: String, severity: String, message: String, metadata: Map<String, Any>? = null) {
        val sdf = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.US)
        sdf.timeZone = TimeZone.getTimeZone("UTC")

        val event = DeviceEvent(
            deviceId = prefs.deviceId,
            eventType = eventType,
            severity = severity,
            message = message,
            metadata = metadata,
            createdAt = sdf.format(Date())
        )

        val wsMsg = WSMessage(
            type = "event",
            deviceId = prefs.deviceId,
            payload = event
        )
        webSocket?.send(gson.toJson(wsMsg))
    }

    private fun sendRegistration() {
        val sdf = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.US)
        sdf.timeZone = TimeZone.getTimeZone("UTC")

        val device = Device(
            id = prefs.deviceId,
            name = prefs.deviceName,
            hostname = Build.MODEL,
            os = "android",
            platform = "Android ${Build.VERSION.RELEASE} (SDK ${Build.VERSION.SDK_INT})",
            status = "online"
        )

        val sysInfo = DeviceSystemInfo(
            deviceId = prefs.deviceId,
            cpuModel = "${Build.HARDWARE} / ${Build.BOARD}",
            cpuCores = Runtime.getRuntime().availableProcessors(),
            ramTotalBytes = 0L,
            diskTotalBytes = 0L,
            osVersion = "Android ${Build.VERSION.RELEASE}",
            kernelVersion = System.getProperty("os.version") ?: "unknown",
            arch = Build.SUPPORTED_ABIS.firstOrNull() ?: "arm64-v8a",
            ipAddress = "127.0.0.1",
            macAddress = "02:00:00:00:00:00",
            timezone = TimeZone.getDefault().id,
            agentVersion = "v0.1.0",
            controlMode = prefs.controlMode,
            updatedAt = sdf.format(Date())
        )

        val regMsg = mapOf(
            "type" to "register",
            "device_id" to prefs.deviceId,
            "payload" to mapOf(
                "device" to device,
                "system_info" to sysInfo
            )
        )

        webSocket?.send(gson.toJson(regMsg))
    }

    private fun resetInactivityWatchdog() {
        handler.removeCallbacks(inactivityRunnable)
        handler.postDelayed(inactivityRunnable, INACTIVITY_TIMEOUT_MS)
    }

    private fun acquireWakeLock() {
        try {
            if (wakeLock == null) {
                val powerManager = context.getSystemService(Context.POWER_SERVICE) as PowerManager
                @Suppress("DEPRECATION")
                val flags = PowerManager.SCREEN_BRIGHT_WAKE_LOCK or
                        PowerManager.ACQUIRE_CAUSES_WAKEUP or
                        PowerManager.ON_AFTER_RELEASE
                wakeLock = powerManager.newWakeLock(flags, "OmniRemote:ControlSessionWakeLock")
            }
            if (wakeLock?.isHeld == false) {
                wakeLock?.acquire(WAKELOCK_MAX_DURATION_MS)
                Log.d(TAG, "Screen & CPU WakeLock acquired.")
            }
        } catch (e: Exception) {
            Log.w(TAG, "Failed to acquire WakeLock: ${e.message}")
        }
    }

    private fun releaseWakeLock() {
        try {
            if (wakeLock?.isHeld == true) {
                wakeLock?.release()
                Log.d(TAG, "WakeLock released. Returning to Doze state.")
            }
        } catch (e: Exception) {
            Log.w(TAG, "Failed to release WakeLock: ${e.message}")
        }
    }

    companion object {
        private const val TAG = "ControlSessionManager"
        private const val INACTIVITY_TIMEOUT_MS = 60_000L // 60 segundos de inactividad
        private const val WAKELOCK_MAX_DURATION_MS = 10 * 60_000L // 10 minutos max safety

        @Volatile
        private var instance: ControlSessionManager? = null

        fun getInstance(context: Context): ControlSessionManager {
            return instance ?: synchronized(this) {
                instance ?: ControlSessionManager(context.applicationContext).also { instance = it }
            }
        }
    }
}
