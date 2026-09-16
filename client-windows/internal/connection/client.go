package connection

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Jeisson005/omni-remote-control/client-windows/internal/config"
	"github.com/Jeisson005/omni-remote-control/client-windows/internal/executor"
	"github.com/Jeisson005/omni-remote-control/client-windows/internal/gui"
	"github.com/Jeisson005/omni-remote-control/client-windows/internal/metrics"
	"github.com/Jeisson005/omni-remote-control/client-windows/internal/sysinfo"
	"github.com/gorilla/websocket"
)

type WSMessage struct {
	Type      string          `json:"type"`
	DeviceID  string          `json:"device_id,omitempty"`
	CommandID string          `json:"command_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type AgentClient struct {
	cfg       *config.Config
	gui       *gui.WindowsGUIController
	collector *metrics.WindowsCollector
	conn      *websocket.Conn
	connMu    sync.Mutex
	stopChan  chan struct{}
}

func NewAgentClient(cfg *config.Config) *AgentClient {
	return &AgentClient{
		cfg:       cfg,
		gui:       gui.NewGUIController(),
		collector: metrics.NewCollector(),
		stopChan:  make(chan struct{}),
	}
}

func (a *AgentClient) Start(ctx context.Context) {
	log.Printf("Starting Omni Windows Agent for device: %s (%s)", a.cfg.DeviceID, a.cfg.DeviceName)

	for {
		select {
		case <-ctx.Done():
			log.Println("Windows agent stopping...")
			return
		case <-a.stopChan:
			return
		default:
			err := a.connectAndRun(ctx)
			if err != nil {
				log.Printf("Connection error: %v. Reconnecting in 3 seconds...", err)
			}
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}
}

func (a *AgentClient) connectAndRun(ctx context.Context) error {
	u, err := url.Parse(a.cfg.ServerURL)
	if err != nil {
		return err
	}

	log.Printf("Connecting to server at %s ...", u.String())
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return err
	}

	a.connMu.Lock()
	a.conn = conn
	a.connMu.Unlock()

	defer func() {
		a.connMu.Lock()
		if a.conn != nil {
			_ = a.conn.Close()
			a.conn = nil
		}
		a.connMu.Unlock()
	}()

	log.Println("Connected to server successfully")

	// 1. Enviar registro inicial y telemetría estática
	if err := a.sendRegistration(); err != nil {
		return err
	}

	// 2. Iniciar temporizadores de heartbeat y telemetría dinámica
	heartbeatTicker := time.NewTicker(a.cfg.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	metricsTicker := time.NewTicker(a.cfg.MetricsInterval)
	defer metricsTicker.Stop()

	// Enviar primera muestra de telemetría dinámica de inmediato
	_ = a.sendDynamicTelemetry()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Printf("Read error from server: %v", err)
				return
			}

			var wsMsg WSMessage
			if err := json.Unmarshal(message, &wsMsg); err != nil {
				log.Printf("Failed to unmarshal server message: %v", err)
				continue
			}

			go a.handleServerMessage(&wsMsg)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-done:
			return nil
		case <-heartbeatTicker.C:
			log.Println("Sending periodic static heartbeat telemetry...")
			if err := a.sendRegistration(); err != nil {
				log.Printf("Failed to send static heartbeat: %v", err)
				return err
			}
		case <-metricsTicker.C:
			if err := a.sendDynamicTelemetry(); err != nil {
				log.Printf("Failed to send dynamic telemetry: %v", err)
				return err
			}
		}
	}
}

func (a *AgentClient) sendRegistration() error {
	info := sysinfo.Collect(a.cfg.DeviceID, a.cfg.AgentVersion)

	regData := map[string]interface{}{
		"device": map[string]interface{}{
			"id":       a.cfg.DeviceID,
			"name":     a.cfg.DeviceName,
			"hostname": a.cfg.DeviceName,
			"os":       "windows",
			"platform": info.OSVersion,
			"status":   "online",
		},
		"system_info": info,
	}

	payloadBytes, _ := json.Marshal(regData)
	msg := WSMessage{
		Type:     "register",
		DeviceID: a.cfg.DeviceID,
		Payload:  payloadBytes,
	}

	return a.sendWSMessage(&msg)
}

func (a *AgentClient) sendDynamicTelemetry() error {
	data := a.collector.Collect(a.cfg.DeviceID)
	payloadBytes, _ := json.Marshal(data)

	msg := WSMessage{
		Type:     "telemetry",
		DeviceID: a.cfg.DeviceID,
		Payload:  payloadBytes,
	}

	return a.sendWSMessage(&msg)
}

func (a *AgentClient) handleServerMessage(msg *WSMessage) {
	switch msg.Type {
	case "registered_ack":
		log.Println("Server acknowledged registration")

	case "ping":
		pong := WSMessage{Type: "pong"}
		_ = a.sendWSMessage(&pong)

	case "command_request":
		var cmdReq struct {
			ID      string                 `json:"id"`
			Type    string                 `json:"type"`
			Payload map[string]interface{} `json:"payload"`
		}

		if err := json.Unmarshal(msg.Payload, &cmdReq); err != nil {
			log.Printf("Invalid command payload: %v", err)
			return
		}

		log.Printf("Executing Windows command [%s] of type [%s]", cmdReq.ID, cmdReq.Type)

		var exitCode int
		var output, errMsg string

		if strings.HasPrefix(cmdReq.Type, "gui_") {
			action := strings.TrimPrefix(cmdReq.Type, "gui_")
			res := a.gui.ExecuteGUIAction(action, cmdReq.Payload)
			exitCode = res.ExitCode
			output = res.Output
			errMsg = res.Error
		} else if cmdReq.Type == "shell" {
			cmdStr, _ := cmdReq.Payload["command"].(string)
			shellType, _ := cmdReq.Payload["shell"].(string) // "powershell" o "cmd"
			timeoutSec := 30
			if t, ok := cmdReq.Payload["timeout"].(float64); ok && t > 0 {
				timeoutSec = int(t)
			}
			res := executor.ExecuteCommand(cmdStr, shellType, time.Duration(timeoutSec)*time.Second)
			exitCode = res.ExitCode
			output = res.Output
			errMsg = res.Error
		} else if cmdReq.Type == "collect_telemetry" || cmdReq.Type == "telemetry" {
			data := a.collector.Collect(a.cfg.DeviceID)
			jsonData, err := json.Marshal(data)
			if err != nil {
				exitCode = 1
				errMsg = "failed to marshal telemetry: " + err.Error()
			} else {
				exitCode = 0
				output = string(jsonData)
				errMsg = ""
			}
		} else {
			exitCode = 1
			errMsg = "unsupported command type for Windows: " + cmdReq.Type
		}

		status := "completed"
		if exitCode != 0 {
			status = "failed"
		}

		respData := map[string]interface{}{
			"id":        cmdReq.ID,
			"device_id": a.cfg.DeviceID,
			"status":    status,
			"exit_code": exitCode,
			"output":    output,
			"error":     errMsg,
		}

		respBytes, _ := json.Marshal(respData)
		replyMsg := WSMessage{
			Type:      "command_response",
			DeviceID:  a.cfg.DeviceID,
			CommandID: cmdReq.ID,
			Payload:   respBytes,
		}

		_ = a.sendWSMessage(&replyMsg)
		log.Printf("Finished Windows command [%s] with status: %s (exit code: %d)", cmdReq.ID, status, exitCode)
	}
}

func (a *AgentClient) sendWSMessage(msg *WSMessage) error {
	a.connMu.Lock()
	defer a.connMu.Unlock()

	if a.conn == nil {
		return nil
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	a.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return a.conn.WriteMessage(websocket.TextMessage, data)
}
