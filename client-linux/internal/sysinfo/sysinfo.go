package sysinfo

import (
	"bufio"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type SystemInfo struct {
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

func Collect(deviceID, agentVersion string) *SystemInfo {
	info := &SystemInfo{
		DeviceID:      deviceID,
		Arch:          runtime.GOARCH,
		AgentVersion:  agentVersion,
		UpdatedAt:     time.Now().UTC(),
		Timezone:      time.Now().Location().String(),
		UptimeSeconds: getUptimeSeconds(),
	}

	info.CPUCores = runtime.NumCPU()
	info.CPUModel = getCPUModel()
	info.RAMTotalBytes = getRAMTotal()
	info.DiskTotalBytes = getDiskTotal()
	info.OSVersion = getOSVersion()
	info.KernelVersion = getKernelVersion()
	info.IPAddress, info.MACAddress, info.NetworkName = getNetworkInfoWithIface()

	return info
}

func getCPUModel() string {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return runtime.GOARCH + " processor"
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "model name") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return runtime.GOARCH + " CPU"
}

func getRAMTotal() uint64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				kb, err := strconv.ParseUint(parts[1], 10, 64)
				if err == nil {
					return kb * 1024
				}
			}
		}
	}
	return 0
}

func getDiskTotal() uint64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0
	}
	return stat.Blocks * uint64(stat.Bsize)
}

func getOSVersion() string {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return "Linux Generic"
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return "Linux"
}

func getKernelVersion() string {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return "Linux"
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 3 {
		return fields[2]
	}
	return strings.TrimSpace(string(data))
}

func getNetworkInfoWithIface() (string, string, string) {
	var ipAddr, macAddr string

	interfaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1", "", "lo"
	}

	for _, iface := range interfaces {
		// Ignore loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Looking for IPv4
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				ipAddr = ip.String()
				macAddr = iface.HardwareAddr.String()
				return ipAddr, macAddr, iface.Name
			}
		}
	}

	return "127.0.0.1", "", "lo"
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
