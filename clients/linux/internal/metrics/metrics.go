package metrics

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
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
	DeviceID     string        `json:"device_id"`
	CPUUsagePct  float64       `json:"cpu_usage_pct"`
	RAMUsagePct  float64       `json:"ram_usage_pct"`
	RAMUsedBytes uint64        `json:"ram_used_bytes"`
	DiskUsagePct float64       `json:"disk_usage_pct"`
	OpenWindows  []WindowInfo  `json:"open_windows"`
	TopProcesses []ProcessInfo `json:"top_processes"`
	RecordedAt   time.Time     `json:"recorded_at"`
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

	return &TelemetryMetric{
		DeviceID:     deviceID,
		CPUUsagePct:  round(cpuPct, 2),
		RAMUsagePct:  round(ramPct, 2),
		RAMUsedBytes: ramUsed,
		DiskUsagePct: round(diskPct, 2),
		OpenWindows:  windows,
		TopProcesses: processes,
		RecordedAt:   time.Now().UTC(),
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
