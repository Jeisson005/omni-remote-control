package sysinfo

import (
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Jeisson005/omni-remote-control/client-windows/internal/win32"
)

type SystemInfo struct {
	DeviceID      string    `json:"device_id"`
	CPUModel      string    `json:"cpu_model"`
	CPUCores      int       `json:"cpu_cores"`
	RAMTotalBytes uint64    `json:"ram_total_bytes"`
	DiskTotalBytes uint64   `json:"disk_total_bytes"`
	OSVersion     string    `json:"os_version"`
	KernelVersion string    `json:"kernel_version"`
	Arch          string    `json:"arch"`
	IPAddress     string    `json:"ip_address"`
	MACAddress    string    `json:"mac_address"`
	Timezone      string    `json:"timezone"`
	AgentVersion  string    `json:"agent_version"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func Collect(deviceID, agentVersion string) *SystemInfo {
	info := &SystemInfo{
		DeviceID:     deviceID,
		Arch:         runtime.GOARCH,
		AgentVersion: agentVersion,
		UpdatedAt:    time.Now().UTC(),
		Timezone:     time.Now().Location().String(),
		CPUCores:     runtime.NumCPU(),
	}

	info.CPUModel = getCPUModel()
	info.RAMTotalBytes = getRAMTotal()
	info.DiskTotalBytes = getDiskTotal()
	info.OSVersion, info.KernelVersion = getWindowsVersion()
	info.IPAddress, info.MACAddress = getNetworkInfo()

	return info
}

func getCPUModel() string {
	if val := os.Getenv("PROCESSOR_IDENTIFIER"); val != "" {
		return val
	}
	return runtime.GOARCH + " Processor"
}

func getRAMTotal() uint64 {
	mem, err := win32.GetMemoryStatus()
	if err != nil {
		return 0
	}
	return mem.TotalPhys
}

func getDiskTotal() uint64 {
	systemDrive := os.Getenv("SystemDrive")
	if systemDrive == "" {
		systemDrive = "C:"
	}
	total, _, err := win32.GetDiskSpaceInfo(systemDrive + "\\")
	if err != nil {
		return 0
	}
	return total
}

func getWindowsVersion() (osVer, kernelVer string) {
	// Obtener nombre amigable de Windows mediante powershell o entorno
	out, err := exec.Command("cmd.exe", "/c", "ver").Output()
	if err == nil && len(out) > 0 {
		kernelVer = strings.TrimSpace(string(out))
	} else {
		kernelVer = "Microsoft Windows"
	}

	osVer = "Windows " + runtime.GOARCH
	return osVer, kernelVer
}

func getNetworkInfo() (string, string) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1", ""
	}

	for _, iface := range interfaces {
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

			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				return ip.String(), iface.HardwareAddr.String()
			}
		}
	}

	return "127.0.0.1", ""
}
