package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// HandleClientConnection handles new client WebSocket connections
func (s *Server) HandleClientConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	clientID := r.URL.Query().Get("id")
	if clientID == "" {
		clientID = fmt.Sprintf("client-%d", time.Now().UnixNano())
	}

	client := &Client{
		ID:       clientID,
		Conn:     conn,
		LastSeen: time.Now(),
	}

	s.register <- client

	go s.handleClientMessages(client)
}

// handleClientMessages handles messages from a client connection
func (s *Server) handleClientMessages(client *Client) {
	defer func() {
		s.unregister <- client
		client.Conn.Close()
	}()

	// Set read deadline for connection health
	client.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	client.Conn.SetPongHandler(func(string) error {
		client.mu.Lock()
		client.LastSeen = time.Now()
		client.mu.Unlock()
		client.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Start ping ticker for client connection health
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	go func() {
		for {
			select {
			case <-pingTicker.C:
				client.mu.Lock()
				// Check if connection is still alive (last seen within last 90 seconds)
				if time.Since(client.LastSeen) > 90*time.Second {
					client.mu.Unlock()
					client.Conn.Close()
					return
				}
				client.mu.Unlock()
				
				// Send ping
				client.mu.Lock()
				err := client.Conn.WriteMessage(websocket.PingMessage, nil)
				client.mu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

	for {
		// Reset read deadline on each message
		client.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		
		messageType, message, err := client.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		client.mu.Lock()
		client.LastSeen = time.Now()
		client.mu.Unlock()

		// Handle binary messages (terminal output) directly
		if messageType == websocket.BinaryMessage {
			// Encode binary data as base64 for JSON transmission
			// This preserves all control sequences needed for TUI apps
			encodedData := base64.StdEncoding.EncodeToString(message)
			msg := map[string]interface{}{
				"type":      "terminal_output",
				"client_id": client.ID,
				"data":      encodedData,
				"binary":    true, // Flag to indicate base64 encoded data
			}
			msgJSON := safeMarshal(msg)
			if msgJSON == nil {
				continue // Failed to marshal, skip this message
			}
			s.broadcast <- msgJSON
			continue
		}

		// Handle text messages (JSON control messages)
		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}

		switch msg.Type {
		case "terminal_output":
			// Legacy text-based terminal output
			msg.ClientID = client.ID
			msg.Timestamp = time.Now().Format(time.RFC3339)
			resultJSON := safeMarshal(msg)
			if resultJSON == nil {
				continue // Failed to marshal, skip this message
			}
			s.broadcast <- resultJSON
		case "command_result":
			// Legacy support - forward command result to web UI
			msg.ClientID = client.ID
			msg.Timestamp = time.Now().Format(time.RFC3339)
			resultJSON := safeMarshal(msg)
			if resultJSON == nil {
				continue // Failed to marshal, skip this message
			}
			s.broadcast <- resultJSON
		case "ping":
			// Respond to ping
			pong := Message{
				Type:      "pong",
				Timestamp: time.Now().Format(time.RFC3339),
			}
			pongJSON := safeMarshal(pong)
			if pongJSON == nil {
				continue
			}
			client.mu.Lock()
			client.Conn.WriteMessage(websocket.TextMessage, pongJSON)
			client.mu.Unlock()
		}
	}
}

// HandleWebUIConnection handles new web UI WebSocket connections
func (s *Server) HandleWebUIConnection(w http.ResponseWriter, r *http.Request) {
	// Check password if required
	if s.uiPasswordHash != nil {
		providedPassword := r.URL.Query().Get("password")
		if !s.CheckUIPassword(providedPassword) {
			log.Printf("Web UI connection rejected: invalid password")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	uiConn := &UIConnection{
		Conn:          conn,
		LastPong:      time.Now(),
		Authenticated: s.uiPasswordHash == nil, // If no password required, auto-authenticate
	}
	
	// If password was required and provided, mark as authenticated
	if s.uiPasswordHash != nil {
		uiConn.Authenticated = true
	}
	
	// Set read deadline for connection health checks
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		uiConn.mu.Lock()
		uiConn.LastPong = time.Now()
		uiConn.mu.Unlock()
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	
	// Register UI connection
	s.uiConnMu.Lock()
	s.uiConnections = append(s.uiConnections, uiConn)
	s.uiConnMu.Unlock()

	// Start ping ticker for connection health checks
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	// Start goroutine to send pings
	go func() {
		for {
			select {
			case <-pingTicker.C:
				uiConn.mu.Lock()
				// Check if connection is still alive (pong received within last 90 seconds)
				if time.Since(uiConn.LastPong) > 90*time.Second {
					uiConn.mu.Unlock()
					conn.Close()
					return
				}
				uiConn.mu.Unlock()
				
				// Send ping
				uiConn.mu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, nil)
				uiConn.mu.Unlock()
				if err != nil {
					log.Printf("Error sending ping to UI connection: %v", err)
					return
				}
			}
		}
	}()

	defer func() {
		// Unregister UI connection
		s.uiConnMu.Lock()
		for i, c := range s.uiConnections {
			if c == uiConn {
				s.uiConnections = append(s.uiConnections[:i], s.uiConnections[i+1:]...)
				break
			}
		}
		s.uiConnMu.Unlock()
		conn.Close()
	}()

	// Send initial client list
	s.clientsMu.RLock()
	clientList := make([]map[string]interface{}, 0, len(s.clients))
	for id, client := range s.clients {
		clientList = append(clientList, map[string]interface{}{
			"id":        id,
			"last_seen": client.LastSeen.Format(time.RFC3339),
		})
	}
	s.clientsMu.RUnlock()

	initialMsg := map[string]interface{}{
		"type":      "client_list",
		"clients":   clientList,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	initialJSON := safeMarshal(initialMsg)
	if initialJSON == nil {
		log.Printf("Failed to marshal initial client list, closing connection")
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, initialJSON); err != nil {
		log.Printf("Error sending initial client list: %v", err)
		return
	}

	// Handle messages from web UI
	for {
		// Reset read deadline on each message
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		
		_, message, err := conn.ReadMessage()
		if err != nil {
			// Check if it's a timeout or normal close
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("UI WebSocket error: %v", err)
			}
			break
		}

		// Check authentication before processing any messages
		uiConn.mu.Lock()
		authenticated := uiConn.Authenticated
		uiConn.mu.Unlock()
		
		if !authenticated {
			log.Printf("Unauthenticated UI connection attempted to send message, closing")
			conn.Close()
			break
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}

		// Validate message type
		if msg.Type == "" {
			log.Printf("Message missing type field")
			continue
		}

		// Use handler pattern to process messages
		handler, ok := s.handlers[msg.Type]
		if !ok {
			log.Printf("Unknown message type: %s", msg.Type)
			continue
		}

		// Validate message before handling
		if err := handler.Validate(msg); err != nil {
			log.Printf("Message validation failed for type %s: %v", msg.Type, err)
			continue
		}

		// Handle validated message
		if err := handler.Handle(s, msg); err != nil {
			log.Printf("Error handling message type %s: %v", msg.Type, err)
		}
	}
}

