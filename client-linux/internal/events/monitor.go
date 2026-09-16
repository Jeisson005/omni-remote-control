package events

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DeviceEvent struct {
	DeviceID  string                 `json:"device_id"`
	EventType string                 `json:"event_type"` // client_started, client_stopping, network_connected, network_disconnected, network_changed, battery_low
	Severity  string                 `json:"severity"`   // info, warning, error
	Message   string                 `json:"message"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

type EventListener func(event *DeviceEvent)

type Monitor struct {
	deviceID      string
	listeners     []EventListener
	listenersMu   sync.RWMutex
	prevIP        string
	prevNet       string
	prevOnline    bool
	batteryWarned bool
	stopChan      chan struct{}
}

func NewMonitor(deviceID string) *Monitor {
	return &Monitor{
		deviceID: deviceID,
		stopChan: make(chan struct{}),
	}
}

func (m *Monitor) AddListener(l EventListener) {
	m.listenersMu.Lock()
	defer m.listenersMu.Unlock()
	m.listeners = append(m.listeners, l)
}

func (m *Monitor) Emit(eventType, severity, message string, meta map[string]interface{}) {
	ev := &DeviceEvent{
		DeviceID:  m.deviceID,
		EventType: eventType,
		Severity:  severity,
		Message:   message,
		Metadata:  meta,
		CreatedAt: time.Now().UTC(),
	}

	m.listenersMu.RLock()
	defer m.listenersMu.RUnlock()
	for _, l := range m.listeners {
		go l(ev)
	}
}

func (m *Monitor) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Initial network check
	currIP, currNet := getActiveNetwork()
	m.prevIP = currIP
	m.prevNet = currNet
	m.prevOnline = (currIP != "")

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.checkNetwork()
			m.checkBattery()
		}
	}
}

func (m *Monitor) Stop() {
	close(m.stopChan)
}

func (m *Monitor) checkNetwork() {
	currIP, currNet := getActiveNetwork()
	isOnline := (currIP != "")

	if !m.prevOnline && isOnline {
		m.Emit("network_connected", "info", "Dispositivo conectado a la red ("+currNet+" - "+currIP+")", map[string]interface{}{
			"network": currNet,
			"ip":      currIP,
		})
	} else if m.prevOnline && !isOnline {
		m.Emit("network_disconnected", "warning", "Se perdió la conexión de red local", nil)
	} else if isOnline && (currIP != m.prevIP || currNet != m.prevNet) {
		m.Emit("network_changed", "info", "Cambio de red detectado: "+m.prevNet+" -> "+currNet, map[string]interface{}{
			"previous_network": m.prevNet,
			"new_network":      currNet,
			"previous_ip":      m.prevIP,
			"new_ip":           currIP,
		})
	}

	m.prevIP = currIP
	m.prevNet = currNet
	m.prevOnline = isOnline
}

func (m *Monitor) checkBattery() {
	pct, isCharging, hasBattery := getBatteryInfo()
	if !hasBattery {
		return
	}

	if pct <= 20 && !isCharging && !m.batteryWarned {
		m.Emit("battery_low", "warning", "Nivel de batería crítico: "+strconv.FormatFloat(pct, 'f', 1, 64)+"%", map[string]interface{}{
			"battery_pct": pct,
			"is_charging": isCharging,
		})
		m.batteryWarned = true
	} else if pct > 25 || isCharging {
		m.batteryWarned = false
	}
}

func getActiveNetwork() (ip string, netName string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", "none"
	}

	for _, iface := range ifaces {
		if (iface.Flags&net.FlagUp) == 0 || (iface.Flags&net.FlagLoopback) != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ipNet *net.IPNet
			switch v := addr.(type) {
			case *net.IPNet:
				ipNet = v
			case *net.IPAddr:
				ipNet = &net.IPNet{IP: v.IP, Mask: net.CIDRMask(32, 32)}
			}
			if ipNet != nil && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
				return ipNet.IP.String(), iface.Name
			}
		}
	}
	return "", "disconnected"
}

func getBatteryInfo() (pct float64, isCharging bool, hasBattery bool) {
	entries, err := os.ReadDir("/sys/class/power_supply")
	if err != nil {
		return 0, true, false
	}

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "BAT") {
			capBytes, err := os.ReadFile("/sys/class/power_supply/" + name + "/capacity")
			if err == nil {
				p, _ := strconv.ParseFloat(strings.TrimSpace(string(capBytes)), 64)
				statusBytes, _ := os.ReadFile("/sys/class/power_supply/" + name + "/status")
				status := strings.TrimSpace(string(statusBytes))
				charging := (status == "Charging" || status == "Full")
				return p, charging, true
			}
		}
	}
	return 0, true, false
}
