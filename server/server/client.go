package server

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents a connected client
type Client struct {
	ID       string
	Conn     *websocket.Conn
	LastSeen time.Time
	mu       sync.Mutex
}

// UIConnection represents a web UI WebSocket connection
type UIConnection struct {
	Conn          *websocket.Conn
	mu            sync.Mutex
	LastPong      time.Time
	Authenticated bool // Whether this connection has been authenticated
}

