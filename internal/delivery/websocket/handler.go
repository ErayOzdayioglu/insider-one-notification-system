package websocket

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
)

// upgrader specifies the parameters for upgrading an HTTP connection to a
// WebSocket connection.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// CheckOrigin allows connections from any origin. In production this
	// should be locked down to trusted origins.
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// HandleWebSocket returns an http.HandlerFunc that upgrades the connection
// to WebSocket, registers a new client with the Hub, and starts the
// read/write pumps.
//
// Route: GET /ws/notifications
func HandleWebSocket(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("websocket: upgrade failed", "error", err)
			return
		}

		client := NewClient(hub, conn)
		hub.register <- client

		// Start the write pump in a new goroutine; the read pump runs in
		// the current goroutine (blocks until the connection closes).
		go client.WritePump()
		client.ReadPump()
	}
}
