package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	ws "github.com/gorilla/websocket"
)

// audioReadyTimeout bounds the wait for the device to start (or refuse) audio.
const audioReadyTimeout = 10 * time.Second

// AudioStream streams a device's audio over a WebSocket. The first message is
// a JSON device.AudioStatus. If audio is available, binary messages follow:
// scrcpy packets (12-byte header: PTS with config/key flags in the top bits,
// then size; then Opus data), starting with the Opus config packet. If not,
// the socket closes normally after the status. Video never depends on this.
func (h *DeviceWSHandler) AudioStream(c *gin.Context) {
	serial := c.Param("serial")

	session := h.deviceManager.GetSession(serial)
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no active session for device"})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("failed to upgrade audio websocket", "error", err)
		return
	}
	defer conn.Close()

	// Read loop to detect client disconnect, which also ends the wait below.
	// Listeners only send control frames; the read loop notices them
	// leaving (close, error, or no pong within pongWait).
	conn.SetReadLimit(maxVideoClientMessage)
	done := make(chan struct{})
	defer close(done)
	h.keepAlive(conn, done)
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

	ctx, cancel := context.WithTimeout(context.Background(), audioReadyTimeout)
	go func() {
		select {
		case <-gone:
			cancel() // stop waiting for a listener that left
		case <-ctx.Done():
		}
	}()
	status := session.WaitAudio(ctx)
	cancel()

	msg, _ := json.Marshal(status)
	if err := h.write(conn, ws.TextMessage, msg); err != nil {
		return
	}
	if !status.Available {
		_ = conn.WriteControl(ws.CloseMessage, ws.FormatCloseMessage(ws.CloseNormalClosure, "audio unavailable"), time.Now().Add(h.writeWait))
		return
	}

	clientID := uuid.New().String()
	h.logger.Info("audio client connected", "serial", serial, "client", clientID)
	audioCh := session.SubscribeAudio(clientID)
	defer session.UnsubscribeAudio(clientID)

	if config := session.GetAudioConfigPacket(); config != nil {
		if err := h.write(conn, ws.BinaryMessage, config); err != nil {
			return
		}
	}
	for {
		select {
		case <-gone:
			h.logger.Debug("audio client disconnected", "serial", serial, "client", clientID)
			return
		case msg, ok := <-audioCh:
			if !ok {
				return
			}
			if err := h.write(conn, ws.BinaryMessage, msg); err != nil {
				h.logger.Debug("audio client disconnected", "serial", serial, "client", clientID)
				return
			}
		}
	}
}
