package server

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

// safeMarshal safely marshals a value to JSON, logging errors and returning nil on failure
func safeMarshal(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("Error marshaling JSON: %v", err)
		return nil
	}
	return data
}

// MessageHandler defines the interface for handling UI messages
type MessageHandler interface {
	// Validate validates the message before handling
	Validate(msg Message) error
	// Handle processes the validated message
	Handle(s *Server, msg Message) error
}

// sendMessageToClient sends a signed message to a specific client
func (s *Server) sendMessageToClient(clientID string, message Message, errorMsg string) error {
	s.clientsMu.RLock()
	targetClient, ok := s.clients[clientID]
	s.clientsMu.RUnlock()

	if !ok {
		return fmt.Errorf("client %s not found", clientID)
	}

	// Sign the message before sending (if not already signed)
	if message.Signature == "" {
		if message.Timestamp == "" {
			message.Timestamp = time.Now().Format(time.RFC3339)
		}
		message.Signature = s.SignMessage(message.Type, clientID, message.Data, message.Timestamp)
	}

	msgJSON := safeMarshal(message)
	if msgJSON == nil {
		return fmt.Errorf("failed to marshal message for client %s", clientID)
	}

	targetClient.mu.Lock()
	err := targetClient.Conn.WriteMessage(websocket.TextMessage, msgJSON)
	targetClient.mu.Unlock()

	if err != nil {
		log.Printf("%s: %v", errorMsg, err)
		return err
	}

	return nil
}

// TerminalInputHandler handles terminal_input messages
type TerminalInputHandler struct{}

func (h *TerminalInputHandler) Validate(msg Message) error {
	typedMsg := TerminalInputMessage{
		ClientID: msg.ClientID,
		Data:     msg.Data,
		Binary:   msg.Binary,
	}
	return typedMsg.Validate()
}

func (h *TerminalInputHandler) Handle(s *Server, msg Message) error {
	cmdMsg := Message{
		Type:      "terminal_input",
		Data:      msg.Data,
		Binary:    msg.Binary,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	return s.sendMessageToClient(msg.ClientID, cmdMsg, fmt.Sprintf("Error sending terminal input to client %s", msg.ClientID))
}

// TerminalResizeHandler handles terminal_resize messages
type TerminalResizeHandler struct{}

func (h *TerminalResizeHandler) Validate(msg Message) error {
	typedMsg := TerminalResizeMessage{
		ClientID: msg.ClientID,
		Rows:     msg.Rows,
		Cols:     msg.Cols,
	}
	return typedMsg.Validate()
}

func (h *TerminalResizeHandler) Handle(s *Server, msg Message) error {
	// For resize, we need to include rows/cols in the signature payload
	timestamp := time.Now().Format(time.RFC3339)
	data := fmt.Sprintf("%d:%d", msg.Rows, msg.Cols)
	cmdMsg := Message{
		Type:      "terminal_resize",
		Rows:      msg.Rows,
		Cols:      msg.Cols,
		Timestamp: timestamp,
		Data:      data, // Store rows:cols in Data field for signing
	}
	// Sign with the data string containing rows:cols
	cmdMsg.Signature = s.SignMessage("terminal_resize", msg.ClientID, data, timestamp)
	return s.sendMessageToClient(msg.ClientID, cmdMsg, fmt.Sprintf("Error sending terminal resize to client %s", msg.ClientID))
}

// ExecuteCommandHandler handles execute_command messages (legacy)
type ExecuteCommandHandler struct{}

func (h *ExecuteCommandHandler) Validate(msg Message) error {
	typedMsg := ExecuteCommandMessage{
		ClientID: msg.ClientID,
		Command:  msg.Command,
	}
	return typedMsg.Validate()
}

func (h *ExecuteCommandHandler) Handle(s *Server, msg Message) error {
	// Convert command to terminal input (add newline to execute)
	cmdMsg := Message{
		Type:      "terminal_input",
		Data:      msg.Command + "\n",
		Binary:    false,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	return s.sendMessageToClient(msg.ClientID, cmdMsg, fmt.Sprintf("Error sending command to client %s", msg.ClientID))
}

// SelfDestructHandler handles self_destruct messages
type SelfDestructHandler struct{}

func (h *SelfDestructHandler) Validate(msg Message) error {
	typedMsg := SelfDestructMessage{
		ClientID: msg.ClientID,
	}
	return typedMsg.Validate()
}

func (h *SelfDestructHandler) Handle(s *Server, msg Message) error {
	cmdMsg := Message{
		Type:      "self_destruct",
		Timestamp: time.Now().Format(time.RFC3339),
	}
	err := s.sendMessageToClient(msg.ClientID, cmdMsg, fmt.Sprintf("Error sending self-destruct to client %s", msg.ClientID))
	if err == nil {
		log.Printf("Self-destruct command sent to client %s", msg.ClientID)
	}
	return err
}

// BroadcastCommandHandler handles broadcast_command messages
type BroadcastCommandHandler struct{}

func (h *BroadcastCommandHandler) Validate(msg Message) error {
	typedMsg := BroadcastCommandMessage{
		Command: msg.Command,
	}
	return typedMsg.Validate()
}

func (h *BroadcastCommandHandler) Handle(s *Server, msg Message) error {
	s.clientsMu.RLock()
	clientCount := len(s.clients)
	clientsCopy := make([]*Client, 0, clientCount)
	for _, client := range s.clients {
		clientsCopy = append(clientsCopy, client)
	}
	s.clientsMu.RUnlock()

	if clientCount == 0 {
		log.Printf("No clients connected to broadcast command to")
		return fmt.Errorf("no clients connected")
	}

	// Send to all clients with individual signatures
	successCount := 0
	timestamp := time.Now().Format(time.RFC3339)
	commandData := msg.Command + "\n"
	
	for _, client := range clientsCopy {
		// Create signed message for each client
		cmdMsg := Message{
			Type:      "terminal_input",
			Data:      commandData,
			Binary:    false,
			Timestamp: timestamp,
			Signature: s.SignMessage("terminal_input", client.ID, commandData, timestamp),
		}
		cmdJSON := safeMarshal(cmdMsg)
		if cmdJSON == nil {
			log.Printf("Error marshaling broadcast command for client %s", client.ID)
			continue
		}

		client.mu.Lock()
		err := client.Conn.WriteMessage(websocket.TextMessage, cmdJSON)
		client.mu.Unlock()
		if err != nil {
			log.Printf("Error broadcasting command to client %s: %v", client.ID, err)
		} else {
			successCount++
		}
	}
	log.Printf("Broadcast command sent to %d/%d clients", successCount, clientCount)
	return nil
}
