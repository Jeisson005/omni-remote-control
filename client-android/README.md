# 📱 Omni Remote Control - Cliente Android (Kotlin)

Cliente nativo para dispositivos Android diseñado con arquitectura de ultra bajo consumo de energía (**Doze Mode Friendly**), soporte para telemetría periódica mediante **WorkManager**, despertar remoto a demanda vía **Firebase Cloud Messaging (FCM)** y control táctil/gestual programático mediante **AccessibilityService** y **WebSockets**.

---

## 🏗️ Arquitectura y Componentes

```
                              ┌──────────────────────────────────┐
                              │      Servidor Central Omni       │
                              │ (Go, PostgreSQL, WebSocket Hub)  │
                              └───────┬───────────────────┬──────┘
                                      │                   │
                     POST /telemetry  │                   │ WS /ws/devices
                      (Cada 1 hora)   │                   │ (Bajo demanda)
                                      │                   │
┌─────────────────────────────────────┼───────────────────┼───────────────────────────┐
│ DISPOSITIVO ANDROID                 ▼                   ▼                           │
│                                                                                     │
│ ┌─────────────────────────────┐               ┌──────────────────────────────────┐  │
│ │      TelemetryWorker        │               │      ControlSessionManager       │  │
│ │ (WorkManager, Doze-friendly,│               │    (OkHttp WebSocket Client)     │  │
│ │  BatteryNotLow, 1 hora)     │               └─────────┬──────────────┬─────────┘  │
│ └──────────────┬──────────────┘                         │              │            │
│                │ Recopila:                              ▼              ▼            │
│                │ - Batería % y estado carga     ┌───────────────┐ ┌────────────────┐│
│                │ - Memoria RAM (MemoryInfo)     │  WakeLock &   │ │ Temporizador   ││
│                │ - Red (SSID / Tipo)            │ Screen Waker  │ │ Inactividad 60s││
│                │ - IP Pública y Uptime          └───────────────┘ └────────────────┘│
│                                                         │              ▲            │
│ ┌─────────────────────────────┐                         ▼              │ (resets)   │
│ │  OmniFirebaseMessaging      │ ─── START_CONTROL ───► ┌────────────────┴─────────┐ │
│ │ (FCM Push de alta prioridad)│                        │ OmniAccessibilityService  │ │
│ └─────────────────────────────┘                        │ - dispatchGesture (Click) │ │
│                                                        │ - dispatchGesture (Swipe) │ │
│                                                        │ - ACTION_SET_TEXT (Input) │ │
│                                                        │ - GLOBAL_ACTION (Teclas)  │ │
│                                                        │ - get_tree (Jerarquía UI) │ │
│                                                        └───────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────────────┘
```

---

## ⚡ Estrategia de Batería y Eficiencia

1. **Telemetría Pasiva en Ventanas de Mantenimiento (`TelemetryWorker`) y a Demanda**:
   - Programado como `PeriodicWorkRequest` de **1 hora** (al igual que en Linux y Windows) con ventana flexible de 15 minutos.
   - Aplica restricción `setRequiresBatteryNotLow(true)` y `NetworkType.CONNECTED`.
   - El sistema operativo lo ejecuta de forma agrupada durante las ventanas estándar de mantenimiento de Android sin interrumpir los ciclos profundos de sueño (Doze mode).
   - Entrega los datos mediante HTTP POST a `/api/v1/telemetry`.
   - **A demanda:** Soporta solicitud de telemetría en vivo vía `GET /api/v1/devices/{id}/telemetry?live=true` o MCP (`get_device_telemetry`). Si el dispositivo está dormido, el servidor lo despierta vía FCM de alta prioridad y extrae la medición al instante.

2. **Receptor de Órdenes Dormido (`OmniFirebaseMessagingService`)**:
   - No mantiene conexiones de red abiertas ni consume CPU en reposo.
   - Cuando se requiere interacción en vivo, el servidor envía un mensaje Push FCM con payload `action: "START_CONTROL"`.
   - El servicio solicita un `WakeLock` temporal para encender la pantalla si está apagada y ordena al gestor de sesión conectarse al WebSocket.

3. **Control y Ejecución (`OmniAccessibilityService`)**:
   - Utiliza la API nativa de accesibilidad de Android (`dispatchGesture`) para inyectar toques de precisión, arrastres y deslizamientos (`swipe`).
   - Permite ingresar texto (`ACTION_SET_TEXT`), disparar acciones globales del sistema (`Back`, `Home`, `Recents`, `Notifications`) e inspeccionar el árbol visual (`get_tree`).

4. **Watchdog de Inactividad (60 Segundos)**:
   - Mientras la sesión está activa, cada comando o evento reinicia un temporizador de 60 segundos.
   - Si transcurren 60 segundos sin órdenes del servidor (o si el servidor envía orden de detención `stop_control`), el WebSocket se desconecta, se libera el `WakeLock` y el dispositivo vuelve inmediatamente al estado Doze.

5. **Lectura de Notificaciones (`OmniNotificationListenerService`)**:
   - Componente nativo `NotificationListenerService` que lee en tiempo real título, texto completo, app, paquete, categoría y botones de acción de cualquier aplicación.
   - Responde al comando `get_notifications` con las notificaciones activas y al comando `notification_action` para pulsar botones ("Responder", "Marcar como leído") con soporte de `RemoteInput`.
   - Requiere permiso manual en **Ajustes > Notificaciones > Acceso a notificaciones**.

6. **Interceptor y Gestión de SMS (`SmsReceiver` / `OmniSmsManager`)**:
   - `BroadcastReceiver` con `RECEIVE_SMS` captura SMS entrantes al instante (combinando multiparte) y los reenvía.
   - `READ_SMS` permite extraer el historial de `content://sms/inbox` (comando `get_sms`).
   - `SEND_SMS` permite enviar mensajes (comando `send_sms`).

7. **Reenvío Robusto (`OmniStore` + `ApiClient`)**:
   - Cada evento se guarda en una cola local persistente y se intenta entregar por HTTP POST inmediato.
   - Lo pendiente se vacía al abrir sesión WebSocket (`notifications_sync` / `sms_sync`) o durante la telemetría.

8. **Modos de Control (Consentimiento / Automático)**:
   - `consent` (por defecto): `ConsentNotifier` muestra una notificación con **Permitir/Denegar**; el dispositivo no abre WebSocket hasta que el usuario acepta (`ControlConsentReceiver`).
   - `auto`: control desatendido, opt-in explícito en la app.
   - `allow_remote_mode_change` permite (o no) que el servidor cambie el modo vía comando.

9. **Desbloqueo (`DeviceUnlockManager` + `ShizukuManager` + `DevicePolicyHelper`)**:
   - **Device Owner**: `setKeyguardDisabled` sin credenciales ni Shizuku.
   - **Shizuku**: `locksettings set-disabled`, `input keyevent` (despertar), `input swipe` (patrón) y `pm grant/revoke` (permisos silenciosos).
   - **Patrón**: se guarda cifrado con `EncryptedSharedPreferences` (Android Keystore) y **nunca** sale del dispositivo.
   - Limitación: con identidad `shell`, Shizuku no puede saltar un keyguard seguro por binder; el desbloqueo por `input` es best-effort y puede requerir calibración.

---

## 📦 Compilación y Ejecución

### Requisitos:
- Android Studio Iguana / Jellyfish (o superior) o Android SDK con `commandlinetools`.
- JDK 17.

### 1. Abrir en Android Studio:
Abre la carpeta `client-android` directamente en Android Studio. El proyecto sincronizará automáticamente las dependencias vía Gradle.

### 2. Compilar APK Debug por consola:
```bash
cd client-android
./gradlew assembleDebug
```
El archivo APK generado se ubicará en `app/build/outputs/apk/debug/app-debug.apk`.

### 3. Instalación vía ADB:
```bash
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

### 4. Configuración en el Dispositivo:
1. Abre la aplicación **Omni Agent** en el dispositivo o emulador.
2. Ingresa la URL de tu servidor Omni (ej. `http://192.168.1.50:8090` o `http://10.0.2.2:8090` en emulador).
3. Haz clic en **Habilitar** en la sección *Servicio de Accesibilidad* y activa **Omni Remote Accessibility Service** en Ajustes de Accesibilidad de Android.
4. Haz clic en **Habilitar** en *Acceso a Notificaciones* y activa **Omni Remote Notification Access**.
5. Haz clic en **Solicitar** en *Permisos SMS* y concede `RECEIVE_SMS`, `READ_SMS` y `SEND_SMS`.
6. (Opcional) Excluye la app del ahorro de batería agresivo para garantizar entregas puntuales de FCM.

### 5. FCM (Despertador remoto):
Para habilitar el despertar por push:
1. Coloca tu `google-services.json` en `app/`.
2. Aplica el plugin `com.google.gms.google-services` en `app/build.gradle.kts`.
3. En el servidor, configura `FCM_CREDENTIALS_JSON` y `FCM_PROJECT_ID`.
4. Concede el permiso `POST_NOTIFICATIONS` (Android 13+) para que funcionen el push y el consentimiento.

### 6. Desbloqueo desatendido (opcional):
- **Device Owner (recomendado, sin credenciales):**
  ```bash
  adb shell dpm set-device-owner com.omni.remote/.receivers.OmniDeviceAdminReceiver
  ```
  El dispositivo debe estar recién provisionado y sin cuentas de usuario.
- **Shizuku:** instala la app Shizuku y actívala por ADB/root; luego pulsa **Autorizar** en la sección de Modo de Control.
- **Patrón:** pulsa **Definir** e introduce la secuencia de nodos (1-9). Se guarda cifrado solo en el dispositivo. Puede requerir calibrar `patternCenterY`/`patternScale` según el OEM.
