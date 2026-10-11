package infraws

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const writeWait = 10 * time.Second
const pongWait = 60 * time.Second
const pingPeriod = 30 * time.Second

type PresenceStore interface {
	SetOnline(ctx context.Context, userID int64, ttl time.Duration) error
	SetOffline(ctx context.Context, userID int64) error
}

type Hub struct {
	mu       sync.RWMutex
	clients  map[int64]map[*Client]struct{}
	presence PresenceStore
}

func NewHub(presence PresenceStore) *Hub {
	return &Hub{
		clients:  map[int64]map[*Client]struct{}{},
		presence: presence,
	}
}

func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, userID int64) error {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(request *http.Request) bool {
			origin := request.Header.Get("Origin")
			if origin == "" {
				return true
			}
			parsed, err := url.Parse(origin)
			if err != nil {
				return false
			}
			return parsed.Host == request.Host ||
				parsed.Hostname() == "localhost" ||
				parsed.Hostname() == "127.0.0.1"
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	client := &Client{
		hub:    h,
		userID: userID,
		conn:   conn,
		send:   make(chan []byte, 64),
		done:   make(chan struct{}),
	}
	h.register(client)
	go client.writeLoop()
	client.readLoop()
	h.unregister(client)
	return nil
}

func (h *Hub) Broadcast(ctx context.Context, userIDs []int64, payload []byte) {
	if h == nil || len(payload) == 0 {
		return
	}
	seen := map[int64]struct{}{}
	h.mu.RLock()
	clients := make([]*Client, 0)
	for _, userID := range userIDs {
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		for client := range h.clients[userID] {
			clients = append(clients, client)
		}
	}
	h.mu.RUnlock()
	for _, client := range clients {
		select {
		case client.send <- payload:
		case <-ctx.Done():
			return
		case <-client.done:
		default:
			client.close()
		}
	}
}

func (h *Hub) Online(userID int64) bool {
	if h == nil || userID <= 0 {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[userID]) > 0
}

func (h *Hub) register(client *Client) {
	if h == nil || client == nil {
		return
	}
	h.mu.Lock()
	if h.clients[client.userID] == nil {
		h.clients[client.userID] = map[*Client]struct{}{}
	}
	h.clients[client.userID][client] = struct{}{}
	h.mu.Unlock()
	if h.presence != nil {
		_ = h.presence.SetOnline(context.Background(), client.userID, 2*pingPeriod)
	}
}

func (h *Hub) unregister(client *Client) {
	if h == nil || client == nil {
		return
	}
	h.mu.Lock()
	if clients := h.clients[client.userID]; clients != nil {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.clients, client.userID)
		}
	}
	offline := len(h.clients[client.userID]) == 0
	h.mu.Unlock()
	client.close()
	if offline && h.presence != nil {
		_ = h.presence.SetOffline(context.Background(), client.userID)
	}
}

type Client struct {
	hub    *Hub
	userID int64
	conn   *websocket.Conn
	send   chan []byte
	done   chan struct{}
	once   sync.Once
}

func (c *Client) readLoop() {
	defer c.close()
	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		if c.hub.presence != nil {
			_ = c.hub.presence.SetOnline(context.Background(), c.userID, 2*pingPeriod)
		}
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *Client) writeLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	defer c.close()
	for {
		select {
		case payload := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-c.done:
			_ = c.conn.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(writeWait))
			return
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) close() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}
