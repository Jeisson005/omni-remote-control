package models

import (
	"encoding/json"
	"time"
)

type Device struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Hostname    string            `json:"hostname"`
	OS          string            `json:"os"`
	Platform    string            `json:"platform"`
	Status      string            `json:"status"` // "online", "offline"
	FirstSeenAt time.Time         `json:"first_seen_at"`
	LastSeenAt  time.Time         `json:"last_seen_at"`
	SystemInfo  *DeviceSystemInfo `json:"system_info,omitempty"`
}

type DeviceSystemInfo struct {
	DeviceID       string    `json:"device_id"`
	CPUModel       string    `json:"cpu_model"`
	CPUCores       int       `json:"cpu_cores"`
	RAMTotalBytes  uint64    `json:"ram_total_bytes"`
	DiskTotalBytes uint64    `json:"disk_total_bytes"`
	OSVersion      string    `json:"os_version"`
	KernelVersion  string    `json:"kernel_version"`
	Arch           string    `json:"arch"`
	IPAddress      string    `json:"ip_address"`
	MACAddress     string    `json:"mac_address"`
	Timezone       string    `json:"timezone"`
	AgentVersion   string    `json:"agent_version"`
	PublicIP       string    `json:"public_ip,omitempty"`
	NetworkName    string    `json:"network_name,omitempty"`
	UptimeSeconds  uint64    `json:"uptime_seconds,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type WindowInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Class string `json:"class,omitempty"`
}

type ProcessInfo struct {
	PID    int     `json:"pid"`
	Name   string  `json:"name"`
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
}

type TelemetryMetric struct {
	ID            int64         `json:"id,omitempty"`
	DeviceID      string        `json:"device_id"`
	CPUUsagePct   float64       `json:"cpu_usage_pct"`
	RAMUsagePct   float64       `json:"ram_usage_pct"`
	RAMUsedBytes  uint64        `json:"ram_used_bytes"`
	DiskUsagePct  float64       `json:"disk_usage_pct"`
	BatteryPct    *float64      `json:"battery_pct,omitempty"`
	IsCharging    *bool         `json:"is_charging,omitempty"`
	NetworkName   string        `json:"network_name,omitempty"`
	PublicIP      string        `json:"public_ip,omitempty"`
	UptimeSeconds uint64        `json:"uptime_seconds,omitempty"`
	OpenWindows   []WindowInfo  `json:"open_windows"`
	TopProcesses  []ProcessInfo `json:"top_processes"`
	RecordedAt    time.Time     `json:"recorded_at"`
}

type DeviceEvent struct {
	ID        int64                  `json:"id,omitempty"`
	DeviceID  string                 `json:"device_id"`
	EventType string                 `json:"event_type"` // "client_started", "client_stopping", "network_connected", "network_disconnected", "network_changed", "battery_low", etc.
	Severity  string                 `json:"severity"`   // "info", "warning", "error"
	Message   string                 `json:"message"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

type Command struct {
	ID          string                 `json:"id"`
	DeviceID    string                 `json:"device_id"`
	Type        string                 `json:"type"` // "shell", "gui_click", "gui_move", "gui_type", "gui_key", "gui_window", "gui_screenshot", "collect_telemetry"
	Payload     map[string]interface{} `json:"payload"`
	Status      string                 `json:"status"` // "pending", "sent", "running", "completed", "failed", "timeout"
	ExitCode    int                    `json:"exit_code"`
	Output      string                 `json:"output"`
	Error       string                 `json:"error,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
}

type WSMessage struct {
	Type      string          `json:"type"` // "register", "telemetry", "event", "command_request", "command_response", "ping", "pong"
	DeviceID  string          `json:"device_id,omitempty"`
	CommandID string          `json:"command_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}
