package metrics

import (
	"bufio"
	"bytes"
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
	"syscall"
	"time"
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

type Collector struct {
	prevIdle  uint64
	prevTotal uint64
	mu        sync.Mutex
}

func NewCollector() *Collector {
	c := &Collector{}
	c.prevIdle, c.prevTotal = readCPUTimes()
	return c
}

func (c *Collector) Collect(deviceID string) *TelemetryMetric {
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

func readCPUTimes() (idle, total uint64) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)[1:]
			var times []uint64
			for _, f := range fields {
				val, _ := strconv.ParseUint(f, 10, 64)
				times = append(times, val)
				total += val
			}
			if len(times) >= 4 {
				idle = times[3] // idle is the 4th field
				if len(times) >= 5 {
					idle += times[4] // iowait
				}
			}
			break
		}
	}
	return idle, total
}

func (c *Collector) calculateCPUUsage() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	currIdle, currTotal := readCPUTimes()
	if currTotal <= c.prevTotal {
		return 0.0
	}

	idleDelta := float64(currIdle - c.prevIdle)
	totalDelta := float64(currTotal - c.prevTotal)

	c.prevIdle = currIdle
	c.prevTotal = currTotal

	if totalDelta <= 0 {
		return 0.0
	}

	usage := 100.0 * (1.0 - (idleDelta / totalDelta))
	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}
	return usage
}

func getRAMUsage() (pct float64, usedBytes uint64) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer file.Close()

	var memTotal, memAvailable uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				memTotal, _ = strconv.ParseUint(parts[1], 10, 64)
			}
		} else if strings.HasPrefix(line, "MemAvailable:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				memAvailable, _ = strconv.ParseUint(parts[1], 10, 64)
			}
		}
	}

	if memTotal == 0 {
		return 0, 0
	}

	memTotalBytes := memTotal * 1024
	memAvailBytes := memAvailable * 1024
	usedBytes = memTotalBytes - memAvailBytes
	pct = (float64(usedBytes) / float64(memTotalBytes)) * 100.0

	return pct, usedBytes
}

func getDiskUsage() float64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0.0
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	if total == 0 {
		return 0.0
	}

	used := total - free
	return (float64(used) / float64(total)) * 100.0
}

func getOpenWindows() []WindowInfo {
	var windows []WindowInfo

	// Check if DISPLAY is configured or default to :0 / :99
	display := os.Getenv("DISPLAY")
	if display == "" {
		return windows
	}

	// Try wmctrl -l
	cmd := exec.Command("wmctrl", "-l")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return windows
	}

	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: 0x01600003  0 hostname Window Title
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			winID := parts[0]
			title := strings.Join(parts[3:], " ")
			windows = append(windows, WindowInfo{
				ID:    winID,
				Title: title,
			})
		}
	}

	return windows
}

func getTopProcesses() []ProcessInfo {
	var procs []ProcessInfo

	// Run: ps -eo pid,pcpu,pmem,comm --sort=-pcpu | head -n 6
	cmd := exec.Command("sh", "-c", "ps -eo pid,pcpu,pmem,comm --sort=-pcpu | head -n 6")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return procs
	}

	scanner := bufio.NewScanner(&out)
	// Skip header
	if scanner.Scan() {
		_ = scanner.Text()
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 4 {
			pid, _ := strconv.Atoi(fields[0])
			cpu, _ := strconv.ParseFloat(fields[1], 64)
			mem, _ := strconv.ParseFloat(fields[2], 64)
			name := strings.Join(fields[3:], " ")

			procs = append(procs, ProcessInfo{
				PID:    pid,
				Name:   name,
				CPU:    cpu,
				Memory: mem,
			})
		}
	}

	return procs
}

func round(val float64, precision int) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

func fmtKB(kb uint64) string {
	return fmt.Sprintf("%d KB", kb)
}

var (
	cachedPublicIP     string
	cachedPublicIPTime time.Time
	publicIPMu         sync.Mutex
)

func getBattery() (*float64, *bool) {
	entries, err := os.ReadDir("/sys/class/power_supply")
	if err != nil {
		return nil, nil
	}

	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "BAT") {
			capBytes, err := os.ReadFile("/sys/class/power_supply/" + name + "/capacity")
			if err == nil {
				pct, _ := strconv.ParseFloat(strings.TrimSpace(string(capBytes)), 64)
				statusBytes, _ := os.ReadFile("/sys/class/power_supply/" + name + "/status")
				status := strings.TrimSpace(string(statusBytes))
				charging := (status == "Charging" || status == "Full")
				return &pct, &charging
			}
		}
	}

	// Si no hay batería, verificar si está con cable de alimentación AC
	acOnline, err := os.ReadFile("/sys/class/power_supply/AC/online")
	if err == nil {
		isOnline := strings.TrimSpace(string(acOnline)) == "1"
		return nil, &isOnline
	}

	return nil, nil
}

func getNetworkName() string {
	// Intentar obtener SSID Wi-Fi
	out, err := exec.Command("iwgetid", "-r").Output()
	if err == nil {
		ssid := strings.TrimSpace(string(out))
		if ssid != "" {
			return ssid
		}
	}

	// Fallback a interfaz activa con IP
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
	return "eth0"
}

func getPublicIPCached() string {
	publicIPMu.Lock()
	defer publicIPMu.Unlock()

	if cachedPublicIP != "" && time.Since(cachedPublicIPTime) < 15*time.Minute {
		return cachedPublicIP
	}

	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("https://api.ipify.org")
	if err == nil {
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			ipStr := strings.TrimSpace(string(body))
			if ipStr != "" {
				cachedPublicIP = ipStr
				cachedPublicIPTime = time.Now()
				return cachedPublicIP
			}
		}
	}

	return cachedPublicIP
}

func getUptimeSeconds() uint64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) > 0 {
		upFloat, err := strconv.ParseFloat(fields[0], 64)
		if err == nil {
			return uint64(upFloat)
		}
	}
	return 0
}
