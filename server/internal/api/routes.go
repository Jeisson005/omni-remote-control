package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Jeisson005/omni-remote-control/server/internal/db"
	"github.com/Jeisson005/omni-remote-control/server/internal/models"
	"github.com/Jeisson005/omni-remote-control/server/internal/ws"
	"github.com/google/uuid"
)

type Server struct {
	db  *db.DB
	hub *ws.Hub
}

func NewServer(database *db.DB, hub *ws.Hub) *Server {
	return &Server{
		db:  database,
		hub: hub,
	}
}

func (s *Server) SetupRoutes() http.Handler {
	mux := http.NewServeMux()

	// WebSocket endpoint
	mux.HandleFunc("/ws/devices", s.hub.HandleWebSocket)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
	})

	// API v1
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	mux.HandleFunc("/api/v1/devices/", s.handleDeviceSubroutes)
	mux.HandleFunc("/api/v1/commands/", s.handleCommands)

	// MCP Tools metadata & invocation
	mux.HandleFunc("/api/v1/mcp/tools", s.handleMCPTools)
	mux.HandleFunc("/api/v1/mcp/tools/call", s.handleMCPToolCall)

	// CORS wrapper
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	devices, err := s.db.GetDevices()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Update status dynamically if active in WebSocket hub
	for i := range devices {
		if s.hub.IsDeviceOnline(devices[i].ID) {
			devices[i].Status = "online"
		}
	}

	writeJSON(w, http.StatusOK, devices)
}

func (s *Server) handleDeviceSubroutes(w http.ResponseWriter, r *http.Request) {
	// Pattern: /api/v1/devices/{id} or /api/v1/devices/{id}/telemetry or /api/v1/devices/{id}/commands
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	deviceID := parts[3]

	if len(parts) == 4 {
		// /api/v1/devices/{id}
		if r.Method == http.MethodGet {
			dev, err := s.db.GetDevice(deviceID)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			if dev == nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "Device not found"})
				return
			}
			if s.hub.IsDeviceOnline(dev.ID) {
				dev.Status = "online"
			}
			writeJSON(w, http.StatusOK, dev)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	action := parts[4]
	switch action {
	case "telemetry":
		if r.Method == http.MethodGet {
			isLive := r.URL.Query().Get("live") == "true" || r.URL.Query().Get("live") == "1"
			if len(parts) > 5 && parts[5] == "live" {
				isLive = true
			}

			if isLive {
				cmd := &models.Command{
					ID:        uuid.New().String(),
					DeviceID:  deviceID,
					Type:      "collect_telemetry",
					Payload:   map[string]interface{}{},
					CreatedAt: time.Now(),
				}

				executedCmd, err := s.hub.SendCommand(cmd, 15*time.Second)
				if err != nil {
					writeJSON(w, http.StatusBadGateway, map[string]interface{}{
						"error": "Failed to request live telemetry from device: " + err.Error(),
					})
					return
				}

				if executedCmd.ExitCode != 0 || executedCmd.Output == "" {
					errMsg := executedCmd.Error
					if errMsg == "" {
						errMsg = "telemetry output was empty"
					}
					writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
						"error": "Failed to collect live telemetry: " + errMsg,
					})
					return
				}

				var metric models.TelemetryMetric
				if err := json.Unmarshal([]byte(executedCmd.Output), &metric); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
						"error": "Failed to parse telemetry data: " + err.Error(),
					})
					return
				}

				if metric.DeviceID == "" {
					metric.DeviceID = deviceID
				}
				if metric.RecordedAt.IsZero() {
					metric.RecordedAt = time.Now()
				}

				// Guardar en base de datos la medición obtenida a demanda
				_ = s.db.InsertTelemetry(&metric)
				_ = s.db.UpdateDeviceStatus(deviceID, "online")

				writeJSON(w, http.StatusOK, metric)
				return
			}

			limitStr := r.URL.Query().Get("limit")
			limit := 20
			if limitStr != "" {
				if l, err := strconv.Atoi(limitStr); err == nil {
					limit = l
				}
			}

			metrics, err := s.db.GetTelemetry(deviceID, limit)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, metrics)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

	case "commands":
		if r.Method == http.MethodPost {
			var req struct {
				Type       string                 `json:"type"`
				Payload    map[string]interface{} `json:"payload"`
				TimeoutSec int                    `json:"timeout_sec"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
				return
			}

			if req.Type == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Command type is required"})
				return
			}

			timeout := 15 * time.Second
			if req.TimeoutSec > 0 {
				timeout = time.Duration(req.TimeoutSec) * time.Second
			}

			cmd := &models.Command{
				ID:        uuid.New().String(),
				DeviceID:  deviceID,
				Type:      req.Type,
				Payload:   req.Payload,
				CreatedAt: time.Now(),
			}

			executedCmd, err := s.hub.SendCommand(cmd, timeout)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]interface{}{
					"error":   err.Error(),
					"command": executedCmd,
				})
				return
			}

			writeJSON(w, http.StatusOK, executedCmd)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

	case "screenshot":
		if r.Method == http.MethodGet {
			cmd := &models.Command{
				ID:        uuid.New().String(),
				DeviceID:  deviceID,
				Type:      "gui_screenshot",
				Payload:   map[string]interface{}{},
				CreatedAt: time.Now(),
			}

			executedCmd, err := s.hub.SendCommand(cmd, 15*time.Second)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]interface{}{
					"error": err.Error(),
				})
				return
			}

			if executedCmd.ExitCode != 0 || executedCmd.Output == "" {
				errMsg := executedCmd.Error
				if errMsg == "" {
					errMsg = "screenshot output was empty"
				}
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
					"error": "Failed to capture screenshot: " + errMsg,
				})
				return
			}

			imgBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(executedCmd.Output))
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
					"error": "Failed to decode screenshot base64 image",
				})
				return
			}

			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", strconv.Itoa(len(imgBytes)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(imgBytes)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	// /api/v1/commands/{id}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || r.Method != http.MethodGet {
		http.Error(w, "Invalid command path or method", http.StatusBadRequest)
		return
	}

	cmdID := parts[3]
	cmd, err := s.db.GetCommand(cmdID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if cmd == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Command not found"})
		return
	}

	writeJSON(w, http.StatusOK, cmd)
}

func (s *Server) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	tools := []map[string]interface{}{
		{
			"name":        "list_devices",
			"description": "Obtiene la lista de todos los dispositivos conectados y registrados con su estado y telemetría.",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "get_device_telemetry",
			"description": "Obtiene la telemetría de un dispositivo (CPU, RAM, ventanas, procesos). Soporta historial o medición fresca en vivo a demanda.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{"type": "string", "description": "ID del dispositivo"},
					"limit":     map[string]interface{}{"type": "integer", "description": "Cantidad de registros históricos"},
					"live":      map[string]interface{}{"type": "boolean", "description": "Si es true, solicita una medición fresca en vivo al dispositivo, la persiste en la base de datos y la entrega."},
				},
				"required": []string{"device_id"},
			},
		},
		{
			"name":        "execute_shell_command",
			"description": "Ejecuta un comando en la terminal remota del cliente y retorna stdout, stderr y código de salida.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{"type": "string", "description": "ID del dispositivo"},
					"command":   map[string]interface{}{"type": "string", "description": "Comando bash/sh a ejecutar"},
					"timeout":   map[string]interface{}{"type": "integer", "description": "Timeout en segundos (opcional)"},
				},
				"required": []string{"device_id", "command"},
			},
		},
		{
			"name":        "control_gui",
			"description": "Controla la interfaz gráfica del dispositivo usando xdotool/wmctrl (mouse, teclado, gestión de ventanas).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{"type": "string", "description": "ID del dispositivo"},
					"action":    map[string]interface{}{"type": "string", "description": "click, move, type, key, list_windows, focus_window, close_window"},
					"x":         map[string]interface{}{"type": "integer", "description": "Coordenada X para mouse"},
					"y":         map[string]interface{}{"type": "integer", "description": "Coordenada Y para mouse"},
					"text":      map[string]interface{}{"type": "string", "description": "Texto a tipear"},
					"key":       map[string]interface{}{"type": "string", "description": "Tecla o atajo (ej. Return, ctrl+c)"},
					"window_id": map[string]interface{}{"type": "string", "description": "ID o título de ventana"},
				},
				"required": []string{"device_id", "action"},
			},
		},
		{
			"name":        "get_device_screenshot",
			"description": "Captura en tiempo real la pantalla del dispositivo cliente y retorna la imagen codificada en base64 (PNG).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{"type": "string", "description": "ID del dispositivo"},
				},
				"required": []string{"device_id"},
			},
		},
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"tools": tools})
}

func (s *Server) handleMCPToolCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	switch req.Name {
	case "list_devices":
		devices, err := s.db.GetDevices()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		for i := range devices {
			if s.hub.IsDeviceOnline(devices[i].ID) {
				devices[i].Status = "online"
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": devices})

	case "get_device_telemetry":
		deviceID, _ := req.Arguments["device_id"].(string)
		isLive, _ := req.Arguments["live"].(bool)

		if isLive {
			cmd := &models.Command{
				ID:        uuid.New().String(),
				DeviceID:  deviceID,
				Type:      "collect_telemetry",
				Payload:   map[string]interface{}{},
				CreatedAt: time.Now(),
			}

			res, err := s.hub.SendCommand(cmd, 15*time.Second)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": "Failed to collect live telemetry: " + err.Error()})
				return
			}

			var metric models.TelemetryMetric
			if err := json.Unmarshal([]byte(res.Output), &metric); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to parse live telemetry: " + err.Error()})
				return
			}

			if metric.DeviceID == "" {
				metric.DeviceID = deviceID
			}
			if metric.RecordedAt.IsZero() {
				metric.RecordedAt = time.Now()
			}

			_ = s.db.InsertTelemetry(&metric)
			_ = s.db.UpdateDeviceStatus(deviceID, "online")

			writeJSON(w, http.StatusOK, map[string]interface{}{"result": metric})
			return
		}

		limit := 10
		if l, ok := req.Arguments["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		metrics, err := s.db.GetTelemetry(deviceID, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": metrics})

	case "execute_shell_command":
		deviceID, _ := req.Arguments["device_id"].(string)
		commandStr, _ := req.Arguments["command"].(string)
		timeoutSec := 15
		if t, ok := req.Arguments["timeout"].(float64); ok && t > 0 {
			timeoutSec = int(t)
		}

		cmd := &models.Command{
			ID:        uuid.New().String(),
			DeviceID:  deviceID,
			Type:      "shell",
			Payload:   map[string]interface{}{"command": commandStr},
			CreatedAt: time.Now(),
		}

		res, err := s.hub.SendCommand(cmd, time.Duration(timeoutSec)*time.Second)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error(), "result": res})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": res})

	case "control_gui":
		deviceID, _ := req.Arguments["device_id"].(string)
		action, _ := req.Arguments["action"].(string)
		delete(req.Arguments, "device_id")

		cmd := &models.Command{
			ID:        uuid.New().String(),
			DeviceID:  deviceID,
			Type:      "gui_" + action,
			Payload:   req.Arguments,
			CreatedAt: time.Now(),
		}

		res, err := s.hub.SendCommand(cmd, 15*time.Second)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error(), "result": res})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": res})

	case "get_device_screenshot":
		deviceID, _ := req.Arguments["device_id"].(string)
		cmd := &models.Command{
			ID:        uuid.New().String(),
			DeviceID:  deviceID,
			Type:      "gui_screenshot",
			Payload:   map[string]interface{}{},
			CreatedAt: time.Now(),
		}

		res, err := s.hub.SendCommand(cmd, 15*time.Second)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error(), "result": res})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"result": map[string]interface{}{
				"device_id":    deviceID,
				"format":       "png",
				"image_base64": res.Output,
			},
		})

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Unknown tool"})
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
