package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

// Session represents an authenticated UI session
type Session struct {
	Token     string
	ExpiresAt time.Time
}

// Server manages WebSocket connections and message routing
type Server struct {
	clients       map[string]*Client
	clientsMu     sync.RWMutex
	uiConnections []*UIConnection
	uiConnMu      sync.RWMutex
	broadcast     chan []byte
	register      chan *Client
	unregister    chan *Client
	handlers      map[string]MessageHandler
	uiPasswordHash []byte // Bcrypt hash of password for UI access (nil means no password required)
	sessions      map[string]*Session // Active sessions
	sessionsMu    sync.RWMutex
	signingKey    []byte // Key for HMAC signing of commands to clients
}

// NewServer creates a new server instance
func NewServer() *Server {
	// Generate a random signing key for HMAC
	signingKey := make([]byte, 32)
	if _, err := rand.Read(signingKey); err != nil {
		log.Fatalf("Failed to generate signing key: %v", err)
	}

	s := &Server{
		clients:       make(map[string]*Client),
		uiConnections: make([]*UIConnection, 0),
		broadcast:     make(chan []byte, 256),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		handlers:      make(map[string]MessageHandler),
		uiPasswordHash: nil,
		sessions:       make(map[string]*Session),
		signingKey:     signingKey,
	}
	
	// Register message handlers
	s.handlers["terminal_input"] = &TerminalInputHandler{}
	s.handlers["terminal_resize"] = &TerminalResizeHandler{}
	s.handlers["execute_command"] = &ExecuteCommandHandler{}
	s.handlers["self_destruct"] = &SelfDestructHandler{}
	s.handlers["broadcast_command"] = &BroadcastCommandHandler{}
	
	// Start session cleanup goroutine
	go s.cleanupExpiredSessions()
	
	return s
}

// SetUIPasswordHash sets the bcrypt hash for UI access
// The hash should be a valid bcrypt hash string (e.g., generated with bcrypt.GenerateFromPassword)
func (s *Server) SetUIPasswordHash(hash string) error {
	// Validate that the provided string is a valid bcrypt hash
	// We do this by attempting to compare it with a dummy password
	// If it's not a valid hash, bcrypt will return an error
	_, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		return fmt.Errorf("invalid bcrypt hash: %v", err)
	}
	s.uiPasswordHash = []byte(hash)
	return nil
}

// CheckUIPassword checks if the provided password matches the stored hash
func (s *Server) CheckUIPassword(password string) bool {
	if s.uiPasswordHash == nil {
		return true // No password required
	}
	err := bcrypt.CompareHashAndPassword(s.uiPasswordHash, []byte(password))
	return err == nil
}

// Run starts the server's main event loop
func (s *Server) Run() {
	for {
		select {
		case client := <-s.register:
			s.clientsMu.Lock()
			s.clients[client.ID] = client
			s.clientsMu.Unlock()
			log.Printf("Client connected: %s", client.ID)
			s.broadcastClientList()

		case client := <-s.unregister:
			s.clientsMu.Lock()
			if _, ok := s.clients[client.ID]; ok {
				delete(s.clients, client.ID)
				client.Conn.Close()
			}
			s.clientsMu.Unlock()
			log.Printf("Client disconnected: %s", client.ID)
			s.broadcastClientList()

		case message := <-s.broadcast:
			// Send to web UI connections only, removing dead connections
			s.uiConnMu.Lock()
			validConnections := make([]*UIConnection, 0, len(s.uiConnections))
			for _, uiConn := range s.uiConnections {
				uiConn.mu.Lock()
				err := uiConn.Conn.WriteMessage(websocket.TextMessage, message)
				uiConn.mu.Unlock()
				if err != nil {
					log.Printf("Error broadcasting to UI, removing dead connection: %v", err)
					uiConn.Conn.Close()
				} else {
					validConnections = append(validConnections, uiConn)
				}
			}
			s.uiConnections = validConnections
			s.uiConnMu.Unlock()
		}
	}
}

// broadcastClientList sends the current client list to all UI connections
func (s *Server) broadcastClientList() {
	s.clientsMu.RLock()
	clientList := make([]map[string]interface{}, 0, len(s.clients))
	for id, client := range s.clients {
		clientList = append(clientList, map[string]interface{}{
			"id":        id,
			"last_seen": client.LastSeen.Format(time.RFC3339),
		})
	}
	s.clientsMu.RUnlock()

	msg := map[string]interface{}{
		"type":      "client_list",
		"clients":   clientList,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	msgJSON := safeMarshal(msg)
	if msgJSON == nil {
		return // Failed to marshal, skip broadcast
	}
	s.broadcast <- msgJSON
}

// CreateSession creates a new authenticated session and returns the token
func (s *Server) CreateSession() (string, error) {
	// Generate a random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate token: %v", err)
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	// Create session with 24 hour expiration
	session := &Session{
		Token:     token,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	s.sessionsMu.Lock()
	s.sessions[token] = session
	s.sessionsMu.Unlock()

	return token, nil
}

// ValidateSession checks if a session token is valid
func (s *Server) ValidateSession(token string) bool {
	if token == "" {
		return false
	}

	s.sessionsMu.RLock()
	session, exists := s.sessions[token]
	s.sessionsMu.RUnlock()

	if !exists {
		return false
	}

	// Check if session expired
	if time.Now().After(session.ExpiresAt) {
		s.sessionsMu.Lock()
		delete(s.sessions, token)
		s.sessionsMu.Unlock()
		return false
	}

	return true
}

// cleanupExpiredSessions periodically removes expired sessions
func (s *Server) cleanupExpiredSessions() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		s.sessionsMu.Lock()
		for token, session := range s.sessions {
			if now.After(session.ExpiresAt) {
				delete(s.sessions, token)
			}
		}
		s.sessionsMu.Unlock()
	}
}

// SignMessage creates an HMAC signature for a message
func (s *Server) SignMessage(messageType, clientID, data string, timestamp string) string {
	// Create message payload for signing
	payload := fmt.Sprintf("%s:%s:%s:%s", messageType, clientID, data, timestamp)
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// GetSigningKey returns the signing key (for clients to verify signatures)
func (s *Server) GetSigningKey() []byte {
	return s.signingKey
}

