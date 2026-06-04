package api

import (
	"context"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

// handleConsole upgrades to a WebSocket and bridges the connection into the
// VM's broker (same fan-out semantics as the Unix-socket and PTY endpoints).
func (s *Server) handleConsole(c *websocket.Conn) {
	defer c.Close()

	id := c.Params("id")
	broker, err := s.mgr.SubscribeConsole(id)
	if err != nil {
		_ = c.WriteMessage(websocket.TextMessage, []byte("error: "+err.Error()))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := broker.Subscribe()
	defer broker.Unsubscribe(ch)

	// VM -> WebSocket
	go func() {
		for data := range ch {
			if err := c.WriteMessage(websocket.BinaryMessage, data); err != nil {
				cancel()
				return
			}
		}
	}()

	// WebSocket -> VM (blocks until client disconnects)
	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			return
		}
		broker.Send(ctx, data)
	}
}

// consoleUpgrade rejects non-websocket requests before the upgrade handler runs.
func consoleUpgrade(c *fiber.Ctx) error {
	if websocket.IsWebSocketUpgrade(c) {
		c.Locals("allowed", true)
		return c.Next()
	}
	return fiber.ErrUpgradeRequired
}
