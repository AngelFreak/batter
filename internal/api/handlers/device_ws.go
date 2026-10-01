package handlers

import (
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/XpertaDK/batter/internal/api/middleware"
	"github.com/XpertaDK/batter/internal/device"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	ws "github.com/gorilla/websocket"
)

// WebSocket limits. Control messages are small JSON, except a clipboard
// paste (scrcpy caps clipboard text at ~256 KiB, which JSON can inflate);
// the video socket only ever receives control frames from the client.
const (
	maxControlMessage     = 1 << 20
	maxVideoClientMessage = 4 << 10
	// videoSendQueue bounds unsent video in the kernel per viewer (~0.25s
	// at 4 Mbps), so lag shows up as blocked writes the session can react to.
	videoSendQueue = 128 << 10
)

// DeviceWSHandler handles WebSocket connections for device video/control.
type DeviceWSHandler struct {
	deviceManager *device.Manager
	logger        *slog.Logger
	upgrader      ws.Upgrader

	// Keepalive: the server pings every pingPeriod and drops a client that
	// hasn't answered (or sent anything) within pongWait, so half-open
	// connections (a laptop leaving the network) are noticed. writeWait
	// bounds each write.
	pingPeriod, pongWait, writeWait time.Duration
}

// NewDeviceWSHandler creates a new device WebSocket handler. allowedOrigins is
// the same origin policy used for CORS (empty = same-origin only); the
// WebSocket upgrader enforces it so a
// page on an untrusted origin cannot open a control socket with a stolen-from-
// the-tab JWT (cross-site WebSocket hijacking).
func NewDeviceWSHandler(dm *device.Manager, logger *slog.Logger, allowedOrigins []string) *DeviceWSHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DeviceWSHandler{
		deviceManager: dm,
		logger:        logger.With("handler", "device-ws"),
		pingPeriod:    25 * time.Second,
		pongWait:      60 * time.Second,
		writeWait:     10 * time.Second,
		upgrader: ws.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 1024 * 1024, // 1MB for video frames
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				// Non-browser clients (no Origin header) are gated by JWT auth.
				if origin == "" {
					return true
				}
				return middleware.IsOriginAllowed(origin, r, allowedOrigins)
			},
		},
	}
}

// keepAlive arms conn's read deadline, extends it on every pong, and pings
// the client until done is closed.
func (h *DeviceWSHandler) keepAlive(conn *ws.Conn, done <-chan struct{}) {
	_ = conn.SetReadDeadline(time.Now().Add(h.pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(h.pongWait))
	})
	go func() {
		ticker := time.NewTicker(h.pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := conn.WriteControl(ws.PingMessage, nil, time.Now().Add(h.writeWait)); err != nil {
					return
				}
			}
		}
	}()
}

// write sends one message, giving up after writeWait.
func (h *DeviceWSHandler) write(conn *ws.Conn, kind int, data []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(h.writeWait))
	return conn.WriteMessage(kind, data)
}

// VideoStream handles WebSocket connections for video streaming.
func (h *DeviceWSHandler) VideoStream(c *gin.Context) {
	serial := c.Param("serial")

	session := h.deviceManager.GetSession(serial)
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active session for device"})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("failed to upgrade video websocket", "error", err)
		return
	}
	defer conn.Close()

	clientID := uuid.New().String()
	h.logger.Info("video client connected", "serial", serial, "client", clientID)

	// Subscribe to video
	videoCh := session.SubscribeVideo(clientID)
	defer session.UnsubscribeVideo(clientID)

	conn.SetReadLimit(maxVideoClientMessage)
	limitSendQueue(conn.UnderlyingConn(), videoSendQueue)
	done := make(chan struct{})
	defer close(done)
	h.keepAlive(conn, done)

	// Send stored SPS/PPS config packet first so decoder can initialize
	if config := session.GetConfigPacket(); config != nil {
		if err := h.write(conn, ws.BinaryMessage, config); err != nil {
			h.logger.Error("failed to send config packet", "error", err)
			return
		}
	}
	// A decoder can't start without a keyframe, and a still screen produces
	// no frames at all, so ask for one now rather than show a black canvas.
	if err := session.RequestKeyframe(); err != nil {
		h.logger.Warn("failed to request keyframe", "serial", serial, "error", err)
	}

	// The read loop notices the client leaving (close, error, or no pong
	// within pongWait), which must end the stream even on a still screen
	// where no frame write would ever fail.
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(h.pongWait))
		}
	}()

	for {
		select {
		case <-gone:
			h.logger.Debug("video client disconnected", "serial", serial, "client", clientID)
			return
		case msg, ok := <-videoCh:
			if !ok {
				return
			}
			if err := h.write(conn, ws.BinaryMessage, msg); err != nil {
				h.logger.Debug("video client disconnected", "serial", serial, "client", clientID)
				return
			}
		}
	}
}

// ControlMessage represents a control input from the browser.
type ControlMessage struct {
	Type      string  `json:"type"`
	Action    uint8   `json:"action"`
	X         float32 `json:"x"`
	Y         float32 `json:"y"`
	PointerID uint64  `json:"pointer_id"`
	Pressure  float32 `json:"pressure"`
	Keycode   uint32  `json:"keycode"`
	Repeat    uint32  `json:"repeat"`
	Metastate uint32  `json:"metastate"`
	Text      string  `json:"text"`
	ScrollH   int32   `json:"scroll_h"`
	ScrollV   int32   `json:"scroll_v"`
	Paste     bool    `json:"paste"`
}

// ControlStream handles WebSocket connections for control input.
func (h *DeviceWSHandler) ControlStream(c *gin.Context) {
	serial := c.Param("serial")

	session := h.deviceManager.GetSession(serial)
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active session for device"})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("failed to upgrade control websocket", "error", err)
		return
	}
	defer conn.Close()

	clientID := uuid.New().String()
	h.logger.Info("control client connected", "serial", serial, "client", clientID)

	// Claim control
	if !session.ClaimControl(clientID) {
		_ = conn.WriteMessage(ws.TextMessage, []byte(`{"error":"control already claimed"}`))
		return
	}
	defer session.ReleaseControl(clientID)

	conn.SetReadLimit(maxControlMessage)

	// Write mutex for concurrent WebSocket writes (clipboard relay + control responses)
	var wsMu sync.Mutex
	done := make(chan struct{})
	defer close(done)
	h.keepAlive(conn, done)

	// Clipboard relay goroutine: reads device clipboard and sends to WebSocket client
	go func() {
		clipCh := session.ClipboardCh()
		for {
			select {
			case <-done:
				return
			case text, ok := <-clipCh:
				if !ok {
					return
				}
				msg, _ := json.Marshal(map[string]string{"type": "clipboard", "text": text})
				wsMu.Lock()
				err := h.write(conn, ws.TextMessage, msg)
				wsMu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

	width := uint16(session.Width)
	height := uint16(session.Height)

	for {
		_, msgData, err := conn.ReadMessage()
		if err != nil {
			h.logger.Debug("control client disconnected", "serial", serial, "client", clientID, "error", err)
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(h.pongWait))

		var msg ControlMessage
		if err := json.Unmarshal(msgData, &msg); err != nil {
			h.logger.Debug("invalid control message", "error", err)
			continue
		}

		var encoded []byte
		switch msg.Type {
		case "touch":
			pressure := uint16(0xFFFF)
			if msg.Pressure > 0 {
				pressure = uint16(msg.Pressure * float32(math.MaxUint16))
			}
			if msg.Action == device.ActionUp {
				pressure = 0
			}
			encoded = device.EncodeTouchEvent(msg.Action, msg.PointerID, msg.X, msg.Y, width, height, pressure)

		case "key":
			encoded = device.EncodeKeyEvent(msg.Action, msg.Keycode, msg.Repeat, msg.Metastate)

		case "text":
			if msg.Text != "" {
				encoded = device.EncodeTextEvent(msg.Text)
			}

		case "scroll":
			encoded = device.EncodeScrollEvent(msg.X, msg.Y, width, height, msg.ScrollH, msg.ScrollV)

		case "back":
			encoded = device.EncodeBackOrScreenOn(msg.Action)

		case "screen_on", "wake":
			for _, kc := range []uint32{device.KeycodeWakeUp, device.KeycodePower} {
				down := device.EncodeKeyEvent(device.ActionDown, kc, 0, 0)
				if err := session.WriteControl(down); err != nil {
					h.logger.Warn("failed to write wake control", "error", err)
				}
				up := device.EncodeKeyEvent(device.ActionUp, kc, 0, 0)
				if err := session.WriteControl(up); err != nil {
					h.logger.Warn("failed to write wake control", "error", err)
				}
			}
			continue

		case "screen_off":
			encoded = device.EncodeKeyEvent(device.ActionDown, device.KeycodeSleep, 0, 0)
			if err := session.WriteControl(encoded); err != nil {
				h.logger.Warn("failed to write screen_off control", "error", err)
			}
			encoded = device.EncodeKeyEvent(device.ActionUp, device.KeycodeSleep, 0, 0)

		case "set_clipboard":
			if msg.Text != "" {
				encoded = device.EncodeSetClipboard(0, msg.Text, msg.Paste)
			}

		case "get_clipboard":
			encoded = device.EncodeGetClipboard(device.CopyKeyCopy)

		default:
			h.logger.Debug("unknown control type", "type", msg.Type)
			continue
		}

		if encoded != nil {
			if err := session.WriteControl(encoded); err != nil {
				if !session.IsAlive() {
					h.logger.Info("session dead, closing control websocket", "serial", serial)
					return
				}
				h.logger.Warn("failed to write control", "error", err)
			}
		}
	}
}
