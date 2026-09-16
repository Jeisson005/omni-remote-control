package com.omni.remote.receivers

import android.app.admin.DeviceAdminReceiver

/**
 * Receptor de administración de dispositivo. Si la app es promovida a
 * **Device Owner** (vía `adb shell dpm set-device-owner ...` o NFC), permite
 * deshabilitar el keyguard sin depender de Shizuku ni guardar credenciales.
 */
class OmniDeviceAdminReceiver : DeviceAdminReceiver()
