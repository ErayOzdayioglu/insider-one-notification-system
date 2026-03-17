package websocket

import (
	"context"
	"log/slog"
	"sync"

	redispkg "github.com/erayozdayioglu/insider-one-notification-system/internal/infrastructure/redis"
)

// Hub maintains the set of active WebSocket clients and broadcasts incoming
// Redis Pub/Sub messages to all of them.
type Hub struct {
	// clients holds all currently registered clients.
	clients map[*Client]struct{}

	// register requests from new clients.
	register chan *Client

	// unregister requests from disconnecting clients.
	unregister chan *Client

	// pubsub is the Redis Pub/Sub abstraction used to receive notification
	// status updates.
	pubsub redispkg.PubSub

	// mu protects the clients map during Stop.
	mu sync.Mutex
}

// NewHub creates a new Hub backed by the provided Redis PubSub.
func NewHub(pubsub redispkg.PubSub) *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		pubsub:     pubsub,
	}
}

// Start subscribes to the Redis "ws:notifications" channel and runs the
// main event loop. It blocks until ctx is cancelled.
func (h *Hub) Start(ctx context.Context) {
	messages, cancel, err := h.pubsub.Subscribe(ctx, redispkg.NotificationsChannel())
	if err != nil {
		slog.Error("websocket hub: failed to subscribe to redis pub/sub", "error", err)
		return
	}
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = struct{}{}
			h.mu.Unlock()
			slog.Debug("websocket hub: client registered", "remote_addr", client.conn.RemoteAddr())

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			slog.Debug("websocket hub: client unregistered", "remote_addr", client.conn.RemoteAddr())

		case msg, ok := <-messages:
			if !ok {
				// Redis subscription channel was closed.
				slog.Warn("websocket hub: redis subscription channel closed")
				return
			}
			h.broadcast(msg)
		}
	}
}

// broadcast sends a message to every connected client. Clients whose send
// buffer is full are removed to prevent a slow consumer from blocking the
// hub.
func (h *Hub) broadcast(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for client := range h.clients {
		select {
		case client.send <- msg:
		default:
			// Client send buffer is full; drop the connection.
			delete(h.clients, client)
			close(client.send)
		}
	}
}

// Stop closes all client connections and drains the hub.
func (h *Hub) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for client := range h.clients {
		close(client.send)
		delete(h.clients, client)
	}
}
