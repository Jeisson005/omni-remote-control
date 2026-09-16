package gui

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Jeisson005/omni-remote-control/client-windows/internal/win32"
)

type ExecutionResult struct {
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
}

type WindowsGUIController struct{}

func NewGUIController() *WindowsGUIController {
	return &WindowsGUIController{}
}

func (g *WindowsGUIController) ExecuteGUIAction(action string, payload map[string]interface{}) *ExecutionResult {
	switch action {
	case "click", "gui_click":
		return g.handleClick(payload)
	case "move", "gui_move":
		return g.handleMove(payload)
	case "type", "gui_type":
		return g.handleType(payload)
	case "key", "gui_key":
		return g.handleKey(payload)
	case "window", "gui_window", "list_windows", "gui_list_windows":
		return g.handleWindow(payload)
	case "focus_window", "gui_focus_window":
		return g.handleFocusWindow(payload)
	case "close_window", "gui_close_window":
		return g.handleCloseWindow(payload)
	case "minimize_window", "gui_minimize_window":
		return g.handleMinimizeWindow(payload)
	case "screenshot", "gui_screenshot":
		return g.handleScreenshot()
	default:
		return &ExecutionResult{
			ExitCode: 1,
			Error:    fmt.Sprintf("Unknown Windows GUI action: %s", action),
		}
	}
}

func (g *WindowsGUIController) handleClick(payload map[string]interface{}) *ExecutionResult {
	button := 1
	if b, ok := payload["button"].(float64); ok && b > 0 {
		button = int(b)
	}

	x, hasX := getInt(payload, "x")
	y, hasY := getInt(payload, "y")
	if hasX && hasY {
		win32.SetCursorPosition(x, y)
		time.Sleep(10 * time.Millisecond)
	}

	switch button {
	case 1: // Clic Izquierdo
		win32.MouseEvent(win32.MOUSEEVENTF_LEFTDOWN, 0, 0, 0)
		time.Sleep(10 * time.Millisecond)
		win32.MouseEvent(win32.MOUSEEVENTF_LEFTUP, 0, 0, 0)
	case 2: // Clic Central
		win32.MouseEvent(win32.MOUSEEVENTF_MIDDLEDOWN, 0, 0, 0)
		time.Sleep(10 * time.Millisecond)
		win32.MouseEvent(win32.MOUSEEVENTF_MIDDLEUP, 0, 0, 0)
	case 3: // Clic Derecho
		win32.MouseEvent(win32.MOUSEEVENTF_RIGHTDOWN, 0, 0, 0)
		time.Sleep(10 * time.Millisecond)
		win32.MouseEvent(win32.MOUSEEVENTF_RIGHTUP, 0, 0, 0)
	}

	return &ExecutionResult{ExitCode: 0, Output: fmt.Sprintf("Clicked button %d at (%d, %d)", button, x, y)}
}

func (g *WindowsGUIController) handleMove(payload map[string]interface{}) *ExecutionResult {
	x, hasX := getInt(payload, "x")
	y, hasY := getInt(payload, "y")
	if !hasX || !hasY {
		return &ExecutionResult{ExitCode: 1, Error: "x and y coordinates are required"}
	}

	success := win32.SetCursorPosition(x, y)
	if !success {
		return &ExecutionResult{ExitCode: 1, Error: "failed to set cursor position"}
	}

	return &ExecutionResult{ExitCode: 0, Output: fmt.Sprintf("Moved cursor to (%d, %d)", x, y)}
}

func (g *WindowsGUIController) handleType(payload map[string]interface{}) *ExecutionResult {
	text, _ := payload["text"].(string)
	if text == "" {
		return &ExecutionResult{ExitCode: 1, Error: "text parameter is required"}
	}

	delayMs := 12
	if d, ok := payload["delay_ms"].(float64); ok && d > 0 {
		delayMs = int(d)
	}

	for _, char := range text {
		win32.SendUnicodeChar(char)
		if delayMs > 0 {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	}

	return &ExecutionResult{ExitCode: 0, Output: fmt.Sprintf("Typed %d characters", len([]rune(text)))}
}

func (g *WindowsGUIController) handleKey(payload map[string]interface{}) *ExecutionResult {
	key, _ := payload["key"].(string)
	if key == "" {
		return &ExecutionResult{ExitCode: 1, Error: "key parameter is required"}
	}

	vk, ok := mapVirtualKey(key)
	if !ok {
		return &ExecutionResult{ExitCode: 1, Error: fmt.Sprintf("unsupported virtual key: %s", key)}
	}

	win32.KeyboardEvent(vk, 0, 0)
	time.Sleep(10 * time.Millisecond)
	win32.KeyboardEvent(vk, 0, win32.KEYEVENTF_KEYUP)

	return &ExecutionResult{ExitCode: 0, Output: fmt.Sprintf("Sent key %s (0x%02X)", key, vk)}
}

func (g *WindowsGUIController) handleWindow(payload map[string]interface{}) *ExecutionResult {
	subAction, _ := payload["sub_action"].(string)
	if subAction == "" {
		subAction = "list"
	}

	switch subAction {
	case "list":
		windows, err := win32.EnumDesktopWindows()
		if err != nil {
			return &ExecutionResult{ExitCode: 1, Error: err.Error()}
		}

		var sb strings.Builder
		for _, w := range windows {
			sb.WriteString(fmt.Sprintf("0x%08X\t%s\n", w.HWND, w.Title))
		}
		return &ExecutionResult{ExitCode: 0, Output: sb.String()}

	case "focus":
		return g.handleFocusWindow(payload)
	case "close":
		return g.handleCloseWindow(payload)
	default:
		return &ExecutionResult{ExitCode: 1, Error: fmt.Sprintf("unknown window sub_action: %s", subAction)}
	}
}

func (g *WindowsGUIController) handleFocusWindow(payload map[string]interface{}) *ExecutionResult {
	hwnd, ok := parseHWND(payload)
	if !ok {
		return &ExecutionResult{ExitCode: 1, Error: "valid window HWND is required (e.g. '0x0001002A' or integer)"}
	}

	if win32.SetWindowForeground(hwnd) {
		return &ExecutionResult{ExitCode: 0, Output: fmt.Sprintf("Focused window 0x%X", hwnd)}
	}
	return &ExecutionResult{ExitCode: 1, Error: fmt.Sprintf("failed to bring window 0x%X to foreground", hwnd)}
}

func (g *WindowsGUIController) handleCloseWindow(payload map[string]interface{}) *ExecutionResult {
	hwnd, ok := parseHWND(payload)
	if !ok {
		return &ExecutionResult{ExitCode: 1, Error: "valid window HWND is required"}
	}

	if win32.CloseTargetWindow(hwnd) {
		return &ExecutionResult{ExitCode: 0, Output: fmt.Sprintf("Closed window 0x%X", hwnd)}
	}
	return &ExecutionResult{ExitCode: 1, Error: fmt.Sprintf("failed to close window 0x%X", hwnd)}
}

func (g *WindowsGUIController) handleMinimizeWindow(payload map[string]interface{}) *ExecutionResult {
	hwnd, ok := parseHWND(payload)
	if !ok {
		return &ExecutionResult{ExitCode: 1, Error: "valid window HWND is required"}
	}

	if win32.MinimizeTargetWindow(hwnd) {
		return &ExecutionResult{ExitCode: 0, Output: fmt.Sprintf("Minimized window 0x%X", hwnd)}
	}
	return &ExecutionResult{ExitCode: 1, Error: fmt.Sprintf("failed to minimize window 0x%X", hwnd)}
}

func parseHWND(payload map[string]interface{}) (uintptr, bool) {
	val, ok := payload["hwnd"]
	if !ok {
		val, ok = payload["window_id"]
	}
	if !ok {
		val, ok = payload["target"]
	}
	if !ok {
		return 0, false
	}

	switch v := val.(type) {
	case float64:
		return uintptr(v), true
	case int:
		return uintptr(v), true
	case string:
		v = strings.TrimSpace(v)
		if strings.HasPrefix(strings.ToLower(v), "0x") {
			parsed, err := strconv.ParseUint(v[2:], 16, 64)
			return uintptr(parsed), err == nil
		}
		parsed, err := strconv.ParseUint(v, 10, 64)
		return uintptr(parsed), err == nil
	}
	return 0, false
}

func mapVirtualKey(name string) (byte, bool) {
	switch strings.ToLower(name) {
	case "return", "enter":
		return 0x0D, true
	case "tab":
		return 0x09, true
	case "space":
		return 0x20, true
	case "backspace":
		return 0x08, true
	case "escape", "esc":
		return 0x1B, true
	case "up":
		return 0x26, true
	case "down":
		return 0x28, true
	case "left":
		return 0x25, true
	case "right":
		return 0x27, true
	case "delete", "del":
		return 0x2E, true
	case "home":
		return 0x24, true
	case "end":
		return 0x23, true
	case "pageup":
		return 0x21, true
	case "pagedown":
		return 0x22, true
	default:
		if len(name) == 1 {
			char := strings.ToUpper(name)[0]
			if (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
				return char, true
			}
		}
		return 0, false
	}
}

func getInt(payload map[string]interface{}, key string) (int, bool) {
	if val, ok := payload[key]; ok {
		switch v := val.(type) {
		case float64:
			return int(v), true
		case int:
			return v, true
		case string:
			if i, err := strconv.Atoi(v); err == nil {
				return i, true
			}
		}
	}
	return 0, false
}

func (g *WindowsGUIController) handleScreenshot() *ExecutionResult {
	// Captura de pantalla nativa usando PowerShell y .NET System.Drawing / System.Windows.Forms
	psScript := `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$bitmap = New-Object System.Drawing.Bitmap $bounds.Width, $bounds.Height
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size)
$stream = New-Object System.IO.MemoryStream
$bitmap.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
[Convert]::ToBase64String($stream.ToArray())
`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", psScript)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return &ExecutionResult{
			ExitCode: 1,
			Error:    fmt.Sprintf("failed to capture Windows screenshot: %v - %s", err, stderr.String()),
		}
	}

	base64Str := strings.TrimSpace(stdout.String())
	return &ExecutionResult{
		ExitCode: 0,
		Output:   base64Str,
	}
}
