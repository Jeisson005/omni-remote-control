package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type ExecutionResult struct {
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
}

// ExecuteCommand ejecuta un comando en PowerShell o cmd según se especifique
func ExecuteCommand(command string, shellType string, timeout time.Duration) *ExecutionResult {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var cmd *exec.Cmd
	if shellType == "cmd" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/c", command)
	} else {
		// Por defecto PowerShell
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", command)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	errMsg := ""

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			exitCode = -1
			errMsg = fmt.Sprintf("Command timed out after %v", timeout)
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			errMsg = stderr.String()
		} else {
			exitCode = 1
			errMsg = err.Error()
		}
	}

	outStr := stdout.String()
	if errMsg == "" && stderr.Len() > 0 {
		errMsg = stderr.String()
	}

	return &ExecutionResult{
		ExitCode: exitCode,
		Output:   outStr,
		Error:    errMsg,
	}
}
