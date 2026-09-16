package ws

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Jeisson005/omni-remote-control/server/internal/db"
	"github.com/Jeisson005/omni-remote-control/server/internal/models"
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
	clients         map[string]*DeviceConn // deviceID -> DeviceConn
	clientsMu       sync.RWMutex
	pendingCommands map[string]chan *models.Command // commandID -> chan
	cmdMu           sync.RWMutex
}

func NewHub(database *db.DB) *Hub {
	return &Hub{
		db:              database,
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
