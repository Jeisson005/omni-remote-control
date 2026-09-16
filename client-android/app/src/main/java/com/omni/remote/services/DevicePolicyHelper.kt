package com.omni.remote.services

import android.app.admin.DevicePolicyManager
import android.content.ComponentName
import android.content.Context
import android.util.Log
import com.omni.remote.receivers.OmniDeviceAdminReceiver
import com.omni.remote.util.OpResult

/**
 * Integración con DevicePolicyManager.
 *
 * Cuando la app es **Device Owner** puede deshabilitar el keyguard por completo
 * (`setKeyguardDisabled`) sin Shizuku y sin almacenar ningún PIN/patrón. Esta es
 * la vía más limpia para control desatendido en dispositivos provisionados.
 */
object DevicePolicyHelper {

    private const val TAG = "DevicePolicyHelper"

    fun adminComponent(context: Context): ComponentName =
        ComponentName(context.applicationContext, OmniDeviceAdminReceiver::class.java)

    private fun dpm(context: Context): DevicePolicyManager =
        context.getSystemService(Context.DEVICE_POLICY_SERVICE) as DevicePolicyManager

    fun isDeviceOwner(context: Context): Boolean {
        return try {
            dpm(context).isDeviceOwnerApp(context.packageName)
        } catch (e: Exception) {
            false
        }
    }

    fun isAdminActive(context: Context): Boolean {
        return try {
            dpm(context).isAdminActive(adminComponent(context))
        } catch (e: Exception) {
            false
        }
    }

    fun setKeyguardDisabled(context: Context, disabled: Boolean): OpResult {
        if (!isDeviceOwner(context)) {
            return OpResult(false, "La app no es Device Owner; se requiere provisionar el dispositivo")
        }
        return try {
            dpm(context).setKeyguardDisabled(adminComponent(context), disabled)
            OpResult(true, if (disabled) "Keyguard deshabilitado" else "Keyguard restaurado")
        } catch (e: Exception) {
            Log.w(TAG, "setKeyguardDisabled failed: ${e.message}")
            OpResult(false, "Error al cambiar el keyguard: ${e.message}")
        }
    }
}
