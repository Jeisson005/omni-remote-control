package gui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/Jeisson005/omni-remote-control/client-linux/internal/executor"
)

type GUIController struct {
	hasXdotool bool
	hasWmctrl  bool
}

func NewGUIController() *GUIController {
	_, errXdo := exec.LookPath("xdotool")
	_, errWm := exec.LookPath("wmctrl")

	return &GUIController{
		hasXdotool: errXdo == nil,
		hasWmctrl:  errWm == nil,
	}
}

func (g *GUIController) ExecuteGUIAction(action string, payload map[string]interface{}) *executor.ExecutionResult {
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":0"
		os.Setenv("DISPLAY", display)
	}

	switch action {
	case "click", "gui_click":
		return g.handleClick(payload)
	case "move", "gui_move":
		return g.handleMove(payload)
	case "type", "gui_type":
		return g.handleType(payload)
	case "key", "gui_key":
		return g.handleKey(payload)
	case "window", "gui_window", "gui_list_windows", "list_windows":
		return g.handleWindow(payload)
	case "focus_window", "gui_focus_window":
		return g.handleFocusWindow(payload)
	case "close_window", "gui_close_window":
		return g.handleCloseWindow(payload)
	case "screenshot", "gui_screenshot":
		return g.handleScreenshot()
	default:
		return &executor.ExecutionResult{
			ExitCode: 1,
			Error:    fmt.Sprintf("Unknown GUI action: %s", action),
		}
	}
}

func (g *GUIController) handleClick(payload map[string]interface{}) *executor.ExecutionResult {
	if !g.hasXdotool {
		return &executor.ExecutionResult{ExitCode: 1, Error: "xdotool is not installed"}
	}

	button := 1
	if b, ok := payload["button"].(float64); ok && b > 0 {
		button = int(b)
	}

	var args []string
	x, hasX := getInt(payload, "x")
	y, hasY := getInt(payload, "y")
	if hasX && hasY {
		args = append(args, "mousemove", strconv.Itoa(x), strconv.Itoa(y))
	}

	args = append(args, "click", strconv.Itoa(button))
	return runCmd("xdotool", args...)
}

func (g *GUIController) handleMove(payload map[string]interface{}) *executor.ExecutionResult {
	if !g.hasXdotool {
		return &executor.ExecutionResult{ExitCode: 1, Error: "xdotool is not installed"}
	}

	x, hasX := getInt(payload, "x")
	y, hasY := getInt(payload, "y")
	if !hasX || !hasY {
		return &executor.ExecutionResult{ExitCode: 1, Error: "x and y coordinates are required"}
	}

	return runCmd("xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y))
}

func (g *GUIController) handleType(payload map[string]interface{}) *executor.ExecutionResult {
	if !g.hasXdotool {
		return &executor.ExecutionResult{ExitCode: 1, Error: "xdotool is not installed"}
	}

	text, _ := payload["text"].(string)
	if text == "" {
		return &executor.ExecutionResult{ExitCode: 1, Error: "text parameter is required"}
	}

	delay := "12"
	if d, ok := payload["delay_ms"].(float64); ok && d > 0 {
		delay = strconv.Itoa(int(d))
	}

	return runCmd("xdotool", "type", "--delay", delay, text)
}

func (g *GUIController) handleKey(payload map[string]interface{}) *executor.ExecutionResult {
	if !g.hasXdotool {
		return &executor.ExecutionResult{ExitCode: 1, Error: "xdotool is not installed"}
	}

	key, _ := payload["key"].(string)
	if key == "" {
		return &executor.ExecutionResult{ExitCode: 1, Error: "key parameter is required"}
	}

	return runCmd("xdotool", "key", key)
}

func (g *GUIController) handleWindow(payload map[string]interface{}) *executor.ExecutionResult {
	subAction, _ := payload["sub_action"].(string)
	if subAction == "" {
		subAction = "list"
	}

	switch subAction {
	case "list":
		if g.hasWmctrl {
			return runCmd("wmctrl", "-l")
		} else if g.hasXdotool {
			return runCmd("xdotool", "search", "--onlyvisible", "--name", ".")
		}
		return &executor.ExecutionResult{ExitCode: 1, Error: "neither wmctrl nor xdotool is installed"}
	case "focus":
		return g.handleFocusWindow(payload)
	case "close":
		return g.handleCloseWindow(payload)
	default:
		return &executor.ExecutionResult{ExitCode: 1, Error: fmt.Sprintf("unknown window sub_action: %s", subAction)}
	}
}

func (g *GUIController) handleFocusWindow(payload map[string]interface{}) *executor.ExecutionResult {
	target, _ := payload["target"].(string)
	if target == "" {
		target, _ = payload["window_id"].(string)
	}
	if target == "" {
		return &executor.ExecutionResult{ExitCode: 1, Error: "window target/id is required"}
	}

	if g.hasWmctrl {
		return runCmd("wmctrl", "-a", target)
	} else if g.hasXdotool {
		return runCmd("xdotool", "windowactivate", target)
	}
	return &executor.ExecutionResult{ExitCode: 1, Error: "neither wmctrl nor xdotool is installed"}
}

func (g *GUIController) handleCloseWindow(payload map[string]interface{}) *executor.ExecutionResult {
	target, _ := payload["target"].(string)
	if target == "" {
		target, _ = payload["window_id"].(string)
	}
	if target == "" {
		return &executor.ExecutionResult{ExitCode: 1, Error: "window target/id is required"}
	}

	if g.hasWmctrl {
		return runCmd("wmctrl", "-c", target)
	} else if g.hasXdotool {
		return runCmd("xdotool", "windowclose", target)
	}
	return &executor.ExecutionResult{ExitCode: 1, Error: "neither wmctrl nor xdotool is installed"}
}

func runCmd(name string, args ...string) *executor.ExecutionResult {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	errMsg := ""
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			errMsg = stderr.String()
		} else {
			exitCode = 1
			errMsg = err.Error()
		}
	}

	return &executor.ExecutionResult{
		ExitCode: exitCode,
		Output:   stdout.String(),
		Error:    errMsg,
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

func (g *GUIController) handleScreenshot() *executor.ExecutionResult {
	tmpFile := fmt.Sprintf("/tmp/omni_screen_%d.png", os.Getpid())
	defer os.Remove(tmpFile)

	var cmd *exec.Cmd
	if _, err := exec.LookPath("scrot"); err == nil {
		cmd = exec.Command("scrot", "-z", "-o", tmpFile)
	} else if _, err := exec.LookPath("import"); err == nil {
		cmd = exec.Command("import", "-window", "root", tmpFile)
	} else {
		return &executor.ExecutionResult{
			ExitCode: 1,
			Error:    "neither scrot nor ImageMagick import is installed for capturing screenshot",
		}
	}

	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return &executor.ExecutionResult{
			ExitCode: 1,
			Error:    fmt.Sprintf("failed to capture screenshot: %v", err),
		}
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		return &executor.ExecutionResult{
			ExitCode: 1,
			Error:    fmt.Sprintf("failed to read captured screenshot: %v", err),
		}
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return &executor.ExecutionResult{
		ExitCode: 0,
		Output:   encoded,
	}
}
