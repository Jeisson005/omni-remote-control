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
   
2. **Telemetría Dinámica y Periódica (Intervalo configurable, ej. 30s - 2min):**
   - Porcentaje de uso de CPU (cálculo delta en tiempo real).
   - Memoria RAM utilizada (MB y porcentaje de ocupación).
   - Porcentaje de uso del disco principal.
   - Lista de ventanas activas/visibles con sus IDs y títulos (`wmctrl -l`).
   - Top procesos del sistema ordenados por consumo de CPU y memoria (`ps`).

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

### 2. Consultar Telemetría Histórica de un Dispositivo
```bash
curl -s "http://localhost:8090/api/v1/devices/<DEVICE_ID>/telemetry?limit=5" | python3 -m json.tool
```

### 3. Ejecutar Comando en Terminal Remota
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

### 4. Control de GUI Remota (Ratón, Teclado y Ventanas)

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

---

## 🤖 Integración con Model Context Protocol (MCP)

El servidor expone herramientas para agentes de Inteligencia Artificial (Claude, Antigravity, Cursor, etc.):

- `GET /api/v1/mcp/tools`: Lista las herramientas disponibles (`list_devices`, `get_device_telemetry`, `execute_shell_command`, `control_gui`).
- `POST /api/v1/mcp/tools/call`: Ejecución directa de herramientas por parte de agentes IA.

Ejemplo de llamada MCP:
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

---

## 📦 Instalación del Agente en Linux (Idempotente)

El script `client-linux/install.sh` instala dependencias, configura el servicio y persiste el identificador del dispositivo:

```bash
sudo OMNI_SERVER_URL="ws://<IP_SERVIDOR>:8090/ws/devices" ./client-linux/install.sh
```

El instalador:
1. Detecta la distribución (Debian, Ubuntu, RHEL, Fedora, Arch, Alpine).
2. Instala automáticamente `xdotool`, `wmctrl`, `curl`, `jq` y `procps`.
3. Crea directorios en `/etc/omni-agent` y `/var/lib/omni-agent`.
4. Genera y preserva un identificador de hardware único para el dispositivo.
5. Configura e inicia el servicio en `systemd` (`omni-agent.service`).

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
