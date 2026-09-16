# ==============================================================================
# Omni Remote Control - Instalador Idempotente para Agente Windows (PowerShell)
# ==============================================================================
#Requires -RunAsAdministrator

param (
    [string]$ServerUrl = "ws://localhost:8090/ws/devices",
    [string]$DeviceName = $env:COMPUTERNAME,
    [string]$InstallDir = "$env:ProgramFiles\OmniAgent",
    [string]$DataDir = "$env:ProgramData\OmniAgent"
)

$ErrorActionPreference = "Stop"

Write-Host "======================================================" -ForegroundColor Cyan
Write-Host "  Omni Remote Control Agent - Instalador para Windows" -ForegroundColor Cyan
Write-Host "======================================================" -ForegroundColor Cyan

# 1. Crear directorios de instalación y datos
Write-Host "[INFO] Verificando directorios..." -ForegroundColor Blue
if (-not (Test-Path $InstallDir)) {
    New-Item -Path $InstallDir -ItemType Directory -Force | Out-Null
    Write-Host "[OK] Directorio de instalacion creado: $InstallDir" -ForegroundColor Green
} else {
    Write-Host "[OK] Directorio de instalacion existente: $InstallDir" -ForegroundColor Green
}

if (-not (Test-Path $DataDir)) {
    New-Item -Path $DataDir -ItemType Directory -Force | Out-Null
    Write-Host "[OK] Directorio de datos creado: $DataDir" -ForegroundColor Green
} else {
    Write-Host "[OK] Directorio de datos existente: $DataDir" -ForegroundColor Green
}

# 2. Generar y persistir Device ID único e inmutable
$DeviceIdFile = Join-Path $DataDir "device-id"
if (-not (Test-Path $DeviceIdFile)) {
    # Intentar obtener UUID de BIOS / Motherboard
    $Csprod = Get-CimInstance -ClassName Win32_ComputerSystemProduct -ErrorAction SilentlyContinue
    if ($Csprod -and $Csprod.UUID) {
        $RawId = ($Csprod.UUID -replace '-', '').ToLower().Substring(0, 32)
        $DeviceId = "windows-$RawId"
    } else {
        $DeviceId = "windows-" + [System.Guid]::NewGuid().ToString("N")
    }
    Set-Content -Path $DeviceIdFile -Value $DeviceId -NoNewline
    Write-Host "[OK] Nuevo Device ID generado: $DeviceId" -ForegroundColor Green
} else {
    $DeviceId = (Get-Content -Path $DeviceIdFile -Raw).Trim()
    Write-Host "[OK] Device ID existente detectado: $DeviceId" -ForegroundColor Green
}

# 3. Archivo de configuración
$ConfigFile = Join-Path $DataDir "config.env"
if (-not (Test-Path $ConfigFile)) {
    $ConfigContent = @"
OMNI_SERVER_URL=$ServerUrl
OMNI_DEVICE_NAME=$DeviceName
OMNI_DEVICE_ID=$DeviceId
OMNI_METRICS_SECONDS=120
OMNI_HEARTBEAT_SECONDS=86400
"@
    Set-Content -Path $ConfigFile -Value $ConfigContent
    Write-Host "[OK] Archivo de configuracion creado en: $ConfigFile" -ForegroundColor Green
} else {
    Write-Host "[OK] Archivo de configuracion existente preservado en: $ConfigFile" -ForegroundColor Green
}

# 4. Copiar o compilar el binario
$CurrentDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$SourceBinary = Join-Path $CurrentDir "omni-agent.exe"
$DestBinary = Join-Path $InstallDir "omni-agent.exe"

if (Test-Path $SourceBinary) {
    Copy-Item -Path $SourceBinary -Destination $DestBinary -Force
    Write-Host "[OK] Binario copiado exitosamente a $DestBinary" -ForegroundColor Green
} elseif (Get-Command go -ErrorAction SilentlyContinue) {
    Write-Host "[INFO] Compilando binario con Go..." -ForegroundColor Blue
    Push-Location $CurrentDir
    go build -ldflags="-w -s" -o $DestBinary cmd/agent/main.go
    Pop-Location
    Write-Host "[OK] Compilacion exitosa en $DestBinary" -ForegroundColor Green
} else {
    if (-not (Test-Path $DestBinary)) {
        Write-Warning "No se encontro omni-agent.exe ni el compilador Go. Por favor copie omni-agent.exe en $InstallDir"
    } else {
        Write-Host "[OK] Binario existente mantenido en $DestBinary" -ForegroundColor Green
    }
}

# 5. Configurar Tarea Programada de Inicio Automático (Scheduled Task)
$TaskName = "OmniAgentService"
Write-Host "[INFO] Configurando ejecucion automatica en el sistema..." -ForegroundColor Blue

$Action = New-ScheduledTaskAction -Execute $DestBinary
$Trigger = New-ScheduledTaskTrigger -AtStartup
$Principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
$Settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -RestartCount 5 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit (New-TimeSpan -Days 365)

# Si la tarea ya existe, actualizarla de forma idempotente
if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}

Register-ScheduledTask -TaskName $TaskName -Action $Action -Trigger $Trigger -Principal $Principal -Settings $Settings | Out-Null
Start-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue

Write-Host "[OK] Tarea programada '$TaskName' creada e iniciada exitosamente." -ForegroundColor Green
Write-Host ""
Write-Host "Instalacion de Omni Agent completada con exito." -ForegroundColor Cyan
Write-Host "Device ID: $DeviceId" -ForegroundColor Yellow
Write-Host "Servidor:  $ServerUrl" -ForegroundColor Yellow
