package session

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"

	"soroban-studio-backend/internal/model"
)

// Session represents an active compilation session.
// It holds WebSocket connections and a message buffer for late-connecting clients.
type Session struct {
	mu     sync.Mutex
	conns  []*websocket.Conn
	buffer []model.OutputMessage
}

// Manager handles session lifecycle and WebSocket connections.
// Uses sync.Map for thread-safe concurrent access across goroutines.
type Manager struct {
	sessions sync.Map // map[string]*Session
}

// NewManager creates a new session manager.
func NewManager() *Manager {
	return &Manager{}
}

// Create initializes a new session with the given ID.
// Must be called before any Send or AddConnection calls.
func (m *Manager) Create(sessionID string) {
	m.sessions.Store(sessionID, &Session{
		conns:  make([]*websocket.Conn, 0),
		buffer: make([]model.OutputMessage, 0),
	})
	log.Printf("[session] created: %s", sessionID)
}

// AddConnection registers a WebSocket connection for a session.
// Any buffered messages (from before the client connected) are flushed immediately.
// Returns false if the session does not exist.
func (m *Manager) AddConnection(sessionID string, conn *websocket.Conn) bool {
	val, ok := m.sessions.Load(sessionID)
	if !ok {
		return false
	}

	s := val.(*Session)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Flush buffered messages to the newly connected client
	for _, msg := range s.buffer {
		data, _ := json.Marshal(msg)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("[session] failed to flush buffer to new connection: %v", err)
			return false
		}
	}

	s.conns = append(s.conns, conn)
	log.Printf("[session] connection added: session=%s, total_conns=%d", sessionID, len(s.conns))
	return true
}

// RemoveConnection removes a specific WebSocket connection from a session.
// Called when a client disconnects.
func (m *Manager) RemoveConnection(sessionID string, conn *websocket.Conn) {
	val, ok := m.sessions.Load(sessionID)
	if !ok {
		return
	}

	s := val.(*Session)
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, c := range s.conns {
		if c == conn {
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			log.Printf("[session] connection removed: session=%s, remaining=%d", sessionID, len(s.conns))
			break
		}
	}
}

// Send broadcasts a message to all connected WebSocket clients for a session.
// Messages are also buffered for clients that connect later.
// Dead connections are automatically cleaned up.
func (m *Manager) Send(sessionID string, msg model.OutputMessage) {
	val, ok := m.sessions.Load(sessionID)
	if !ok {
		return
	}

	s := val.(*Session)
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[session] failed to marshal message: %v", err)
		return
	}

	// Always buffer for late-connecting clients
	s.buffer = append(s.buffer, msg)

	// Broadcast to all active connections, removing dead ones
	activeConns := make([]*websocket.Conn, 0, len(s.conns))
	for _, conn := range s.conns {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("[session] removing dead connection: %v", err)
			conn.Close()
		} else {
			activeConns = append(activeConns, conn)
		}
	}
	s.conns = activeConns
}

// Remove cleans up a session entirely, closing all connections.
func (m *Manager) Remove(sessionID string) {
	val, ok := m.sessions.LoadAndDelete(sessionID)
	if !ok {
		return
	}

	s := val.(*Session)
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, conn := range s.conns {
		conn.Close()
	}
	log.Printf("[session] removed: %s", sessionID)
}
