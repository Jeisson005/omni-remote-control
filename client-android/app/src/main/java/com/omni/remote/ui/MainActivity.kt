package com.omni.remote.ui

import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import com.omni.remote.R
import com.omni.remote.data.prefs.PreferencesManager
import com.omni.remote.services.ControlSessionManager
import com.omni.remote.services.OmniAccessibilityService
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
        btnTestSession = findViewById(R.id.btnTestSession)
        btnSendTelemetryNow = findViewById(R.id.btnSendTelemetryNow)

        tvDeviceId.text = prefs.deviceId
        etServerUrl.setText(prefs.serverUrl)

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
    }
}
