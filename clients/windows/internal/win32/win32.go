package win32

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	// User32 procedures
	procSetCursorPos         = user32.NewProc("SetCursorPos")
	procMouseEvent           = user32.NewProc("mouse_event")
	procKeybdEvent           = user32.NewProc("keybd_event")
	procEnumWindows          = user32.NewProc("EnumWindows")
	procIsWindowVisible      = user32.NewProc("IsWindowVisible")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procShowWindow           = user32.NewProc("ShowWindow")
	procPostMessageW         = user32.NewProc("PostMessageW")

	// Kernel32 procedures
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGetDiskFreeSpaceExW  = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetNativeSystemInfo  = kernel32.NewProc("GetNativeSystemInfo")
)

const (
	// Mouse event flags
	MOUSEEVENTF_MOVE       = 0x0001
	MOUSEEVENTF_LEFTDOWN   = 0x0002
	MOUSEEVENTF_LEFTUP     = 0x0004
	MOUSEEVENTF_RIGHTDOWN  = 0x0008
	MOUSEEVENTF_RIGHTUP    = 0x0010
	MOUSEEVENTF_MIDDLEDOWN = 0x0020
	MOUSEEVENTF_MIDDLEUP   = 0x0040

	// Keyboard event flags
	KEYEVENTF_KEYUP   = 0x0002
	KEYEVENTF_UNICODE = 0x0004

	// Window messages
	WM_CLOSE = 0x0010

	// Show window commands
	SW_MINIMIZE = 6
	SW_RESTORE  = 9
)

type MEMORYSTATUSEX struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type FILETIME struct {
	LowDateTime  uint32
	HighDateTime uint32
}

func (ft *FILETIME) ToUint64() uint64 {
	return (uint64(ft.HighDateTime) << 32) | uint64(ft.LowDateTime)
}

type SYSTEM_INFO struct {
	ProcessorArchitecture     uint16
	Reserved                  uint16
	PageSize                  uint32
	MinimumApplicationAddress uintptr
	MaximumApplicationAddress uintptr
	ActiveProcessorMask       uintptr
	NumberOfProcessors        uint32
	ProcessorType             uint32
	AllocationGranularity     uint32
	ProcessorLevel            uint16
	ProcessorRevision         uint16
}

// SetCursorPosition mueve el cursor a las coordenadas X, Y
func SetCursorPosition(x, y int) bool {
	ret, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y))
	return ret != 0
}

// MouseEvent dispara un evento de ratón en Windows
func MouseEvent(flags uint32, dx, dy, data uint32) {
	procMouseEvent.Call(uintptr(flags), uintptr(dx), uintptr(dy), uintptr(data), 0)
}

// KeyboardEvent dispara un evento de teclado
func KeyboardEvent(bVk, bScan byte, flags uint32) {
	procKeybdEvent.Call(uintptr(bVk), uintptr(bScan), uintptr(flags), 0)
}

// SendUnicodeChar envía un caracter Unicode mediante keybd_event
func SendUnicodeChar(char rune) {
	procKeybdEvent.Call(0, uintptr(char), KEYEVENTF_UNICODE, 0)
	procKeybdEvent.Call(0, uintptr(char), KEYEVENTF_UNICODE|KEYEVENTF_KEYUP, 0)
}

// WindowInfo representa una ventana abierta en el escritorio
type WindowInfo struct {
	HWND  uintptr
	Title string
}

// EnumDesktopWindows lista todas las ventanas de nivel superior visibles con título
func EnumDesktopWindows() ([]WindowInfo, error) {
	var windows []WindowInfo

	cb := syscall.NewCallback(func(hwnd, lParam uintptr) uintptr {
		// Verificar visibilidad
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1 // continuar
		}

		length, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if length == 0 {
			return 1 // ventana sin título (invisible o de sistema)
		}

		buf := make([]uint16, length+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(length+1))
		title := syscall.UTF16ToString(buf)

		if title != "" {
			windows = append(windows, WindowInfo{
				HWND:  hwnd,
				Title: title,
			})
		}
		return 1 // continuar enumeración
	})

	procEnumWindows.Call(cb, 0)
	return windows, nil
}

// SetWindowForeground trae la ventana al frente
func SetWindowForeground(hwnd uintptr) bool {
	ret, _, _ := procSetForegroundWindow.Call(hwnd)
	return ret != 0
}

// CloseTargetWindow envía mensaje WM_CLOSE a la ventana
func CloseTargetWindow(hwnd uintptr) bool {
	ret, _, _ := procPostMessageW.Call(hwnd, WM_CLOSE, 0, 0)
	return ret != 0
}

// MinimizeTargetWindow minimiza la ventana
func MinimizeTargetWindow(hwnd uintptr) bool {
	ret, _, _ := procShowWindow.Call(hwnd, SW_MINIMIZE)
	return ret != 0
}

// GetMemoryStatus obtiene estado de memoria física en Windows
func GetMemoryStatus() (*MEMORYSTATUSEX, error) {
	var mem MEMORYSTATUSEX
	mem.Length = uint32(unsafe.Sizeof(mem))

	ret, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem)))
	if ret == 0 {
		return nil, err
	}
	return &mem, nil
}

// GetSystemCPUtimes obtiene tiempos globales del procesador (Idle, Kernel, User)
func GetSystemCPUtimes() (idle, kernel, user uint64, err error) {
	var idleTime, kernelTime, userTime FILETIME
	ret, _, callErr := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	if ret == 0 {
		return 0, 0, 0, callErr
	}
	return idleTime.ToUint64(), kernelTime.ToUint64(), userTime.ToUint64(), nil
}

// GetDiskSpaceInfo obtiene espacio total y libre en el volumen (ej. "C:\\")
func GetDiskSpaceInfo(path string) (totalBytes, freeBytes uint64, err error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}

	var freeAvailable, total, totalFree uint64
	ret, _, callErr := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeAvailable)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if ret == 0 {
		return 0, 0, callErr
	}
	return total, freeAvailable, nil
}
