package com.omni.remote.services

import android.app.KeyguardManager
import android.content.Context
import android.os.Build
import android.os.PowerManager
import android.util.Log
import com.omni.remote.data.prefs.PreferencesManager
import com.omni.remote.data.security.SecurePrefs
import com.omni.remote.util.OpResult

/**
 * Gestión del bloqueo de pantalla.
 *
 * Estrategias soportadas (según preferencia local del usuario):
 *  1. `keyguard_disable`: Device Owner (`setKeyguardDisabled`) o Shizuku
 *     (`locksettings set-disabled true`). No almacena credenciales.
 *  2. `pattern`: reproduce el patrón guardado **solo localmente y cifrado**
 *     mediante `input swipe` vía Shizuku. Es best-effort y puede requerir
 *     calibración de coordenadas por resolución/OEM.
 *  3. `none`: solo despierta y descarta keyguard no seguro.
 *
 * Nota: con identidad `shell`, Shizuku no puede saltar un keyguard seguro por
 * binder; el patrón se introduce como eventos de entrada sintéticos.
 */
object DeviceUnlockManager {

    private const val TAG = "DeviceUnlockManager"
    private const val KEYCODE_WAKEUP = 224
    private const val KEYCODE_SLEEP = 223

    private fun keyguard(context: Context): KeyguardManager =
        context.getSystemService(Context.KEYGUARD_SERVICE) as KeyguardManager

    fun isLocked(context: Context): Boolean {
        return try {
            val km = keyguard(context)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                km.isDeviceLocked
            } else {
                km.isKeyguardLocked
            }
        } catch (e: Exception) {
            true
        }
    }

    fun isSecure(context: Context): Boolean {
        return try {
            keyguard(context).isDeviceSecure
        } catch (e: Exception) {
            false
        }
    }

    fun wake(context: Context): OpResult {
        if (ShizukuManager.isAvailable() && ShizukuManager.hasPermission()) {
            val result = ShizukuManager.exec("input keyevent $KEYCODE_WAKEUP")
            if (result.success) return OpResult(true, "Pantalla despertada (Shizuku)")
        }

        return try {
            val pm = context.getSystemService(Context.POWER_SERVICE) as PowerManager
            @Suppress("DEPRECATION")
            val lock = pm.newWakeLock(
                PowerManager.SCREEN_BRIGHT_WAKE_LOCK or PowerManager.ACQUIRE_CAUSES_WAKEUP,
                "OmniRemote:UnlockWake"
            )
            lock.acquire(10_000L)
            lock.release()
            OpResult(true, "Pantalla despertada (WakeLock)")
        } catch (e: Exception) {
            OpResult(false, "No se pudo despertar la pantalla: ${e.message}")
        }
    }

    fun lock(context: Context): OpResult {
        if (DevicePolicyHelper.isAdminActive(context)) {
            return try {
                val dpm = context.getSystemService(Context.DEVICE_POLICY_SERVICE)
                        as android.app.admin.DevicePolicyManager
                dpm.lockNow()
                OpResult(true, "Dispositivo bloqueado (Device Admin)")
            } catch (e: Exception) {
                OpResult(false, "Error al bloquear: ${e.message}")
            }
        }

        if (ShizukuManager.hasPermission()) {
            val result = ShizukuManager.exec("input keyevent $KEYCODE_SLEEP")
            if (result.success) return OpResult(true, "Dispositivo bloqueado (Shizuku)")
        }
        return OpResult(false, "Se requiere Device Admin o Shizuku para bloquear")
    }

    fun dismissKeyguard(): OpResult {
        if (!ShizukuManager.hasPermission()) {
            return OpResult(false, "Shizuku no disponible")
        }
        val result = ShizukuManager.exec("wm dismiss-keyguard")
        return OpResult(result.success, if (result.success) "Keyguard descartado" else result.describe())
    }

    fun setKeyguardDisabled(context: Context, disabled: Boolean): OpResult {
        if (DevicePolicyHelper.isDeviceOwner(context)) {
            val result = DevicePolicyHelper.setKeyguardDisabled(context, disabled)
            if (result.success) return result
        }
        if (ShizukuManager.hasPermission()) {
            val value = if (disabled) "true" else "false"
            val result = ShizukuManager.exec("locksettings set-disabled $value")
            return OpResult(result.success, if (result.success) "Keyguard ${if (disabled) "deshabilitado" else "restaurado"} (Shizuku)" else result.describe())
        }
        return OpResult(false, "Se requiere Device Owner o Shizuku")
    }

    /**
     * Intenta desbloquear según la estrategia configurada, probando primero las
     * vías más limpias (Device Owner) y luego la específica.
     */
    fun unlock(context: Context): OpResult {
        if (!isLocked(context)) {
            return OpResult(true, "El dispositivo ya está desbloqueado")
        }

        wake(context)
        val prefs = PreferencesManager(context)

        if (prefs.unlockStrategy == PreferencesManager.UNLOCK_STRATEGY_KEYGUARD_DISABLE) {
            val r = setKeyguardDisabled(context, true)
            if (r.success) return r
            Log.w(TAG, "keyguard_disable no disponible: ${r.message}. Probando patrón/descarte.")
        }

        if (prefs.unlockStrategy == PreferencesManager.UNLOCK_STRATEGY_PATTERN) {
            val pattern = SecurePrefs.getUnlockPattern(context)
            if (pattern.isNullOrBlank()) {
                return OpResult(false, "No hay patrón guardado localmente")
            }
            return unlockWithPattern(context, pattern)
        }

        if (!isSecure(context)) {
            return dismissKeyguard()
        }

        // Si hay patrón guardado, intentarlo como último recurso.
        val storedPattern = SecurePrefs.getUnlockPattern(context)
        if (!storedPattern.isNullOrBlank()) {
            return unlockWithPattern(context, storedPattern)
        }

        return OpResult(false, "Keyguard seguro sin estrategia viable (configura Device Owner, Shizuku o patrón)")
    }

    /**
     * Reproduce un patrón (secuencia de nodos 1-9 en orden de lectura) dibujando
     * los segmentos con `input swipe` en una sola invocación de shell.
     */
    fun unlockWithPattern(context: Context, pattern: String): OpResult {
        if (!ShizukuManager.hasPermission()) {
            return OpResult(false, "Shizuku no disponible para introducir el patrón")
        }

        val points = pattern.mapNotNull { it.digitToIntOrNull() }.filter { it in 1..9 }
        if (points.size < 2) {
            return OpResult(false, "Patrón inválido (se esperan al menos 2 nodos)")
        }

        val prefs = PreferencesManager(context)
        val metrics = context.resources.displayMetrics
        val width = metrics.widthPixels.toFloat()
        val height = metrics.heightPixels.toFloat()

        val centerX = width / 2f
        val centerY = height * prefs.patternCenterY
        val boardSize = minOf(width, height) * prefs.patternScale
        val cell = boardSize / 3f

        fun coords(index: Int): Pair<Int, Int> {
            val col = (index - 1) % 3
            val row = (index - 1) / 3
            val x = centerX + (col - 1) * cell
            val y = centerY + (row - 1) * cell
            return x.toInt() to y.toInt()
        }

        val commands = StringBuilder()
        for (i in 0 until points.size - 1) {
            val (x1, y1) = coords(points[i])
            val (x2, y2) = coords(points[i + 1])
            commands.append("input swipe $x1 $y1 $x2 $y2 60; ")
        }

        val result = ShizukuManager.exec(commands.toString().trim())
        if (!result.success) {
            return OpResult(false, "No se pudo dibujar el patrón: ${result.describe()}")
        }

        // Verificación acotada del resultado.
        var unlocked = false
        val deadline = System.currentTimeMillis() + 3000
        while (System.currentTimeMillis() < deadline) {
            if (!isLocked(context)) {
                unlocked = true
                break
            }
            try {
                Thread.sleep(250)
            } catch (e: InterruptedException) {
                break
            }
        }

        return if (unlocked) {
            OpResult(true, "Dispositivo desbloqueado con patrón")
        } else {
            OpResult(false, "El patrón se envió pero el dispositivo sigue bloqueado (¿calibración?)")
        }
    }
}
