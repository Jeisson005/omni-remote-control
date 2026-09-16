package events

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Jeisson005/omni-remote-control/client-windows/internal/win32"
)

type DeviceEvent struct {
	DeviceID  string                 `json:"device_id"`
	EventType string                 `json:"event_type"`
	Severity  string                 `json:"severity"`
	Message   string                 `json:"message"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

type EventListener func(event *DeviceEvent)

type WindowsMonitor struct {
	deviceID      string
	listeners     []EventListener
	listenersMu   sync.RWMutex
	prevIP        string
	prevNet       string
	prevOnline    bool
	batteryWarned bool
	stopChan      chan struct{}
}

func NewMonitor(deviceID string) *WindowsMonitor {
	return &WindowsMonitor{
		deviceID: deviceID,
		stopChan: make(chan struct{}),
	}
}

func (m *WindowsMonitor) AddListener(l EventListener) {
	m.listenersMu.Lock()
	defer m.listenersMu.Unlock()
	m.listeners = append(m.listeners, l)
}

func (m *WindowsMonitor) Emit(eventType, severity, message string, meta map[string]interface{}) {
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

func (m *WindowsMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

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

func (m *WindowsMonitor) Stop() {
	close(m.stopChan)
}

func (m *WindowsMonitor) checkNetwork() {
	currIP, currNet := getActiveNetwork()
	isOnline := (currIP != "")

	if !m.prevOnline && isOnline {
		m.Emit("network_connected", "info", "Dispositivo conectado a la red ("+currNet+" - "+currIP+")", map[string]interface{}{
			"network": currNet,
			"ip":      currIP,
		})
	} else if m.prevOnline && !isOnline {
		m.Emit("network_disconnected", "warning", "Se perdió la conexión de red local en Windows", nil)
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

func (m *WindowsMonitor) checkBattery() {
	var sps win32.SYSTEM_POWER_STATUS
	if !win32.GetSystemPowerStatus(&sps) {
		return
	}

	// 255 significa desconocido o sin batería
	if sps.BatteryLifePercent > 100 {
		return
	}

	pct := float64(sps.BatteryLifePercent)
	isCharging := sps.ACLineStatus == 1

	if pct <= 20 && !isCharging && !m.batteryWarned {
		m.Emit("battery_low", "warning", "Nivel de batería crítico en Windows: "+strconv.FormatFloat(pct, 'f', 1, 64)+"%", map[string]interface{}{
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
