package com.omni.remote.ui

import android.Manifest
import android.app.admin.DevicePolicyManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import android.widget.Button
import android.widget.CheckBox
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.appcompat.widget.SwitchCompat
import androidx.core.app.ActivityCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import com.omni.remote.R
import com.omni.remote.data.prefs.PreferencesManager
import com.omni.remote.data.security.SecurePrefs
import com.omni.remote.services.ControlSessionManager
import com.omni.remote.services.DevicePolicyHelper
import com.omni.remote.services.DeviceUnlockManager
import com.omni.remote.services.OmniAccessibilityService
import com.omni.remote.services.ShizukuManager
import com.omni.remote.workers.TelemetryWorker

class MainActivity : AppCompatActivity() {

    private lateinit var prefs: PreferencesManager

    private lateinit var tvDeviceId: TextView
    private lateinit var etServerUrl: EditText
    private lateinit var btnSaveServerUrl: Button
    private lateinit var tvAccessibilityStatus: TextView
    private lateinit var btnEnableAccessibility: Button
    private lateinit var tvBatteryOptStatus: TextView
    private lateinit var btnIgnoreBatteryOpt: Button
    private lateinit var tvNotificationAccessStatus: TextView
    private lateinit var btnNotificationAccess: Button
    private lateinit var tvSmsPermissionStatus: TextView
    private lateinit var btnRequestSmsPermissions: Button
    private lateinit var swAutoMode: SwitchCompat
    private lateinit var cbAllowRemoteModeChange: CheckBox
    private lateinit var tvShizukuStatus: TextView
    private lateinit var btnShizukuPermission: Button
    private lateinit var tvDeviceOwnerStatus: TextView
    private lateinit var btnRequestDeviceAdmin: Button
    private lateinit var tvPatternStatus: TextView
    private lateinit var btnSetPattern: Button
    private lateinit var btnClearPattern: Button
    private lateinit var btnTestUnlock: Button
    private lateinit var btnTestSession: Button
    private lateinit var btnSendTelemetryNow: Button

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        prefs = PreferencesManager(this)

        tvDeviceId = findViewById(R.id.tvDeviceId)
        etServerUrl = findViewById(R.id.etServerUrl)
        btnSaveServerUrl = findViewById(R.id.btnSaveServerUrl)
        tvAccessibilityStatus = findViewById(R.id.tvAccessibilityStatus)
        btnEnableAccessibility = findViewById(R.id.btnEnableAccessibility)
        tvBatteryOptStatus = findViewById(R.id.tvBatteryOptStatus)
        btnIgnoreBatteryOpt = findViewById(R.id.btnIgnoreBatteryOpt)
        tvNotificationAccessStatus = findViewById(R.id.tvNotificationAccessStatus)
        btnNotificationAccess = findViewById(R.id.btnNotificationAccess)
        tvSmsPermissionStatus = findViewById(R.id.tvSmsPermissionStatus)
        btnRequestSmsPermissions = findViewById(R.id.btnRequestSmsPermissions)
        swAutoMode = findViewById(R.id.swAutoMode)
        cbAllowRemoteModeChange = findViewById(R.id.cbAllowRemoteModeChange)
        tvShizukuStatus = findViewById(R.id.tvShizukuStatus)
        btnShizukuPermission = findViewById(R.id.btnShizukuPermission)
        tvDeviceOwnerStatus = findViewById(R.id.tvDeviceOwnerStatus)
        btnRequestDeviceAdmin = findViewById(R.id.btnRequestDeviceAdmin)
        tvPatternStatus = findViewById(R.id.tvPatternStatus)
        btnSetPattern = findViewById(R.id.btnSetPattern)
        btnClearPattern = findViewById(R.id.btnClearPattern)
        btnTestUnlock = findViewById(R.id.btnTestUnlock)
        btnTestSession = findViewById(R.id.btnTestSession)
        btnSendTelemetryNow = findViewById(R.id.btnSendTelemetryNow)

        tvDeviceId.text = prefs.deviceId
        etServerUrl.setText(prefs.serverUrl)

        requestPostNotificationsIfNeeded()

        btnSaveServerUrl.setOnClickListener {
            val newUrl = etServerUrl.text.toString().trim()
            if (newUrl.isNotEmpty()) {
                prefs.serverUrl = newUrl
                Toast.makeText(this, "URL guardada: $newUrl", Toast.LENGTH_SHORT).show()
            }
        }

        btnEnableAccessibility.setOnClickListener {
            val intent = Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS)
            startActivity(intent)
        }

        btnIgnoreBatteryOpt.setOnClickListener {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                val intent = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).apply {
                    data = Uri.parse("package:$packageName")
                }
                startActivity(intent)
            }
        }

        btnNotificationAccess.setOnClickListener {
            try {
                startActivity(Intent(Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS))
            } catch (e: Exception) {
                Toast.makeText(this, "No se pudo abrir Ajustes de acceso a notificaciones.", Toast.LENGTH_SHORT).show()
            }
        }

        btnRequestSmsPermissions.setOnClickListener {
            ActivityCompat.requestPermissions(
                this,
                arrayOf(
                    Manifest.permission.RECEIVE_SMS,
                    Manifest.permission.READ_SMS,
                    Manifest.permission.SEND_SMS
                ),
                SMS_PERMISSION_REQUEST_CODE
            )
        }

        btnTestSession.setOnClickListener {
            val sessionMgr = ControlSessionManager.getInstance(this)
            sessionMgr.startSession("manual_button")
            Toast.makeText(this, "Iniciando conexión WebSocket...", Toast.LENGTH_SHORT).show()
        }

        btnSendTelemetryNow.setOnClickListener {
            val workRequest = OneTimeWorkRequestBuilder<TelemetryWorker>().build()
            WorkManager.getInstance(this).enqueue(workRequest)
            Toast.makeText(this, "Recolección y envío de telemetría encolado.", Toast.LENGTH_SHORT).show()
        }

        swAutoMode.isChecked = prefs.controlMode == PreferencesManager.CONTROL_MODE_AUTO
        swAutoMode.setOnCheckedChangeListener { _, isChecked ->
            prefs.controlMode = if (isChecked) PreferencesManager.CONTROL_MODE_AUTO else PreferencesManager.CONTROL_MODE_CONSENT
            if (isChecked) {
                Toast.makeText(
                    this,
                    "Modo AUTOMÁTICO: el control remoto ya no pedirá confirmación.",
                    Toast.LENGTH_LONG
                ).show()
            }
        }

        cbAllowRemoteModeChange.isChecked = prefs.allowRemoteModeChange
        cbAllowRemoteModeChange.setOnCheckedChangeListener { _, isChecked ->
            prefs.allowRemoteModeChange = isChecked
        }

        btnShizukuPermission.setOnClickListener {
            if (!ShizukuManager.isAvailable()) {
                Toast.makeText(this, "Shizuku no está activo. Instálalo y actívalo por ADB/root.", Toast.LENGTH_LONG).show()
                return@setOnClickListener
            }
            ShizukuManager.requestPermission(SHIZUKU_PERMISSION_REQUEST_CODE)
            Toast.makeText(this, "Solicitando permiso a Shizuku...", Toast.LENGTH_SHORT).show()
        }

        btnRequestDeviceAdmin.setOnClickListener {
            val intent = Intent(DevicePolicyManager.ACTION_ADD_DEVICE_ADMIN).apply {
                putExtra(DevicePolicyManager.EXTRA_DEVICE_ADMIN, DevicePolicyHelper.adminComponent(this@MainActivity))
                putExtra(DevicePolicyManager.EXTRA_ADD_EXPLANATION, "Permite deshabilitar el keyguard para control desatendido.")
            }
            @Suppress("DEPRECATION")
            startActivityForResult(intent, DEVICE_ADMIN_REQUEST_CODE)
        }

        btnSetPattern.setOnClickListener { showPatternDialog() }

        btnClearPattern.setOnClickListener {
            SecurePrefs.clearUnlockPattern(this)
            if (prefs.unlockStrategy == PreferencesManager.UNLOCK_STRATEGY_PATTERN) {
                prefs.unlockStrategy = PreferencesManager.UNLOCK_STRATEGY_NONE
            }
            Toast.makeText(this, "Patrón eliminado.", Toast.LENGTH_SHORT).show()
            updateStatus()
        }

        btnTestUnlock.setOnClickListener {
            val result = DeviceUnlockManager.unlock(this)
            Toast.makeText(this, result.message, Toast.LENGTH_LONG).show()
            updateStatus()
        }
    }

    override fun onResume() {
        super.onResume()
        updateStatus()
    }

    private fun updateStatus() {
        // Estado del servicio de accesibilidad
        val isServiceActive = OmniAccessibilityService.instance != null
        if (isServiceActive) {
            tvAccessibilityStatus.text = "Servicio de Accesibilidad: ACTIVO"
            tvAccessibilityStatus.setTextColor(Color.parseColor("#4CAF50"))
            btnEnableAccessibility.isEnabled = false
        } else {
            tvAccessibilityStatus.text = "Servicio de Accesibilidad: INACTIVO"
            tvAccessibilityStatus.setTextColor(Color.parseColor("#FF5252"))
            btnEnableAccessibility.isEnabled = true
        }

        // Estado de optimización de batería
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
            val isIgnoring = pm.isIgnoringBatteryOptimizations(packageName)
            if (isIgnoring) {
                tvBatteryOptStatus.text = "Batería: Excluido de ahorro (óptimo)"
                tvBatteryOptStatus.setTextColor(Color.parseColor("#4CAF50"))
                btnIgnoreBatteryOpt.isEnabled = false
            } else {
                tvBatteryOptStatus.text = "Batería: Ahorro activo (restringido)"
                tvBatteryOptStatus.setTextColor(Color.parseColor("#FFA000"))
                btnIgnoreBatteryOpt.isEnabled = true
            }
        }

        // Estado del acceso a notificaciones (NotificationListenerService)
        val hasNotificationAccess = NotificationManagerCompat
            .getEnabledListenerPackages(this)
            .contains(packageName)
        if (hasNotificationAccess) {
            tvNotificationAccessStatus.text = "Acceso a Notificaciones: ACTIVO"
            tvNotificationAccessStatus.setTextColor(Color.parseColor("#4CAF50"))
            btnNotificationAccess.isEnabled = false
        } else {
            tvNotificationAccessStatus.text = "Acceso a Notificaciones: INACTIVO"
            tvNotificationAccessStatus.setTextColor(Color.parseColor("#FF5252"))
            btnNotificationAccess.isEnabled = true
        }

        // Estado de los permisos de SMS
        val hasSmsPermissions = hasPermission(Manifest.permission.RECEIVE_SMS) &&
                hasPermission(Manifest.permission.READ_SMS) &&
                hasPermission(Manifest.permission.SEND_SMS)
        if (hasSmsPermissions) {
            tvSmsPermissionStatus.text = "Permisos SMS: ACTIVOS"
            tvSmsPermissionStatus.setTextColor(Color.parseColor("#4CAF50"))
            btnRequestSmsPermissions.isEnabled = false
        } else {
            tvSmsPermissionStatus.text = "Permisos SMS: INACTIVOS"
            tvSmsPermissionStatus.setTextColor(Color.parseColor("#FF5252"))
            btnRequestSmsPermissions.isEnabled = true
        }

        // Estado de Shizuku
        when {
            !ShizukuManager.isAvailable() -> {
                tvShizukuStatus.text = "Shizuku: no disponible"
                tvShizukuStatus.setTextColor(Color.parseColor("#FF5252"))
                btnShizukuPermission.isEnabled = true
                btnShizukuPermission.text = "Autorizar"
            }
            ShizukuManager.hasPermission() -> {
                val mode = if (ShizukuManager.isRoot()) "root" else "shell"
                tvShizukuStatus.text = "Shizuku: ACTIVO ($mode)"
                tvShizukuStatus.setTextColor(Color.parseColor("#4CAF50"))
                btnShizukuPermission.isEnabled = false
                btnShizukuPermission.text = "Listo"
            }
            else -> {
                tvShizukuStatus.text = "Shizuku: sin permiso"
                tvShizukuStatus.setTextColor(Color.parseColor("#FFA000"))
                btnShizukuPermission.isEnabled = true
                btnShizukuPermission.text = "Autorizar"
            }
        }

        // Estado de Device Owner
        val isOwner = DevicePolicyHelper.isDeviceOwner(this)
        val isAdmin = DevicePolicyHelper.isAdminActive(this)
        when {
            isOwner -> {
                tvDeviceOwnerStatus.text = "Device Owner: SÍ (keyguard gestionable)"
                tvDeviceOwnerStatus.setTextColor(Color.parseColor("#4CAF50"))
                btnRequestDeviceAdmin.isEnabled = false
            }
            isAdmin -> {
                tvDeviceOwnerStatus.text = "Device Admin: SÍ (sin poder de keyguard)"
                tvDeviceOwnerStatus.setTextColor(Color.parseColor("#FFA000"))
                btnRequestDeviceAdmin.isEnabled = false
            }
            else -> {
                tvDeviceOwnerStatus.text = "Device Owner/Admin: no"
                tvDeviceOwnerStatus.setTextColor(Color.parseColor("#FF5252"))
                btnRequestDeviceAdmin.isEnabled = true
            }
        }

        // Estado del patrón
        val hasPattern = !SecurePrefs.getUnlockPattern(this).isNullOrBlank()
        if (hasPattern) {
            tvPatternStatus.text = "Patrón guardado: SÍ (estrategia: ${prefs.unlockStrategy})"
            tvPatternStatus.setTextColor(Color.parseColor("#4CAF50"))
            btnClearPattern.isEnabled = true
        } else {
            tvPatternStatus.text = "Patrón guardado: no (estrategia: ${prefs.unlockStrategy})"
            tvPatternStatus.setTextColor(Color.parseColor("#AAAAAA"))
            btnClearPattern.isEnabled = false
        }
    }

    private fun showPatternDialog() {
        if (!SecurePrefs.isAvailable(this)) {
            Toast.makeText(this, "El almacenamiento cifrado no está disponible en este dispositivo.", Toast.LENGTH_LONG).show()
            return
        }

        val input = EditText(this).apply {
            hint = "Secuencia de nodos 1-9 (ej. 1478)"
            inputType = android.text.InputType.TYPE_CLASS_NUMBER
        }

        AlertDialog.Builder(this)
            .setTitle("Definir patrón de desbloqueo")
            .setMessage("Introduce la secuencia de nodos del patrón (1-9 en orden de lectura). " +
                    "Se guardará cifrado en este dispositivo y nunca se enviará al servidor.")
            .setView(input)
            .setPositiveButton("Guardar") { _, _ ->
                val pattern = input.text.toString().trim().filter { it in '1'..'9' }
                if (pattern.length < 2) {
                    Toast.makeText(this, "Patrón inválido (mínimo 2 nodos).", Toast.LENGTH_SHORT).show()
                    return@setPositiveButton
                }
                SecurePrefs.setUnlockPattern(this, pattern)
                prefs.unlockStrategy = PreferencesManager.UNLOCK_STRATEGY_PATTERN
                Toast.makeText(this, "Patrón guardado cifrado.", Toast.LENGTH_SHORT).show()
                updateStatus()
            }
            .setNegativeButton("Cancelar", null)
            .show()
    }

    private fun requestPostNotificationsIfNeeded() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            !hasPermission(Manifest.permission.POST_NOTIFICATIONS)
        ) {
            ActivityCompat.requestPermissions(
                this,
                arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                NOTIFICATIONS_REQUEST_CODE
            )
        }
    }

    private fun hasPermission(permission: String): Boolean {
        return ContextCompat.checkSelfPermission(this, permission) == PackageManager.PERMISSION_GRANTED
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == DEVICE_ADMIN_REQUEST_CODE) {
            updateStatus()
        }
    }

    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == SMS_PERMISSION_REQUEST_CODE) {
            val granted = grantResults.isNotEmpty() && grantResults.all { it == PackageManager.PERMISSION_GRANTED }
            val message = if (granted) {
                "Permisos SMS concedidos."
            } else {
                "Permisos SMS incompletos. Habilítalos manualmente en Ajustes."
            }
            Toast.makeText(this, message, Toast.LENGTH_SHORT).show()
            updateStatus()
        }
    }

    companion object {
        private const val SMS_PERMISSION_REQUEST_CODE = 7042
        private const val SHIZUKU_PERMISSION_REQUEST_CODE = 7043
        private const val DEVICE_ADMIN_REQUEST_CODE = 7044
        private const val NOTIFICATIONS_REQUEST_CODE = 7045
    }
}
