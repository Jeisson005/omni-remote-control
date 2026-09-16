package com.omni.remote.services

import android.content.Context
import android.content.pm.PackageManager
import android.util.Log
import rikka.shizuku.Shizuku

/**
 * Envoltorio mínimo sobre Shizuku.
 *
 * Shizuku permite ejecutar comandos con identidad `shell` (UID 2000) o `root`
 * una vez que el usuario lo autoriza. Se usa para:
 *  - `input keyevent` / `input swipe` (despertar y desbloquear).
 *  - `locksettings set-disabled` (deshabilitar el keyguard).
 *  - `pm grant` / `pm revoke` (conceder permisos peligrosos silenciosamente).
 *
 * Limitación importante: con identidad `shell` NO se puede saltar un keyguard
 * seguro por binder; `wm dismiss-keyguard` solo funciona con keyguard no seguro.
 */
object ShizukuManager {

    private const val TAG = "ShizukuManager"

    data class ShellResult(val exitCode: Int, val stdout: String, val stderr: String) {
        val success: Boolean get() = exitCode == 0
        fun describe(): String =
            if (success) stdout.ifBlank { "ok" }
            else stderr.ifBlank { "exit code $exitCode" }
    }

    @Volatile
    private var initialized = false

    private val permissionListener = Shizuku.OnRequestPermissionResultListener { requestCode, grantResult ->
        Log.i(TAG, "Shizuku permission result for request $requestCode: $grantResult")
    }

    fun init(context: Context) {
        if (initialized) return
        initialized = true
        try {
            Shizuku.addRequestPermissionResultListener(permissionListener)
        } catch (e: Exception) {
            Log.w(TAG, "Could not register Shizuku listener: ${e.message}")
        }
    }

    fun isAvailable(): Boolean {
        return try {
            Shizuku.pingBinder()
        } catch (e: Exception) {
            false
        }
    }

    fun hasPermission(): Boolean {
        return try {
            isAvailable() && Shizuku.checkSelfPermission() == PackageManager.PERMISSION_GRANTED
        } catch (e: Exception) {
            false
        }
    }

    fun isRoot(): Boolean {
        return try {
            isAvailable() && Shizuku.getUid() == 0
        } catch (e: Exception) {
            false
        }
    }

    fun requestPermission(requestCode: Int) {
        try {
            Shizuku.requestPermission(requestCode)
        } catch (e: Exception) {
            Log.w(TAG, "Could not request Shizuku permission: ${e.message}")
        }
    }

    fun exec(command: String): ShellResult {
        if (!isAvailable()) {
            return ShellResult(-1, "", "Shizuku no está disponible (¿app instalada y activada?)")
        }
        if (!hasPermission()) {
            return ShellResult(-1, "", "Permiso de Shizuku no concedido")
        }

        return try {
            @Suppress("DEPRECATION")
            val process = Shizuku.newProcess(arrayOf("sh", "-c", command), null, null)
            val stdout = process.inputStream.bufferedReader().use { it.readText() }
            val stderr = process.errorStream.bufferedReader().use { it.readText() }
            val exit = process.waitFor()
            ShellResult(exit, stdout.trim(), stderr.trim())
        } catch (e: Exception) {
            Log.w(TAG, "Shizuku exec failed: ${e.message}")
            ShellResult(-1, "", "Error ejecutando comando: ${e.message}")
        }
    }
}
