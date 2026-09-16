package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	mux.HandleFunc("/api/v1/telemetry", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var metric models.TelemetryMetric
		if err := json.NewDecoder(r.Body).Decode(&metric); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid telemetry payload: " + err.Error()})
			return
		}
		if metric.DeviceID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device_id is required"})
			return
		}
		if metric.RecordedAt.IsZero() {
			metric.RecordedAt = time.Now()
		}
		if err := s.db.InsertTelemetry(&metric); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to store telemetry: " + err.Error()})
			return
		}
		_ = s.db.UpdateDeviceStatus(metric.DeviceID, "online")
		writeJSON(w, http.StatusCreated, map[string]interface{}{"status": "success", "metric": metric})
	})

	// Ingesta directa de notificaciones y SMS (clientes Android con WorkManager/BroadcastReceiver)
	mux.HandleFunc("/api/v1/notifications", s.handleNotificationIngest)
	mux.HandleFunc("/api/v1/sms", s.handleSmsIngest)

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
				output, err := s.executeDeviceCommand(deviceID, "collect_telemetry", map[string]interface{}{})
				if err != nil {
					writeJSON(w, http.StatusBadGateway, map[string]interface{}{
						"error": "Failed to collect live telemetry: " + err.Error(),
					})
					return
				}

				var metric models.TelemetryMetric
				if err := json.Unmarshal([]byte(output), &metric); err != nil {
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
		} else if r.Method == http.MethodPost {
			var metric models.TelemetryMetric
			if err := json.NewDecoder(r.Body).Decode(&metric); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid telemetry payload: " + err.Error()})
				return
			}
			if metric.DeviceID == "" {
				metric.DeviceID = deviceID
			}
			if metric.RecordedAt.IsZero() {
				metric.RecordedAt = time.Now()
			}
			if err := s.db.InsertTelemetry(&metric); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to store telemetry: " + err.Error()})
				return
			}
			_ = s.db.UpdateDeviceStatus(deviceID, "online")
			writeJSON(w, http.StatusCreated, map[string]interface{}{"status": "success", "metric": metric})
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

	case "events":
		if r.Method == http.MethodGet {
			limitStr := r.URL.Query().Get("limit")
			limit := 50
			if limitStr != "" {
				if l, err := strconv.Atoi(limitStr); err == nil {
					limit = l
				}
			}
			eventType := r.URL.Query().Get("type")
			events, err := s.db.GetEvents(deviceID, eventType, limit)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, events)
			return
		} else if r.Method == http.MethodPost {
			var ev models.DeviceEvent
			if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid event payload: " + err.Error()})
				return
			}
			if ev.DeviceID == "" {
				ev.DeviceID = deviceID
			}
			if ev.CreatedAt.IsZero() {
				ev.CreatedAt = time.Now()
			}
			if err := s.db.InsertEvent(&ev); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to store event: " + err.Error()})
				return
			}
			_ = s.db.UpdateDeviceStatus(deviceID, "online")
			writeJSON(w, http.StatusCreated, map[string]interface{}{"status": "success", "event": ev})
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

	case "notifications":
		// /api/v1/devices/{id}/notifications/action
		if len(parts) > 5 && parts[5] == "action" {
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req struct {
				Key         string `json:"key"`
				ActionIndex int    `json:"action_index"`
				ReplyText   string `json:"reply_text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
				return
			}
			if req.Key == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "notification key is required"})
				return
			}
			output, err := s.executeDeviceCommand(deviceID, "notification_action", map[string]interface{}{
				"key":          req.Key,
				"action_index": req.ActionIndex,
				"reply_text":   req.ReplyText,
			})
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": "success", "result": output})
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		limit := parseLimit(r, 50)
		isLive := r.URL.Query().Get("live") == "true" || r.URL.Query().Get("live") == "1" ||
			(len(parts) > 5 && parts[5] == "live")

		if isLive {
			records, err := s.collectLiveNotifications(deviceID, limit)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, records)
			return
		}

		records, err := s.db.GetNotifications(deviceID, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, records)
		return

	case "sms":
		if r.Method == http.MethodPost {
			var req struct {
				Address string `json:"address"`
				Body    string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
				return
			}
			if req.Address == "" || req.Body == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "address and body are required"})
				return
			}
			output, err := s.executeDeviceCommand(deviceID, "send_sms", map[string]interface{}{
				"address": req.Address,
				"body":    req.Body,
			})
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": "success", "result": output})
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		limit := parseLimit(r, 50)
		isLive := r.URL.Query().Get("live") == "true" || r.URL.Query().Get("live") == "1" ||
			(len(parts) > 5 && parts[5] == "live")

		if isLive {
			messages, err := s.collectLiveSms(deviceID, limit)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, messages)
			return
		}

		messages, err := s.db.GetSmsMessages(deviceID, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, messages)
		return

	case "fcm-token":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token is required"})
			return
		}
		if err := s.db.UpdateDeviceFCMToken(deviceID, req.Token); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
		return

	case "unlock", "lock", "wake":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		commandType := map[string]string{
			"unlock": "unlock_device",
			"lock":   "lock_device",
			"wake":   "wake_device",
		}[action]
		output, err := s.executeDeviceCommand(deviceID, commandType, map[string]interface{}{})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "success", "result": output})
		return

	case "mode":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
			return
		}
		if req.Mode != "auto" && req.Mode != "consent" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be 'auto' or 'consent'"})
			return
		}
		output, err := s.executeDeviceCommand(deviceID, "set_control_mode", map[string]interface{}{"mode": req.Mode})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "success", "result": output})
		return

	case "permissions":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Package    string `json:"package"`
			Permission string `json:"permission"`
			Grant      bool   `json:"grant"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
			return
		}
		if req.Package == "" || req.Permission == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "package and permission are required"})
			return
		}
		commandType := "grant_permission"
		if !req.Grant {
			commandType = "revoke_permission"
		}
		output, err := s.executeDeviceCommand(deviceID, commandType, map[string]interface{}{
			"package":    req.Package,
			"permission": req.Permission,
		})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "success", "result": output})
		return

	default:
		http.NotFound(w, r)
	}
}

// handleNotificationIngest recibe notificaciones reenviadas de forma directa
// (HTTP POST) por el cliente Android, incluso sin sesión WebSocket activa.
func (s *Server) handleNotificationIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var n models.NotificationRecord
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid notification payload: " + err.Error()})
		return
	}
	if n.DeviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device_id is required"})
		return
	}
	if err := s.db.InsertNotification(&n); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to store notification: " + err.Error()})
		return
	}
	_ = s.db.UpdateDeviceStatus(n.DeviceID, "online")
	writeJSON(w, http.StatusCreated, map[string]interface{}{"status": "success", "notification": n})
}

// handleSmsIngest recibe SMS reenviados de forma directa por el cliente Android.
func (s *Server) handleSmsIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var m models.SmsMessage
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid sms payload: " + err.Error()})
		return
	}
	if m.DeviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device_id is required"})
		return
	}
	if err := s.db.InsertSms(&m); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to store sms: " + err.Error()})
		return
	}
	_ = s.db.UpdateDeviceStatus(m.DeviceID, "online")
	writeJSON(w, http.StatusCreated, map[string]interface{}{"status": "success", "sms": m})
}

func (s *Server) executeDeviceCommand(deviceID, commandType string, payload map[string]interface{}) (string, error) {
	if !s.hub.EnsureOnline(deviceID, 25*time.Second) {
		return "", fmt.Errorf("device %s is offline and could not be woken", deviceID)
	}

	cmd := &models.Command{
		ID:        uuid.New().String(),
		DeviceID:  deviceID,
		Type:      commandType,
		Payload:   payload,
		CreatedAt: time.Now(),
	}

	res, err := s.hub.SendCommand(cmd, 25*time.Second)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		errMsg := res.Error
		if errMsg == "" {
			errMsg = "device returned a non-zero exit code"
		}
		return "", fmt.Errorf("%s", errMsg)
	}
	return res.Output, nil
}

func (s *Server) collectLiveNotifications(deviceID string, limit int) ([]models.NotificationRecord, error) {
	output, err := s.executeDeviceCommand(deviceID, "get_notifications", map[string]interface{}{"limit": limit})
	if err != nil {
		return nil, err
	}

	var records []models.NotificationRecord
	if err := json.Unmarshal([]byte(output), &records); err != nil {
		return nil, fmt.Errorf("failed to parse live notifications: %w", err)
	}

	for i := range records {
		if records[i].DeviceID == "" {
			records[i].DeviceID = deviceID
		}
		_ = s.db.InsertNotification(&records[i])
	}
	_ = s.db.UpdateDeviceStatus(deviceID, "online")
	return records, nil
}

func (s *Server) collectLiveSms(deviceID string, limit int) ([]models.SmsMessage, error) {
	output, err := s.executeDeviceCommand(deviceID, "get_sms", map[string]interface{}{"limit": limit})
	if err != nil {
		return nil, err
	}

	var messages []models.SmsMessage
	if err := json.Unmarshal([]byte(output), &messages); err != nil {
		return nil, fmt.Errorf("failed to parse live sms: %w", err)
	}

	for i := range messages {
		if messages[i].DeviceID == "" {
			messages[i].DeviceID = deviceID
		}
		_ = s.db.InsertSms(&messages[i])
	}
	_ = s.db.UpdateDeviceStatus(deviceID, "online")
	return messages, nil
}

func parseLimit(r *http.Request, fallback int) int {
	limitStr := r.URL.Query().Get("limit")
	if limitStr == "" {
		return fallback
	}
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		return l
	}
	return fallback
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
		{
			"name":        "get_device_events",
			"description": "Consulta eventos de ciclo de vida y red de un dispositivo (inicio, apagado, desconexiones, cambios de red, alertas de batería).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id":  map[string]interface{}{"type": "string", "description": "ID del dispositivo"},
					"event_type": map[string]interface{}{"type": "string", "description": "Filtrar por tipo de evento (ej. client_started, client_stopping, network_changed, battery_low)"},
					"limit":      map[string]interface{}{"type": "integer", "description": "Cantidad máxima de eventos (default 20)"},
				},
				"required": []string{"device_id"},
			},
		},
		{
			"name":        "get_device_notifications",
			"description": "Consulta las notificaciones capturadas de otras apps en un dispositivo Android (WhatsApp, correo, bancos, etc.). Con live=true despierta al dispositivo vía FCM y obtiene además las notificaciones activas en tiempo real.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{"type": "string", "description": "ID del dispositivo Android"},
					"limit":     map[string]interface{}{"type": "integer", "description": "Cantidad máxima de notificaciones (default 50)"},
					"live":      map[string]interface{}{"type": "boolean", "description": "Si es true, despierta el dispositivo y lee las notificaciones activas ahora mismo."},
				},
				"required": []string{"device_id"},
			},
		},
		{
			"name":        "get_device_sms",
			"description": "Consulta los SMS de un dispositivo Android (entrantes reenviados e historial de la bandeja de entrada). Con live=true despierta al dispositivo vía FCM y extrae el historial completo vía READ_SMS.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{"type": "string", "description": "ID del dispositivo Android"},
					"limit":     map[string]interface{}{"type": "integer", "description": "Cantidad máxima de mensajes (default 50)"},
					"live":      map[string]interface{}{"type": "boolean", "description": "Si es true, despierta el dispositivo y lee la bandeja de entrada ahora mismo."},
				},
				"required": []string{"device_id"},
			},
		},
		{
			"name":        "send_device_sms",
			"description": "Envía un SMS desde un dispositivo Android remoto (requiere permiso SEND_SMS).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{"type": "string", "description": "ID del dispositivo Android"},
					"address":   map[string]interface{}{"type": "string", "description": "Número de teléfono destino"},
					"body":      map[string]interface{}{"type": "string", "description": "Contenido del mensaje"},
				},
				"required": []string{"device_id", "address", "body"},
			},
		},
		{
			"name":        "notification_action",
			"description": "Ejecuta un botón de acción de una notificación Android (por ejemplo 'Responder' o 'Marcar como leído') identificado por su key y el índice de acción.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id":    map[string]interface{}{"type": "string", "description": "ID del dispositivo Android"},
					"key":          map[string]interface{}{"type": "string", "description": "Key de la notificación (campo external_id)"},
					"action_index": map[string]interface{}{"type": "integer", "description": "Índice del botón de acción a ejecutar (default 0)"},
					"reply_text":   map[string]interface{}{"type": "string", "description": "Texto opcional para acciones de respuesta directa (RemoteInput)"},
				},
				"required": []string{"device_id", "key"},
			},
		},
		{
			"name":        "unlock_device",
			"description": "Desbloquea la pantalla de un dispositivo Android (Device Owner, Shizuku/locksettings o patrón local). Requiere que el dispositivo ya tenga una estrategia de desbloqueo configurada.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{"type": "string", "description": "ID del dispositivo Android"},
				},
				"required": []string{"device_id"},
			},
		},
		{
			"name":        "lock_device",
			"description": "Bloquea la pantalla de un dispositivo Android (Device Admin o Shizuku).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{"type": "string", "description": "ID del dispositivo Android"},
				},
				"required": []string{"device_id"},
			},
		},
		{
			"name":        "wake_device",
			"description": "Enciende la pantalla de un dispositivo Android sin desbloquearlo.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{"type": "string", "description": "ID del dispositivo Android"},
				},
				"required": []string{"device_id"},
			},
		},
		{
			"name":        "grant_device_permission",
			"description": "Concede o revoca un permiso peligroso en un dispositivo Android de forma silenciosa usando Shizuku (pm grant/revoke).",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id":  map[string]interface{}{"type": "string", "description": "ID del dispositivo Android"},
					"package":    map[string]interface{}{"type": "string", "description": "Paquete destino (ej. com.example.app)"},
					"permission": map[string]interface{}{"type": "string", "description": "Permiso (ej. android.permission.CAMERA)"},
					"grant":      map[string]interface{}{"type": "boolean", "description": "true para conceder, false para revocar"},
				},
				"required": []string{"device_id", "package", "permission"},
			},
		},
		{
			"name":        "set_device_control_mode",
			"description": "Cambia el modo de control de un dispositivo Android ('auto' desatendido o 'consent' con aprobación del usuario). Requiere que el dispositivo permita cambios remotos de modo.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"device_id": map[string]interface{}{"type": "string", "description": "ID del dispositivo Android"},
					"mode":      map[string]interface{}{"type": "string", "description": "'auto' o 'consent'"},
				},
				"required": []string{"device_id", "mode"},
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
			output, err := s.executeDeviceCommand(deviceID, "collect_telemetry", map[string]interface{}{})
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": "Failed to collect live telemetry: " + err.Error()})
				return
			}

			var metric models.TelemetryMetric
			if err := json.Unmarshal([]byte(output), &metric); err != nil {
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

	case "get_device_events":
		deviceID, _ := req.Arguments["device_id"].(string)
		eventType, _ := req.Arguments["event_type"].(string)
		limit := 20
		if l, ok := req.Arguments["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		events, err := s.db.GetEvents(deviceID, eventType, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": events})

	case "get_device_notifications":
		deviceID, _ := req.Arguments["device_id"].(string)
		isLive, _ := req.Arguments["live"].(bool)
		limit := 50
		if l, ok := req.Arguments["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}

		if isLive {
			records, err := s.collectLiveNotifications(deviceID, limit)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"result": records})
			return
		}

		records, err := s.db.GetNotifications(deviceID, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": records})

	case "get_device_sms":
		deviceID, _ := req.Arguments["device_id"].(string)
		isLive, _ := req.Arguments["live"].(bool)
		limit := 50
		if l, ok := req.Arguments["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}

		if isLive {
			messages, err := s.collectLiveSms(deviceID, limit)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"result": messages})
			return
		}

		messages, err := s.db.GetSmsMessages(deviceID, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": messages})

	case "send_device_sms":
		deviceID, _ := req.Arguments["device_id"].(string)
		address, _ := req.Arguments["address"].(string)
		body, _ := req.Arguments["body"].(string)
		if address == "" || body == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "address and body are required"})
			return
		}
		output, err := s.executeDeviceCommand(deviceID, "send_sms", map[string]interface{}{
			"address": address,
			"body":    body,
		})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": output})

	case "notification_action":
		deviceID, _ := req.Arguments["device_id"].(string)
		key, _ := req.Arguments["key"].(string)
		actionIndex := 0
		if idx, ok := req.Arguments["action_index"].(float64); ok {
			actionIndex = int(idx)
		}
		replyText, _ := req.Arguments["reply_text"].(string)
		if key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
			return
		}
		output, err := s.executeDeviceCommand(deviceID, "notification_action", map[string]interface{}{
			"key":          key,
			"action_index": actionIndex,
			"reply_text":   replyText,
		})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": output})

	case "unlock_device", "lock_device", "wake_device":
		deviceID, _ := req.Arguments["device_id"].(string)
		commandType := map[string]string{
			"unlock_device": "unlock_device",
			"lock_device":   "lock_device",
			"wake_device":   "wake_device",
		}[req.Name]
		output, err := s.executeDeviceCommand(deviceID, commandType, map[string]interface{}{})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": output})

	case "grant_device_permission":
		deviceID, _ := req.Arguments["device_id"].(string)
		pkg, _ := req.Arguments["package"].(string)
		permission, _ := req.Arguments["permission"].(string)
		grant, _ := req.Arguments["grant"].(bool)
		if pkg == "" || permission == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "package and permission are required"})
			return
		}
		commandType := "grant_permission"
		if !grant {
			commandType = "revoke_permission"
		}
		output, err := s.executeDeviceCommand(deviceID, commandType, map[string]interface{}{
			"package":    pkg,
			"permission": permission,
		})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": output})

	case "set_device_control_mode":
		deviceID, _ := req.Arguments["device_id"].(string)
		mode, _ := req.Arguments["mode"].(string)
		if mode != "auto" && mode != "consent" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be 'auto' or 'consent'"})
			return
		}
		output, err := s.executeDeviceCommand(deviceID, "set_control_mode", map[string]interface{}{"mode": mode})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"result": output})

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Unknown tool"})
	}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
