package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	ServerURL             string        `json:"server_url"`
	DeviceID              string        `json:"device_id"`
	DeviceName            string        `json:"device_name"`
	HeartbeatInterval     time.Duration `json:"heartbeat_interval"`
	MetricsInterval       time.Duration `json:"metrics_interval"`
	AgentVersion          string        `json:"agent_version"`
}

func LoadConfig() *Config {
	serverURL := os.Getenv("OMNI_SERVER_URL")
	if serverURL == "" {
		serverURL = "ws://localhost:8090/ws/devices"
	}

	hostname, _ := os.Hostname()
	deviceName := os.Getenv("OMNI_DEVICE_NAME")
	if deviceName == "" {
		deviceName = hostname
	}

	deviceID := getOrCreateDeviceID()

	heartbeatSec := 24 * 3600 // 24 hours
	if s := os.Getenv("OMNI_HEARTBEAT_SECONDS"); s != "" {
		if d, err := time.ParseDuration(s + "s"); err == nil {
			heartbeatSec = int(d.Seconds())
		}
	}

	metricsSec := 120 // 2 minutes by default
	if s := os.Getenv("OMNI_METRICS_SECONDS"); s != "" {
		if d, err := time.ParseDuration(s + "s"); err == nil {
			metricsSec = int(d.Seconds())
		}
	}

	return &Config{
		ServerURL:         serverURL,
		DeviceID:          deviceID,
		DeviceName:        deviceName,
		HeartbeatInterval: time.Duration(heartbeatSec) * time.Second,
		MetricsInterval:   time.Duration(metricsSec) * time.Second,
		AgentVersion:      "v0.1.0",
	}
}

func getOrCreateDeviceID() string {
	if id := os.Getenv("OMNI_DEVICE_ID"); id != "" {
		return id
	}

	// Paths to check for saved machine ID
	paths := []string{
		"/etc/omni-agent/device-id",
		"/var/lib/omni-agent/device-id",
		"/etc/machine-id",
		"./.omni-device-id",
	}

	for _, p := range paths {
		if data, err := os.ReadFile(p); err == nil {
			id := strings.TrimSpace(string(data))
			if len(id) >= 8 {
				return "linux-" + id[:min(len(id), 32)]
			}
		}
	}

	// Generate a random ID and attempt to persist in ./.omni-device-id
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	generatedID := "linux-" + hex.EncodeToString(buf)

	_ = os.MkdirAll(filepath.Dir("./.omni-device-id"), 0755)
	_ = os.WriteFile("./.omni-device-id", []byte(generatedID), 0644)

	return generatedID
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
