package metrics

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jeisson005/omni-remote-control/client-windows/internal/win32"
)

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

type WindowsCollector struct {
	prevIdle   uint64
	prevKernel uint64
	prevUser   uint64
	mu         sync.Mutex
}

func NewCollector() *WindowsCollector {
	c := &WindowsCollector{}
	idle, kernel, user, _ := win32.GetSystemCPUtimes()
	c.prevIdle = idle
	c.prevKernel = kernel
	c.prevUser = user
	return c
}

func (c *WindowsCollector) Collect(deviceID string) *TelemetryMetric {
	cpuPct := c.calculateCPUUsage()
	ramPct, ramUsed := getRAMUsage()
	diskPct := getDiskUsage()
	windows := getOpenWindows()
	processes := getTopProcesses()
	batPct, isCharging := getBattery()
	netName := getNetworkName()
	pubIP := getPublicIPCached()
	uptime := getUptimeSeconds()

	return &TelemetryMetric{
		DeviceID:      deviceID,
		CPUUsagePct:   round(cpuPct, 2),
		RAMUsagePct:   round(ramPct, 2),
		RAMUsedBytes:  ramUsed,
		DiskUsagePct:  round(diskPct, 2),
		BatteryPct:    batPct,
		IsCharging:    isCharging,
		NetworkName:   netName,
		PublicIP:      pubIP,
		UptimeSeconds: uptime,
		OpenWindows:   windows,
		TopProcesses:  processes,
		RecordedAt:    time.Now().UTC(),
	}
}

func (c *WindowsCollector) calculateCPUUsage() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	idle, kernel, user, err := win32.GetSystemCPUtimes()
	if err != nil {
		return 0.0
	}

	idleDelta := idle - c.prevIdle
	kernelDelta := kernel - c.prevKernel
	userDelta := user - c.prevUser

	c.prevIdle = idle
	c.prevKernel = kernel
	c.prevUser = user

	// En Windows, kernel time incluye idle time
	totalSys := kernelDelta + userDelta
	if totalSys == 0 {
		return 0.0
	}

	if totalSys < idleDelta {
		return 0.0
	}

	busy := totalSys - idleDelta
	usage := (float64(busy) / float64(totalSys)) * 100.0
	if usage < 0 {
		return 0.0
	}
	if usage > 100 {
		return 100.0
	}
	return usage
}

func getRAMUsage() (pct float64, usedBytes uint64) {
	mem, err := win32.GetMemoryStatus()
	if err != nil || mem.TotalPhys == 0 {
		return 0.0, 0
	}

	usedBytes = mem.TotalPhys - mem.AvailPhys
	pct = (float64(usedBytes) / float64(mem.TotalPhys)) * 100.0
	return pct, usedBytes
}

func getDiskUsage() float64 {
	systemDrive := os.Getenv("SystemDrive")
	if systemDrive == "" {
		systemDrive = "C:"
	}

	total, free, err := win32.GetDiskSpaceInfo(systemDrive + "\\")
	if err != nil || total == 0 {
		return 0.0
	}

	used := total - free
	return (float64(used) / float64(total)) * 100.0
}

func getOpenWindows() []WindowInfo {
	var list []WindowInfo
	windows, err := win32.EnumDesktopWindows()
	if err != nil {
		return list
	}

	for _, w := range windows {
		list = append(list, WindowInfo{
			ID:    fmt.Sprintf("0x%08X", w.HWND),
			Title: w.Title,
		})
	}
	return list
}

func getTopProcesses() []ProcessInfo {
	var procs []ProcessInfo

	// Ejecutar tasklist en formato CSV
	cmd := exec.Command("tasklist.exe", "/FO", "CSV", "/NH")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return procs
	}

	reader := csv.NewReader(&out)
	records, err := reader.ReadAll()
	if err != nil {
		return procs
	}

	count := 0
	for _, rec := range records {
		if len(rec) >= 5 {
			name := rec[0]
			pid, _ := strconv.Atoi(rec[1])
			memStr := strings.ReplaceAll(rec[4], " K", "")
			memStr = strings.ReplaceAll(memStr, ",", "")
			memStr = strings.ReplaceAll(memStr, ".", "")
			memKb, _ := strconv.ParseFloat(memStr, 64)

			procs = append(procs, ProcessInfo{
				PID:    pid,
				Name:   name,
				Memory: memKb * 1024,
			})
			count++
			if count >= 6 {
				break
			}
		}
	}

	return procs
}

func round(val float64, precision int) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

var (
	cachedWinPublicIP     string
	cachedWinPublicIPTime time.Time
	winPublicIPMu         sync.Mutex
)

func getBattery() (*float64, *bool) {
	var sps win32.SYSTEM_POWER_STATUS
	if !win32.GetSystemPowerStatus(&sps) {
		return nil, nil
	}

	if sps.BatteryLifePercent > 100 {
		isOnline := sps.ACLineStatus == 1
		return nil, &isOnline
	}

	pct := float64(sps.BatteryLifePercent)
	isCharging := sps.ACLineStatus == 1
	return &pct, &isCharging
}

func getNetworkName() string {
	// Intentar obtener SSID Wi-Fi con netsh
	cmd := exec.Command("netsh", "wlan", "show", "interfaces")
	out, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "SSID") && !strings.HasPrefix(trimmed, "BSSID") {
				parts := strings.Split(trimmed, ":")
				if len(parts) > 1 {
					ssid := strings.TrimSpace(parts[1])
					if ssid != "" {
						return ssid
					}
				}
			}
		}
	}

	// Fallback a interfaz de red activa
	ifaces, err := net.Interfaces()
	if err == nil {
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
					return iface.Name
				}
			}
		}
	}
	return "Ethernet"
}

func getPublicIPCached() string {
	winPublicIPMu.Lock()
	defer winPublicIPMu.Unlock()

	if cachedWinPublicIP != "" && time.Since(cachedWinPublicIPTime) < 15*time.Minute {
		return cachedWinPublicIP
	}

	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("https://api.ipify.org")
	if err == nil {
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			ipStr := strings.TrimSpace(string(body))
			if ipStr != "" {
				cachedWinPublicIP = ipStr
				cachedWinPublicIPTime = time.Now()
				return cachedWinPublicIP
			}
		}
	}

	return cachedWinPublicIP
}

func getUptimeSeconds() uint64 {
	return win32.GetTickCount64() / 1000
}
