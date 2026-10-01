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
	ctx, cancel := context.WithTimeout(context.Background(), audioReadyTimeout)
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	status := session.WaitAudio(ctx)
	cancel()

	msg, _ := json.Marshal(status)
	if err := conn.WriteMessage(ws.TextMessage, msg); err != nil {
		return
	}
	if !status.Available {
		_ = conn.WriteMessage(ws.CloseMessage, ws.FormatCloseMessage(ws.CloseNormalClosure, "audio unavailable"))
		return
	}

	clientID := uuid.New().String()
	h.logger.Info("audio client connected", "serial", serial, "client", clientID)
	audioCh := session.SubscribeAudio(clientID)
	defer session.UnsubscribeAudio(clientID)

	if config := session.GetAudioConfigPacket(); config != nil {
		if err := conn.WriteMessage(ws.BinaryMessage, config); err != nil {
			return
		}
	}
	for msg := range audioCh {
		if err := conn.WriteMessage(ws.BinaryMessage, msg); err != nil {
			h.logger.Debug("audio client disconnected", "serial", serial, "client", clientID)
			return
		}
	}
}
