#!/usr/bin/env bash
# ==============================================================================
# Omni Remote Control - Instalador Idempotente para Agente Linux
# ==============================================================================
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info()  { echo -e "${BLUE}[INFO]${NC} $*"; }
success(){ echo -e "${GREEN}[OK]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# Verificar permisos de root
if [ "$EUID" -ne 0 ]; then
    error "Este script debe ejecutarse con permisos de superusuario (root o sudo)."
fi

# Variables de configuración por defecto
OMNI_SERVER_URL="${OMNI_SERVER_URL:-ws://localhost:8090/ws/devices}"
OMNI_DEVICE_NAME="${OMNI_DEVICE_NAME:-$(hostname)}"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/omni-agent"
DATA_DIR="/var/lib/omni-agent"
LOG_DIR="/var/log/omni-agent"
BINARY_PATH="${INSTALL_DIR}/omni-agent"
SERVICE_FILE="/etc/systemd/system/omni-agent.service"

info "Iniciando instalación/actualización de Omni Agent para Linux..."

# 1. Crear directorios necesarios
mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
chmod 755 "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"

# 2. Instalar dependencias del sistema según el gestor de paquetes
install_dependencies() {
    info "Verificando e instalando dependencias (xdotool, wmctrl, curl, jq, procps)..."
    
    if command -v apt-get >/dev/null 2>&1; then
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -qq || true
        apt-get install -y --no-install-recommends \
            curl \
            jq \
            procps \
            xdotool \
            wmctrl \
            scrot \
            ca-certificates || warn "Algunos paquetes opcionales no pudieron instalarse"
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y curl jq procps-ng xdotool wmctrl scrot ca-certificates || true
    elif command -v yum >/dev/null 2>&1; then
        yum install -y curl jq procps-ng xdotool wmctrl scrot ca-certificates || true
    elif command -v pacman >/dev/null 2>&1; then
        pacman -Sy --noconfirm curl jq procps-ng xdotool wmctrl scrot ca-certificates || true
    elif command -v apk >/dev/null 2>&1; then
        apk add --no-cache curl jq procps xdotool wmctrl scrot ca-certificates || true
    else
        warn "Gestor de paquetes no reconocido. Asegúrate de tener instalados xdotool, wmctrl y scrot."
    fi
}

install_dependencies

# 3. Generar ID único persistente para este dispositivo si no existe
DEVICE_ID_FILE="${CONFIG_DIR}/device-id"
if [ ! -f "$DEVICE_ID_FILE" ]; then
    MACH_ID=""
    if [ -s "/etc/machine-id" ]; then
        MACH_ID=$(head -n 1 /etc/machine-id | tr -d ' \n\r')
    elif [ -s "/var/lib/dbus/machine-id" ]; then
        MACH_ID=$(head -n 1 /var/lib/dbus/machine-id | tr -d ' \n\r')
    fi

    if [ -n "$MACH_ID" ]; then
        echo "linux-${MACH_ID:0:32}" > "$DEVICE_ID_FILE"
    elif [ -f "/proc/sys/kernel/random/uuid" ]; then
        echo "linux-$(cat /proc/sys/kernel/random/uuid | tr -d '-' | cut -c 1-32)" > "$DEVICE_ID_FILE"
    else
        echo "linux-$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')" > "$DEVICE_ID_FILE"
    fi
    chmod 644 "$DEVICE_ID_FILE"
    info "Nuevo Device ID generado: $(cat "$DEVICE_ID_FILE")"
else
    info "Device ID existente detectado: $(cat "$DEVICE_ID_FILE")"
fi

# 4. Archivo de variables de entorno / configuración
ENV_FILE="${CONFIG_DIR}/config.env"
if [ ! -f "$ENV_FILE" ]; then
    cat <<EOF > "$ENV_FILE"
# Configuración de Omni Agent
OMNI_SERVER_URL=${OMNI_SERVER_URL}
OMNI_DEVICE_NAME=${OMNI_DEVICE_NAME}
OMNI_DEVICE_ID=$(cat "$DEVICE_ID_FILE")
OMNI_METRICS_SECONDS=3600
OMNI_HEARTBEAT_SECONDS=86400
DISPLAY=${DISPLAY:-:0}
EOF
    chmod 600 "$ENV_FILE"
    info "Archivo de configuración creado en ${ENV_FILE}"
else
    info "Archivo de configuración existente preservado en ${ENV_FILE}"
fi

# 5. Instalar o compilar binario
# Si se provee un binario local en el mismo directorio del script, se copia
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "${SCRIPT_DIR}/omni-agent" ]; then
    info "Instalando binario local desde ${SCRIPT_DIR}/omni-agent..."
    cp -f "${SCRIPT_DIR}/omni-agent" "$BINARY_PATH"
elif [ -f "${SCRIPT_DIR}/cmd/agent/main.go" ] && command -v go >/dev/null 2>&1; then
    info "Compilando binario con Go..."
    (cd "$SCRIPT_DIR" && go build -ldflags="-w -s" -o "$BINARY_PATH" cmd/agent/main.go)
else
    warn "Binario omni-agent no encontrado en ${SCRIPT_DIR}. Si ya existe en ${BINARY_PATH}, se mantendrá."
    if [ ! -f "$BINARY_PATH" ]; then
        warn "Nota: Copie o compile el binario omni-agent en ${BINARY_PATH}"
    fi
fi

if [ -f "$BINARY_PATH" ]; then
    chmod 755 "$BINARY_PATH"
    success "Binario instalado en ${BINARY_PATH}"
fi

# 6. Configurar servicio systemd si systemd está disponible
if command -v systemctl >/dev/null 2>&1 && [ -d "/run/systemd/system" ]; then
    info "Configurando servicio systemd omni-agent..."
    cat <<EOF > "$SERVICE_FILE"
[Unit]
Description=Omni Remote Control Agent
After=network.target network-online.target graphical.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=${ENV_FILE}
ExecStart=${BINARY_PATH}
Restart=always
RestartSec=5s
StandardOutput=append:${LOG_DIR}/agent.log
StandardError=append:${LOG_DIR}/agent.log

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable omni-agent.service || true
    if [ -f "$BINARY_PATH" ]; then
        systemctl restart omni-agent.service || true
    fi
    success "Servicio systemd configurado y habilitado"
else
    info "systemd no detectado (entorno contenedor o alternativo). Servicio no instalado."
fi

success "Instalación completada exitosamente."
info "Para ejecutar manualmente: OMNI_SERVER_URL=${OMNI_SERVER_URL} ${BINARY_PATH}"
