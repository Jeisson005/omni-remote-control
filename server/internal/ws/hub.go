package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Jeisson005/omni-remote-control/server/internal/db"
	"github.com/Jeisson005/omni-remote-control/server/internal/models"
	"github.com/Jeisson005/omni-remote-control/server/internal/push"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024 * 16,
	WriteBufferSize: 1024 * 16,
	CheckOrigin: func(r *http.Request) bool {
		return true // Permitir orígenes para conexiones de agentes
	},
}

type DeviceConn struct {
	DeviceID string
	Conn     *websocket.Conn
	SendChan chan []byte
	mu       sync.Mutex
}

type Hub struct {
	db              *db.DB
	push            *push.Client
	clients         map[string]*DeviceConn // deviceID -> DeviceConn
	clientsMu       sync.RWMutex
	pendingCommands map[string]chan *models.Command // commandID -> chan
	cmdMu           sync.RWMutex
}

func NewHub(database *db.DB, pushClient *push.Client) *Hub {
	return &Hub{
		db:              database,
		push:            pushClient,
		clients:         make(map[string]*DeviceConn),
		pendingCommands: make(map[string]chan *models.Command),
	}
}

func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Error upgrading to websocket: %v", err)
		return
	}

	devConn := &DeviceConn{
		Conn:     conn,
		SendChan: make(chan []byte, 64),
	}

	go devConn.writePump()
	h.readPump(devConn)
}

func (c *DeviceConn) writePump() {
	defer c.Conn.Close()
	for msg := range c.SendChan {
		c.mu.Lock()
		c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := c.Conn.WriteMessage(websocket.TextMessage, msg)
		c.mu.Unlock()
		if err != nil {
			log.Printf("Error writing message to device %s: %v", c.DeviceID, err)
			return
		}
	}
}

func (h *Hub) readPump(devConn *DeviceConn) {
	defer func() {
		devConn.Conn.Close()
		close(devConn.SendChan)
		if devConn.DeviceID != "" {
			h.clientsMu.Lock()
			delete(h.clients, devConn.DeviceID)
			h.clientsMu.Unlock()

			_ = h.db.UpdateDeviceStatus(devConn.DeviceID, "offline")
			log.Printf("Device disconnected: %s", devConn.DeviceID)
		}
	}()

	devConn.Conn.SetReadLimit(1024 * 1024) // 1MB max
	devConn.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	devConn.Conn.SetPongHandler(func(string) error {
		devConn.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := devConn.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WS error for device %s: %v", devConn.DeviceID, err)
			}
			break
		}

		devConn.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		var wsMsg models.WSMessage
		if err := json.Unmarshal(message, &wsMsg); err != nil {
			log.Printf("Invalid WS message format: %v", err)
			continue
		}

		h.handleMessage(devConn, &wsMsg)
	}
}

func (h *Hub) handleMessage(devConn *DeviceConn, msg *models.WSMessage) {
	switch msg.Type {
	case "register":
		type RegisterPayload struct {
			Device     models.Device           `json:"device"`
			SystemInfo models.DeviceSystemInfo `json:"system_info"`
		}

		var payload RegisterPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			log.Printf("Failed to unmarshal register payload: %v", err)
			return
		}

		devConn.DeviceID = payload.Device.ID
		payload.Device.Status = "online"
		payload.SystemInfo.DeviceID = payload.Device.ID

		if err := h.db.UpsertDevice(&payload.Device); err != nil {
			log.Printf("Error registering device: %v", err)
		}
		if err := h.db.UpsertSystemInfo(&payload.SystemInfo); err != nil {
			log.Printf("Error registering system info: %v", err)
		}

		h.clientsMu.Lock()
		h.clients[devConn.DeviceID] = devConn
		h.clientsMu.Unlock()

		log.Printf("Registered device successfully: %s (%s - %s)", payload.Device.ID, payload.Device.Hostname, payload.Device.OS)

		// Enviar confirmación de registro
		ack, _ := json.Marshal(models.WSMessage{Type: "registered_ack"})
		devConn.SendChan <- ack

	case "telemetry":
		var metric models.TelemetryMetric
		if err := json.Unmarshal(msg.Payload, &metric); err != nil {
			log.Printf("Failed to unmarshal telemetry payload: %v", err)
			return
		}

		if metric.DeviceID == "" {
			metric.DeviceID = devConn.DeviceID
		}

		if err := h.db.InsertTelemetry(&metric); err != nil {
			log.Printf("Error storing telemetry: %v", err)
		}
		_ = h.db.UpdateDeviceStatus(devConn.DeviceID, "online")

	case "event":
		var ev models.DeviceEvent
		if err := json.Unmarshal(msg.Payload, &ev); err != nil {
			log.Printf("Failed to unmarshal event payload: %v", err)
			return
		}

		if ev.DeviceID == "" {
			ev.DeviceID = devConn.DeviceID
		}
		if ev.CreatedAt.IsZero() {
			ev.CreatedAt = time.Now()
		}

		if err := h.db.InsertEvent(&ev); err != nil {
			log.Printf("Error storing event: %v", err)
		} else {
			log.Printf("Device event recorded [%s]: %s (%s) - %s", ev.DeviceID, ev.EventType, ev.Severity, ev.Message)
		}

		if ev.EventType == "client_stopping" {
			_ = h.db.UpdateDeviceStatus(devConn.DeviceID, "offline")
		} else {
			_ = h.db.UpdateDeviceStatus(devConn.DeviceID, "online")
		}

	case "command_response":
		var cmd models.Command
		if err := json.Unmarshal(msg.Payload, &cmd); err != nil {
			log.Printf("Failed to unmarshal command response: %v", err)
			return
		}

		if cmd.ID == "" {
			cmd.ID = msg.CommandID
		}

		_ = h.db.UpdateCommand(&cmd)

		// Recuperar el comando completo con payload original y timestamps
		fullCmd, err := h.db.GetCommand(cmd.ID)
		if err == nil && fullCmd != nil {
			cmd = *fullCmd
		}

		h.cmdMu.Lock()
		if ch, exists := h.pendingCommands[cmd.ID]; exists {
			select {
			case ch <- &cmd:
			default:
			}
			delete(h.pendingCommands, cmd.ID)
		}
		h.cmdMu.Unlock()

	case "fcm_token":
		var payload struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			log.Printf("Failed to unmarshal fcm_token payload: %v", err)
			return
		}
		if payload.Token == "" {
			return
		}
		if err := h.db.UpdateDeviceFCMToken(devConn.DeviceID, payload.Token); err != nil {
			log.Printf("Error storing FCM token for %s: %v", devConn.DeviceID, err)
		} else {
			log.Printf("FCM token updated for device %s", devConn.DeviceID)
		}

	case "notification":
		var n models.NotificationRecord
		if err := json.Unmarshal(msg.Payload, &n); err != nil {
			log.Printf("Failed to unmarshal notification payload: %v", err)
			return
		}
		if n.DeviceID == "" {
			n.DeviceID = devConn.DeviceID
		}
		if err := h.db.InsertNotification(&n); err != nil {
			log.Printf("Error storing notification for %s: %v", devConn.DeviceID, err)
		}

	case "sms":
		var s models.SmsMessage
		if err := json.Unmarshal(msg.Payload, &s); err != nil {
			log.Printf("Failed to unmarshal sms payload: %v", err)
			return
		}
		if s.DeviceID == "" {
			s.DeviceID = devConn.DeviceID
		}
		if err := h.db.InsertSms(&s); err != nil {
			log.Printf("Error storing sms for %s: %v", devConn.DeviceID, err)
		}

	case "notifications_sync":
		var payload struct {
			Notifications []models.NotificationRecord `json:"notifications"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			log.Printf("Failed to unmarshal notifications_sync payload: %v", err)
			return
		}
		for i := range payload.Notifications {
			n := payload.Notifications[i]
			if n.DeviceID == "" {
				n.DeviceID = devConn.DeviceID
			}
			if err := h.db.InsertNotification(&n); err != nil {
				log.Printf("Error storing synced notification: %v", err)
			}
		}
		log.Printf("Synced %d notifications from device %s", len(payload.Notifications), devConn.DeviceID)

	case "sms_sync":
		var payload struct {
			Messages []models.SmsMessage `json:"messages"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			log.Printf("Failed to unmarshal sms_sync payload: %v", err)
			return
		}
		for i := range payload.Messages {
			s := payload.Messages[i]
			if s.DeviceID == "" {
				s.DeviceID = devConn.DeviceID
			}
			if err := h.db.InsertSms(&s); err != nil {
				log.Printf("Error storing synced sms: %v", err)
			}
		}
		log.Printf("Synced %d SMS from device %s", len(payload.Messages), devConn.DeviceID)

	case "ping":
		pong, _ := json.Marshal(models.WSMessage{Type: "pong"})
		select {
		case devConn.SendChan <- pong:
		default:
		}
	}
}

func (h *Hub) SendCommand(cmd *models.Command, timeout time.Duration) (*models.Command, error) {
	h.clientsMu.RLock()
	devConn, exists := h.clients[cmd.DeviceID]
	h.clientsMu.RUnlock()

	if !exists {
		cmd.Status = "failed"
		cmd.Error = "device is offline or not connected"
		_ = h.db.InsertCommand(cmd)
		return cmd, fmt.Errorf("device %s is offline", cmd.DeviceID)
	}

	cmd.Status = "pending"
	if err := h.db.InsertCommand(cmd); err != nil {
		return nil, fmt.Errorf("could not insert command: %w", err)
	}

	respChan := make(chan *models.Command, 1)
	h.cmdMu.Lock()
	h.pendingCommands[cmd.ID] = respChan
	h.cmdMu.Unlock()

	defer func() {
		h.cmdMu.Lock()
		delete(h.pendingCommands, cmd.ID)
		h.cmdMu.Unlock()
	}()

	payloadBytes, _ := json.Marshal(cmd)
	msg := models.WSMessage{
		Type:      "command_request",
		DeviceID:  cmd.DeviceID,
		CommandID: cmd.ID,
		Payload:   payloadBytes,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	select {
	case devConn.SendChan <- msgBytes:
	case <-time.After(3 * time.Second):
		cmd.Status = "failed"
		cmd.Error = "failed to send command to device queue"
		_ = h.db.UpdateCommand(cmd)
		return cmd, fmt.Errorf("timeout dispatching command to device")
	}

	if timeout <= 0 {
		return cmd, nil
	}

	select {
	case res := <-respChan:
		return res, nil
	case <-time.After(timeout):
		cmd.Status = "timeout"
		cmd.Error = fmt.Sprintf("command timed out after %v", timeout)
		_ = h.db.UpdateCommand(cmd)
		return cmd, fmt.Errorf("timeout waiting for device response")
	}
}

func (h *Hub) IsDeviceOnline(deviceID string) bool {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	_, exists := h.clients[deviceID]
	return exists
}

// WakeDevice envía un push FCM de alta prioridad al dispositivo para que abra
// su sesión WebSocket (útil en clientes Android Doze-friendly). Es no-op si el
// dispositivo ya está online o si FCM no está configurado.
func (h *Hub) WakeDevice(deviceID string) {
	if h.IsDeviceOnline(deviceID) {
		return
	}
	if !h.push.Enabled() {
		return
	}

	token, err := h.db.GetDeviceFCMToken(deviceID)
	if err != nil {
		log.Printf("Could not retrieve FCM token for %s: %v", deviceID, err)
		return
	}
	if token == "" {
		log.Printf("Device %s has no FCM token registered; cannot wake remotely", deviceID)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	if err := h.push.SendWake(ctx, token, map[string]string{"action": "START_CONTROL"}); err != nil {
		log.Printf("Failed to send wake push to %s: %v", deviceID, err)
		return
	}
	log.Printf("Wake push sent to device %s", deviceID)
}

// EnsureOnline despierta al dispositivo (si es necesario) y espera hasta que su
// conexión WebSocket esté registrada, o hasta agotar el timeout.
func (h *Hub) EnsureOnline(deviceID string, timeout time.Duration) bool {
	if h.IsDeviceOnline(deviceID) {
		return true
	}

	h.WakeDevice(deviceID)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h.IsDeviceOnline(deviceID) {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return h.IsDeviceOnline(deviceID)
}
