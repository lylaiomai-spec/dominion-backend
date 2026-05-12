package Websockets

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var globalMsgID int64

const bufferWindow = 30 * time.Second

// BufferedMessage holds a sent message with its ID and timestamp for replay.
type BufferedMessage struct {
	ID        int64
	Timestamp time.Time
	Payload   map[string]interface{}
}

type Client struct {
	Hub    *Hub
	Conn   *websocket.Conn
	Send   chan interface{}
	UserID int
}

func (c *Client) writePump() {
	defer func() {
		// Close the connection so the read goroutine's ReadMessage returns
		// an error and its cleanup defer runs (RemoveUser, broadcast, etc.).
		c.Conn.Close()
		c.Hub.unregister <- c
	}()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// The Hub closed the channel, which means the client is being disconnected.
				// Send a close message to the client for a clean shutdown.
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// If writing to the connection fails, the client is considered disconnected.
			// We return from the function, which will trigger the deferred cleanup.
			if err := c.Conn.WriteJSON(message); err != nil {
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

type Hub struct {
	clients    map[int]map[*Client]bool
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex

	bufferMu   sync.Mutex
	userBuffer map[int][]*BufferedMessage
}

var MainHub = NewHub()

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[int]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		userBuffer: make(map[int][]*BufferedMessage),
	}
}

func (h *Hub) Run() {
	cleanupTicker := time.NewTicker(10 * time.Second)
	defer cleanupTicker.Stop()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if h.clients[client.UserID] == nil {
				h.clients[client.UserID] = make(map[*Client]bool)
			}
			h.clients[client.UserID][client] = true
			h.mu.Unlock()
			go client.writePump()

		case client := <-h.unregister:
			h.mu.Lock()
			if userClients, ok := h.clients[client.UserID]; ok {
				if _, ok := userClients[client]; ok {
					delete(userClients, client)
					close(client.Send)
					if len(userClients) == 0 {
						delete(h.clients, client.UserID)
					}
				}
			}
			h.mu.Unlock()

		case <-cleanupTicker.C:
			h.cleanupBuffer()
		}
	}
}

// cleanupBuffer removes messages older than bufferWindow from all user buffers.
func (h *Hub) cleanupBuffer() {
	cutoff := time.Now().Add(-bufferWindow)
	h.bufferMu.Lock()
	defer h.bufferMu.Unlock()
	for userID, msgs := range h.userBuffer {
		i := 0
		for i < len(msgs) && !msgs[i].Timestamp.After(cutoff) {
			i++
		}
		if i == len(msgs) {
			delete(h.userBuffer, userID)
		} else {
			h.userBuffer[userID] = msgs[i:]
		}
	}
}

// enrichMessage assigns a msg_id to the message and returns it as a map.
func enrichMessage(msg interface{}, msgID int64) map[string]interface{} {
	var enriched map[string]interface{}
	if m, ok := msg.(map[string]interface{}); ok {
		enriched = make(map[string]interface{}, len(m)+1)
		for k, v := range m {
			enriched[k] = v
		}
	} else {
		b, _ := json.Marshal(msg)
		json.Unmarshal(b, &enriched)
		if enriched == nil {
			enriched = make(map[string]interface{})
		}
	}
	enriched["msg_id"] = msgID
	return enriched
}

func (h *Hub) addToBuffer(userID int, msgID int64, payload map[string]interface{}) {
	h.bufferMu.Lock()
	h.userBuffer[userID] = append(h.userBuffer[userID], &BufferedMessage{
		ID:        msgID,
		Timestamp: time.Now(),
		Payload:   payload,
	})
	h.bufferMu.Unlock()
}

// GetMissedMessages returns all buffered messages for userID with ID > afterID, in order.
func (h *Hub) GetMissedMessages(userID int, afterID int64) []map[string]interface{} {
	h.bufferMu.Lock()
	defer h.bufferMu.Unlock()
	var result []map[string]interface{}
	for _, msg := range h.userBuffer[userID] {
		if msg.ID > afterID {
			result = append(result, msg.Payload)
		}
	}
	return result
}

func (h *Hub) Register(client *Client) {
	h.register <- client
}

func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	count := 0
	for _, clients := range h.clients {
		count += len(clients)
	}
	return count
}

func (h *Hub) Broadcast(message interface{}) {
	msgID := atomic.AddInt64(&globalMsgID, 1)
	payload := enrichMessage(message, msgID)

	// Snapshot connected users without holding mu while buffering.
	h.mu.RLock()
	type clientEntry struct {
		userID int
		client *Client
	}
	entries := make([]clientEntry, 0)
	for userID, userClients := range h.clients {
		for client := range userClients {
			entries = append(entries, clientEntry{userID, client})
		}
	}
	h.mu.RUnlock()

	seen := make(map[int]bool)
	for _, e := range entries {
		if !seen[e.userID] {
			seen[e.userID] = true
			h.addToBuffer(e.userID, msgID, payload)
		}
		func() {
			defer func() { recover() }()
			select {
			case e.client.Send <- payload:
			default:
			}
		}()
	}
}

// BroadcastToUsers sends a message to a specific set of users using a single msg_id.
func (h *Hub) BroadcastToUsers(userIDs []int, message interface{}) {
	if len(userIDs) == 0 {
		return
	}
	msgID := atomic.AddInt64(&globalMsgID, 1)
	payload := enrichMessage(message, msgID)

	h.mu.RLock()
	type clientEntry struct {
		userID int
		client *Client
	}
	entries := make([]clientEntry, 0)
	for _, userID := range userIDs {
		for client := range h.clients[userID] {
			entries = append(entries, clientEntry{userID, client})
		}
	}
	h.mu.RUnlock()

	seen := make(map[int]bool)
	for _, e := range entries {
		if !seen[e.userID] {
			seen[e.userID] = true
			h.addToBuffer(e.userID, msgID, payload)
		}
		func() {
			defer func() { recover() }()
			select {
			case e.client.Send <- payload:
			default:
			}
		}()
	}
}

// SendToClients sends a message to a specific set of clients (individual connections).
func (h *Hub) SendToClients(clients []any, message interface{}) {
	if len(clients) == 0 {
		return
	}
	msgID := atomic.AddInt64(&globalMsgID, 1)
	payload := enrichMessage(message, msgID)

	for _, c := range clients {
		if client, ok := c.(*Client); ok {
			func() {
				defer func() { recover() }()
				select {
				case client.Send <- payload:
				default:
				}
			}()
		}
	}
}

// IsUserConnected returns true if the user has at least one active WebSocket connection.
func (h *Hub) IsUserConnected(userID int) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[userID]) > 0
}

// CloseUserConnections forcefully closes all WebSocket connections for a user.
// The read loop detects the error and runs its cleanup defer (RemoveUser, Unregister).
func (h *Hub) CloseUserConnections(userID int) {
	h.mu.RLock()
	clients := make([]*Client, 0)
	for c := range h.clients[userID] {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		c.Conn.Close()
	}
}

func (h *Hub) SendNotification(userID int, message interface{}) {
	msgID := atomic.AddInt64(&globalMsgID, 1)
	payload := enrichMessage(message, msgID)

	h.addToBuffer(userID, msgID, payload)

	h.mu.RLock()
	defer h.mu.RUnlock()
	if userClients, ok := h.clients[userID]; ok {
		for client := range userClients {
			func() {
				defer func() { recover() }()
				select {
				case client.Send <- payload:
				default:
				}
			}()
		}
	}
}
