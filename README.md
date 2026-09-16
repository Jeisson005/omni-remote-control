# Omni Remote Control

Sistema centralizado de alto rendimiento para el control, automatización programática y recolección de telemetría continua en dispositivos cliente multiplataforma (Linux, Windows, Android, etc.).

---

## 🚀 Descripción General

**Omni Remote Control** permite orquestar flotas de dispositivos remotos mediante una arquitectura cliente-servidor basada en **Go**, **PostgreSQL** y conexiones **WebSockets bidireccionales de baja latencia**:

- **Clientes Multiplataforma**: Agentes ligeros y compilados en binarios nativos que reportan telemetría continua (hardware, estado de red, CPU, RAM, procesos, ventanas) y ejecutan órdenes remotas.
- **Control de Terminal y GUI (Linux)**: Ejecución remota de comandos shell y control total de interfaz gráfica (ratón, pulsaciones de teclado, atajos, listar y enfocar ventanas) usando herramientas estándar como `xdotool` y `wmctrl`.
- **Servidor Central (Go + PostgreSQL)**: Gestiona el ciclo de vida de los dispositivos, persistencia de métricas, despacho de órdenes y confirmaciones en tiempo real.
- **API REST y Protocolo MCP (Model Context Protocol)**: Permite que usuarios, sistemas externos y agentes de Inteligencia Artificial (LLMs) inspeccionen telemetría y comanden dispositivos de forma autónoma.

---

## 📊 Estrategia de Telemetría

Para garantizar un monitoreo integral sin saturar la red ni la base de datos, la telemetría se divide en dos niveles:

1. **Telemetría Inicial / Estática (Registro y Heartbeat Diario):**
   - Hostname, distribución Linux (`/etc/os-release`), versión del kernel, arquitectura.
   - Modelo de CPU y cantidad de núcleos (`/proc/cpuinfo`).
   - Memoria RAM total y espacio en disco.
   - Dirección IP local, dirección MAC y zona horaria.
   - Versión del agente.
   
2. **Telemetría Dinámica Periódica (Intervalo por defecto: 1 hora / 3600s, configurable):**
   - Porcentaje de uso de CPU (cálculo delta en tiempo real).
   - Memoria RAM utilizada (MB y porcentaje de ocupación).
   - Porcentaje de uso del disco principal.
   - **Nivel de batería (`battery_pct`)** y estado de carga (`is_charging`).
   - **Nombre de la red actual (`network_name`)**: SSID Wi-Fi (ej. "MiOficina-5G") o interfaz activa (ej. "eth0", "Wi-Fi").
   - **Dirección IP pública (`public_ip`)**: Resolución remota con caché local de 15 minutos.
   - **Tiempo de actividad (`uptime_seconds`)**: Segundos de encendido del sistema operativo.
   - Lista de ventanas activas/visibles con sus IDs y títulos (`wmctrl -l` en Linux, `EnumWindows` en Windows).
   - Top procesos del sistema ordenados por consumo de CPU y memoria (`ps` en Linux, `tasklist` en Windows).

3. **Telemetría en Vivo a Demanda (On-Demand Live Collection):**
   - Permite solicitar en cualquier momento el estado actual del dispositivo en tiempo real.
   - El servidor solicita al agente cliente vía WebSocket que recopile las métricas en ese instante exacto.
   - El resultado se **persiste automáticamente en PostgreSQL** en la tabla `telemetry_metrics` y se **entrega de inmediato en la respuesta HTTP o del MCP tool**.

4. **Eventos de Telemetría y Ciclo de Vida (`device_events`):**
   - `client_started`: Emitido al iniciar el software o encender el agente cliente, reportando uptime, red y versión.
   - `client_stopping`: Emitido al apagar o reiniciar el equipo de forma limpia (capturando `SIGINT`, `SIGTERM` o cierre de servicio).
   - `network_connected` / `network_disconnected`: Reporta desconexiones y recuperaciones de red local y servidor.
   - `network_changed`: Reporta variaciones de red activa (cambio de SSID Wi-Fi o cambio de interfaz/IP).
   - `battery_low`: Emite advertencia cuando la batería cae al 20% o menos en modo descarga.

---

## 📐 Arquitectura del Sistema

```
  ┌─────────────────────────────────────────────────────────────┐
  │                 Dispositivos Clientes                       │
  │  (Linux: omni-agent con xdotool / wmctrl / procps / sysinfo) │
  └──────────────────────────────┬──────────────────────────────┘
                                 │
                                 │ WebSocket Bidireccional (/ws/devices)
                                 ▼
                 ┌───────────────────────────────┐
                 │       Servidor Central        │
                 │          (Golang)             │
                 │   - Hub de Conexiones WS      │
                 │   - Despachador de Comandos   │
                 │   - Ingesta de Telemetría     │
                 └───────┬──────────────┬────────┘
                         │              │
        SQL Queries &    │              │ HTTP REST & MCP Tools
        Persistencia     │              │
                         ▼              ▼
                 ┌──────────────┐   ┌─────────────────────────────┐
                 │  PostgreSQL  │   │     API REST & MCP Tools    │
                 │  (16-alpine) │   │   - GET /api/v1/devices     │
                 └──────────────┘   │   - POST /commands          │
                                    │   - POST /mcp/tools/call    │
                                    └─────────────────────────────┘
```

---

## 📂 Estructura del Repositorio

```text
omni-remote-control/
├── client-linux/                  # Agente para Linux (X11 / Headless)
│   ├── cmd/agent/main.go          # Punto de entrada del agente Linux
│   ├── internal/
│   │   ├── config/                # Configuración y persistencia de Device ID
│   │   ├── sysinfo/               # Telemetría estática (CPU, RAM, kernel, red)
│   │   ├── metrics/               # Telemetría dinámica (CPU%, RAM%, ventanas, procesos)
│   │   ├── executor/              # Ejecutor seguro de comandos shell
│   │   ├── gui/                   # Controlador GUI (xdotool y wmctrl)
│   │   └── connection/            # Cliente WebSocket y reconexión automática
│   ├── install.sh                 # Instalador idempotente (Debian, Ubuntu, Fedora, Arch)
│   ├── Dockerfile.test            # Entorno de pruebas con Xvfb, Openbox y xterm
│   └── entrypoint-test.sh         # Script de inicio para entorno X11 virtual
│
├── client-windows/                # Agente para Windows (Win32 nativo)
│   ├── cmd/agent/main.go          # Punto de entrada del agente Windows
│   ├── internal/
│   │   ├── config/                # Configuración y persistencia en ProgramData
│   │   ├── sysinfo/               # Telemetría estática vía Win32 API
│   │   ├── metrics/               # Telemetría dinámica (GetSystemTimes, EnumWindows, tasklist)
│   │   ├── executor/              # Ejecución de PowerShell y cmd.exe
│   │   ├── win32/                 # Wrappers nativos de user32.dll y kernel32.dll (sin CGO)
│   │   ├── gui/                   # Control GUI (cursor, clics, unicode typing, ventanas)
│   │   └── connection/            # Conexión WebSocket al servidor
│   └── install.ps1                # Instalador idempotente en PowerShell (Scheduled Task)
│
├── server/
│   ├── cmd/server/main.go             # Punto de entrada del servidor Go
│   ├── internal/
│   │   ├── api/                       # Endpoints REST y herramientas MCP
│   │   ├── ws/                        # Hub WebSocket concurrente
│   │   ├── db/                        # Conexión y migraciones automáticas PostgreSQL
│   │   └── models/                    # Modelos de dominio y contratos
│   └── Dockerfile                     # Imagen de producción ligera multi-etapa
│
├── docker-compose.yml                 # Despliegue de Server + PostgreSQL + Cliente Linux de prueba
└── README.md
```

---

## 🚀 Inicio Rápido con Docker

Para levantar el servidor, la base de datos PostgreSQL y un cliente Linux de prueba con entorno gráfico simulado (Xvfb + Openbox):

```bash
docker compose up -d --build
```

### Verificar estado de los contenedores:

```bash
docker compose ps
docker compose logs -f server
```

---

## 🌐 Endpoints de la API REST

### 1. Listar Dispositivos Registrados
```bash
curl -s http://localhost:8090/api/v1/devices | python3 -m json.tool
```

### 2. Consultar Telemetría de un Dispositivo

* **Historial almacenado en Base de Datos:**
```bash
curl -s "http://localhost:8090/api/v1/devices/<DEVICE_ID>/telemetry?limit=5" | python3 -m json.tool
```

* **Telemetría EN VIVO a demanda (recolectada al instante y guardada en BD):**
```bash
# Vía parámetro ?live=true
curl -s "http://localhost:8090/api/v1/devices/<DEVICE_ID>/telemetry?live=true" | python3 -m json.tool

# O directamente vía subruta /telemetry/live
curl -s "http://localhost:8090/api/v1/devices/<DEVICE_ID>/telemetry/live" | python3 -m json.tool
```

### 3. Consultar Eventos de Telemetría y Ciclo de Vida
```bash
# Listar últimos eventos (arranque, apagado, red, batería)
curl -s "http://localhost:8090/api/v1/devices/<DEVICE_ID>/events" | python3 -m json.tool

# Filtrar por tipo de evento
curl -s "http://localhost:8090/api/v1/devices/<DEVICE_ID>/events?type=client_started" | python3 -m json.tool
```

### 4. Ingesta Directa de Telemetría vía HTTP (Android WorkManager)
```bash
curl -s -X POST http://localhost:8090/api/v1/telemetry \
  -H "Content-Type: application/json" \
  -d '{
    "device_id": "android-mi-dispositivo",
    "cpu_usage_pct": 10.2,
    "ram_usage_pct": 45.0,
    "ram_used_bytes": 1800000000,
    "disk_usage_pct": 32.5,
    "battery_pct": 85.0,
    "is_charging": false,
    "network_name": "Wi-Fi (Oficina-5G)",
    "public_ip": "186.144.41.3",
    "uptime_seconds": 36000
  }' | python3 -m json.tool
```

### 5. Ejecutar Comando en Terminal Remota
```bash
curl -s -X POST "http://localhost:8090/api/v1/devices/<DEVICE_ID>/commands" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "shell",
    "payload": {
      "command": "uname -a && uptime"
    }
  }' | python3 -m json.tool
```

### 6. Control de GUI Remota (Ratón, Teclado y Ventanas)

* **Listar ventanas visibles:**
```bash
curl -s -X POST "http://localhost:8090/api/v1/devices/<DEVICE_ID>/commands" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "gui_window",
    "payload": { "sub_action": "list" }
  }' | python3 -m json.tool
```

* **Mover cursor y hacer clic:**
```bash
curl -s -X POST "http://localhost:8090/api/v1/devices/<DEVICE_ID>/commands" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "gui_click",
    "payload": { "x": 400, "y": 300, "button": 1 }
  }' | python3 -m json.tool
```

* **Tipear texto en la ventana activa:**
```bash
curl -s -X POST "http://localhost:8090/api/v1/devices/<DEVICE_ID>/commands" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "gui_type",
    "payload": { "text": "ls -la\n" }
  }' | python3 -m json.tool
```

### 7. Captura de Pantalla a Demanda (Screenshots)

Obtén una captura en tiempo real de la pantalla del cliente (Linux o Windows) directamente como imagen PNG:

* **Descargar o ver imagen PNG directamente:**
```bash
curl -s "http://localhost:8090/api/v1/devices/<DEVICE_ID>/screenshot" -o pantalla.png
```

* **Vía comando genérico (retorna base64):**
```bash
curl -s -X POST "http://localhost:8090/api/v1/devices/<DEVICE_ID>/commands" \
  -H "Content-Type: application/json" \
  -d '{ "type": "gui_screenshot" }' | python3 -m json.tool
```

---

## 🤖 Integración con Model Context Protocol (MCP)

El servidor expone herramientas para agentes de Inteligencia Artificial (Claude, Antigravity, Cursor, etc.):

- `GET /api/v1/mcp/tools`: Lista las herramientas disponibles (`list_devices`, `get_device_telemetry`, `get_device_events`, `execute_shell_command`, `control_gui`, `get_device_screenshot`).
- `POST /api/v1/mcp/tools/call`: Ejecución directa de herramientas por parte de agentes IA.

Ejemplo de llamada MCP (Consultar eventos de dispositivo):
```bash
curl -s -X POST http://localhost:8090/api/v1/mcp/tools/call \
  -H "Content-Type: application/json" \
  -d '{
    "name": "get_device_events",
    "arguments": {
      "device_id": "<DEVICE_ID>",
      "limit": 10
    }
  }' | python3 -m json.tool
```

Ejemplo de llamada MCP (Ejecución remota de comandos):
```bash
curl -s -X POST http://localhost:8090/api/v1/mcp/tools/call \
  -H "Content-Type: application/json" \
  -d '{
    "name": "execute_shell_command",
    "arguments": {
      "device_id": "<DEVICE_ID>",
      "command": "free -h"
    }
  }' | python3 -m json.tool
```

Ejemplo de llamada MCP (Telemetría en vivo a demanda):
```bash
curl -s -X POST http://localhost:8090/api/v1/mcp/tools/call \
  -H "Content-Type: application/json" \
  -d '{
    "name": "get_device_telemetry",
    "arguments": {
      "device_id": "<DEVICE_ID>",
      "live": true
    }
  }' | python3 -m json.tool
```

---

## 📦 Instalación del Agente en Linux (Idempotente)

El script `client-linux/install.sh` instala dependencias, configura el servicio y persiste el identificador del dispositivo:

```bash
sudo OMNI_SERVER_URL="ws://<IP_SERVIDOR>:8090/ws/devices" ./client-linux/install.sh
```

El instalador:
1. Detecta automáticamente el gestor de paquetes de la distribución.
2. Instala utilidades necesarias (`xdotool`, `wmctrl`, `procps`, `curl`, `scrot`).
3. Genera un identificador único y persistente en `/etc/omni-agent/device-id` (a partir de `/etc/machine-id` o DMI UUID).
4. Configura el archivo `/etc/omni-agent/config.env`.
5. Compila/instala el binario en `/usr/local/bin/omni-agent`.
6. Crea, habilita e inicia el servicio persistente `systemd` (`omni-agent.service`) para arranque automático junto con el sistema.

---

## 🪟 Instalación del Agente en Windows (Idempotente)

El agente de Windows está desarrollado en Go utilizando llamadas nativas a la API Win32 (`user32.dll` y `kernel32.dll`), por lo que **no requiere dependencias CGO ni runtimes externos**.

### 1. Compilación Cruzada (desde Linux / CI/CD):
```bash
cd client-windows
GOOS=windows GOARCH=amd64 go build -ldflags="-w -s" -o omni-agent.exe cmd/agent/main.go
```

### 2. Instalación con PowerShell (como Administrador):
```powershell
# Ejecutar en PowerShell con permisos de Administrador:
.\client-windows\install.ps1 -ServerUrl "ws://<IP_SERVIDOR>:8090/ws/devices"
```

El instalador en PowerShell:
1. Crea los directorios en `C:\Program Files\OmniAgent` y `C:\ProgramData\OmniAgent`.
2. Genera y guarda un `device-id` único a partir del UUID de la BIOS/Motherboard.
3. Copia el binario `omni-agent.exe`.
4. Registra e inicia una Tarea Programada de Windows (`Scheduled Task`) para arranque automático en segundo plano con privilegios máximos del sistema.

---

## 📱 Cliente Android (Kotlin / Doze-Friendly)

Ubicado en `client-android/`, desarrollado en **Kotlin nativo** con un enfoque de ultra bajo consumo de batería y sin conexión persistente obligatoria:

1. **Telemetría Pasiva con `WorkManager`**:
   - Tarea periódica cada 15 a 30 minutos restringida por `setRequiresBatteryNotLow(true)`.
   - Recolecta nivel de batería, estado de carga, consumo de RAM, ocupación de almacenamiento, nombre/SSID de la red, IP pública y tiempo de actividad (uptime).
   - Entrega los datos mediante HTTP POST a `/api/v1/telemetry` respetando los ciclos profundos de reposo del sistema (**Doze mode**).

2. **Despertador Remoto con Firebase Cloud Messaging (`FCM`)**:
   - El dispositivo duerme sin conexiones abiertas de WebSocket ni consumo activo de CPU.
   - Al requerirse control en vivo, el backend envía un push FCM de alta prioridad (`action: "START_CONTROL"`).
   - El servicio adquiere un `WakeLock` temporal para encender la pantalla si está apagada y abre la sesión WebSocket.

3. **Control y Ejecución con `AccessibilityService`**:
   - **Clics y Toques**: Ejecución precisa con `dispatchGesture`.
   - **Deslizamientos (`swipe`)**: Gesto continuo con coordenadas de inicio, fin y duración.
   - **Entrada de Texto**: Inyección mediante `ACTION_SET_TEXT` en el nodo de entrada activo o especificado.
   - **Acciones Globales**: Navegación de sistema (`Back`, `Home`, `Recents`, `Notifications`).
   - **Inspección de Pantalla (`get_tree`)**: Extrae el árbol jerárquico de nodos UI visibles para automatizaciones de IA.

4. **Watchdog de Inactividad (60 Segundos)**:
   - Si no se reciben órdenes durante 60 segundos (o si se envía `action: "STOP"`), la sesión WebSocket se desconecta, se libera el `WakeLock` y el dispositivo vuelve al modo Doze.

### Compilación y Despliegue de Android:
```bash
cd client-android
./gradlew assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```
