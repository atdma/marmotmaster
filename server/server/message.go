package server

// Message represents a generic WebSocket message (for unmarshaling)
type Message struct {
	Type      string `json:"type"`
	ClientID  string `json:"client_id,omitempty"`
	Command   string `json:"command,omitempty"`
	Data      string `json:"data,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Signature string `json:"signature,omitempty"` // HMAC signature for command verification
}

// TerminalInputMessage represents a terminal_input message
type TerminalInputMessage struct {
	ClientID string `json:"client_id"`
	Data     string `json:"data"`
	Binary   bool   `json:"binary,omitempty"`
}

// Validate validates a TerminalInputMessage
func (m *TerminalInputMessage) Validate() error {
	if m.ClientID == "" {
		return &ValidationError{Field: "client_id", Message: "client_id is required"}
	}
	if m.Data == "" {
		return &ValidationError{Field: "data", Message: "data is required"}
	}
	return nil
}

// TerminalResizeMessage represents a terminal_resize message
type TerminalResizeMessage struct {
	ClientID string `json:"client_id"`
	Rows     int    `json:"rows"`
	Cols     int    `json:"cols"`
}

// Validate validates a TerminalResizeMessage
func (m *TerminalResizeMessage) Validate() error {
	if m.ClientID == "" {
		return &ValidationError{Field: "client_id", Message: "client_id is required"}
	}
	if m.Rows <= 0 {
		return &ValidationError{Field: "rows", Message: "rows must be greater than 0"}
	}
	if m.Cols <= 0 {
		return &ValidationError{Field: "cols", Message: "cols must be greater than 0"}
	}
	return nil
}

// ExecuteCommandMessage represents an execute_command message (legacy)
type ExecuteCommandMessage struct {
	ClientID string `json:"client_id"`
	Command  string `json:"command"`
}

// Validate validates an ExecuteCommandMessage
func (m *ExecuteCommandMessage) Validate() error {
	if m.ClientID == "" {
		return &ValidationError{Field: "client_id", Message: "client_id is required"}
	}
	if m.Command == "" {
		return &ValidationError{Field: "command", Message: "command is required"}
	}
	return nil
}

// SelfDestructMessage represents a self_destruct message
type SelfDestructMessage struct {
	ClientID string `json:"client_id"`
}

// Validate validates a SelfDestructMessage
func (m *SelfDestructMessage) Validate() error {
	if m.ClientID == "" {
		return &ValidationError{Field: "client_id", Message: "client_id is required"}
	}
	return nil
}

// BroadcastCommandMessage represents a broadcast_command message
type BroadcastCommandMessage struct {
	Command string `json:"command"`
}

// Validate validates a BroadcastCommandMessage
func (m *BroadcastCommandMessage) Validate() error {
	if m.Command == "" {
		return &ValidationError{Field: "command", Message: "command is required"}
	}
	return nil
}

// ValidationError represents a message validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}
