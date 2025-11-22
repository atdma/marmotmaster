package client

// Message represents a WebSocket message
type Message struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	Command   string `json:"command,omitempty"` // Legacy field for execute_command
	Binary    bool   `json:"binary,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

