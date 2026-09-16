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
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procBeginPaint          = user32.NewProc("BeginPaint")
	procEndPaint            = user32.NewProc("EndPaint")
	procFillRect            = user32.NewProc("FillRect")
	procDrawTextW           = user32.NewProc("DrawTextW")
	procInvalidateRect      = user32.NewProc("InvalidateRect")

	// Gdi32 procedures
	gdi32                = syscall.NewLazyDLL("gdi32.dll")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procSetBkMode        = gdi32.NewProc("SetBkMode")

	// Kernel32 procedures
	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGetDiskFreeSpaceExW  = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetNativeSystemInfo  = kernel32.NewProc("GetNativeSystemInfo")
	procGetSystemPowerStatus = kernel32.NewProc("GetSystemPowerStatus")
	procGetTickCount64       = kernel32.NewProc("GetTickCount64")
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
	WM_DESTROY    = 0x0002
	WM_PAINT      = 0x000F
	WM_CLOSE      = 0x0010
	WM_ERASEBKGND = 0x0014

	// Show window commands
	SW_HIDE           = 0
	SW_SHOWNOACTIVATE = 4
	SW_MINIMIZE       = 6
	SW_RESTORE        = 9

	// Window Extended Styles
	WS_EX_TOPMOST    = 0x00000008
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_NOACTIVATE = 0x08000000

	// Window Styles
	WS_POPUP = 0x80000000

	// System Metrics
	SM_CXSCREEN = 0
	SM_CYSCREEN = 1

	// DrawText format flags
	DT_LEFT       = 0x00000000
	DT_TOP        = 0x00000000
	DT_SINGLELINE = 0x00000020
	DT_NOCLIP     = 0x00000100
)

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type MSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       struct{ X, Y int32 }
	LPrivate uint32
}

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

type SYSTEM_POWER_STATUS struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

func GetSystemPowerStatus(sps *SYSTEM_POWER_STATUS) bool {
	ret, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(sps)))
	return ret != 0
}

func GetTickCount64() uint64 {
	ret, _, _ := procGetTickCount64.Call()
	return uint64(ret)
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

func RegisterClassEx(wc *WNDCLASSEXW) (uint16, error) {
	ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(wc)))
	if ret == 0 {
		return 0, err
	}
	return uint16(ret), nil
}

func CreateWindowEx(exStyle uint32, className, windowName string, style uint32, x, y, width, height int32, parent, menu, instance uintptr, param unsafe.Pointer) (uintptr, error) {
	clsNamePtr, err := syscall.UTF16PtrFromString(className)
	if err != nil {
		return 0, err
	}
	winNamePtr, err := syscall.UTF16PtrFromString(windowName)
	if err != nil {
		return 0, err
	}

	ret, _, callErr := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(clsNamePtr)),
		uintptr(unsafe.Pointer(winNamePtr)),
		uintptr(style),
		uintptr(x),
		uintptr(y),
		uintptr(width),
		uintptr(height),
		parent,
		menu,
		instance,
		uintptr(param),
	)
	if ret == 0 {
		return 0, callErr
	}
	return ret, nil
}

func DestroyWindow(hwnd uintptr) bool {
	ret, _, _ := procDestroyWindow.Call(hwnd)
	return ret != 0
}

func ShowWindow(hwnd uintptr, cmdShow int32) bool {
	ret, _, _ := procShowWindow.Call(hwnd, uintptr(cmdShow))
	return ret != 0
}

func DefWindowProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

func GetMessage(msg *MSG, hwnd uintptr, msgFilterMin, msgFilterMax uint32) int32 {
	ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(msg)), hwnd, uintptr(msgFilterMin), uintptr(msgFilterMax))
	return int32(ret)
}

func TranslateMessage(msg *MSG) bool {
	ret, _, _ := procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
	return ret != 0
}

func DispatchMessage(msg *MSG) uintptr {
	ret, _, _ := procDispatchMessageW.Call(uintptr(unsafe.Pointer(msg)))
	return ret
}

func PostQuitMessage(exitCode int32) {
	procPostQuitMessage.Call(uintptr(exitCode))
}

func GetSystemMetrics(nIndex int32) int32 {
	ret, _, _ := procGetSystemMetrics.Call(uintptr(nIndex))
	return int32(ret)
}

func BeginPaint(hwnd uintptr, ps *PAINTSTRUCT) uintptr {
	ret, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(ps)))
	return ret
}

func EndPaint(hwnd uintptr, ps *PAINTSTRUCT) bool {
	ret, _, _ := procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(ps)))
	return ret != 0
}

func FillRect(hdc uintptr, rect *RECT, hbr uintptr) int32 {
	ret, _, _ := procFillRect.Call(hdc, uintptr(unsafe.Pointer(rect)), hbr)
	return int32(ret)
}

func DrawText(hdc uintptr, text string, rect *RECT, format uint32) int32 {
	textPtr, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return 0
	}
	ret, _, _ := procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(textPtr)), uintptr(len(text)), uintptr(unsafe.Pointer(rect)), uintptr(format))
	return int32(ret)
}

func InvalidateRect(hwnd uintptr, rect *RECT, erase bool) bool {
	var eraseVal uintptr
	if erase {
		eraseVal = 1
	}
	ret, _, _ := procInvalidateRect.Call(hwnd, uintptr(unsafe.Pointer(rect)), eraseVal)
	return ret != 0
}

func CreateSolidBrush(color uint32) uintptr {
	ret, _, _ := procCreateSolidBrush.Call(uintptr(color))
	return ret
}

func DeleteObject(obj uintptr) bool {
	ret, _, _ := procDeleteObject.Call(obj)
	return ret != 0
}

func SetTextColor(hdc uintptr, color uint32) uint32 {
	ret, _, _ := procSetTextColor.Call(hdc, uintptr(color))
	return uint32(ret)
}

func SetBkMode(hdc uintptr, mode int32) int32 {
	ret, _, _ := procSetBkMode.Call(hdc, uintptr(mode))
	return int32(ret)
}
