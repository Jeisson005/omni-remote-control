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
                    (Cada 15-30 min)  │                   │ (Bajo demanda)
                                      │                   │
┌─────────────────────────────────────┼───────────────────┼───────────────────────────┐
│ DISPOSITIVO ANDROID                 ▼                   ▼                           │
│                                                                                     │
│ ┌─────────────────────────────┐               ┌──────────────────────────────────┐  │
│ │      TelemetryWorker        │               │      ControlSessionManager       │  │
│ │ (WorkManager, Doze-friendly,│               │    (OkHttp WebSocket Client)     │  │
│ │  BatteryNotLow, 15-30m)     │               └─────────┬──────────────┬─────────┘  │
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

1. **Telemetría Pasiva en Ventanas de Mantenimiento (`TelemetryWorker`)**:
   - Programado como `PeriodicWorkRequest` de 15 a 30 minutos.
   - Aplica restricción `setRequiresBatteryNotLow(true)` y `NetworkType.CONNECTED`.
   - El sistema operativo lo ejecuta de forma agrupada durante las ventanas estándar de mantenimiento de Android sin interrumpir los ciclos profundos de sueño (Doze mode).
   - Entrega los datos mediante HTTP POST a `/api/v1/telemetry`.

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
4. (Opcional) Excluye la app del ahorro de batería agresivo para garantizar entregas puntuales de FCM.
