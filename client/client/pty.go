package client

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// PTYManager manages the PTY lifecycle with proper cleanup and error handling
type PTYManager struct {
	client      *Client
	pty         *os.File
	cmd         *exec.Cmd
	ptyMu       sync.RWMutex
	restartCh   chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	initialSize *pty.Winsize
}

// NewPTYManager creates a new PTY manager
func NewPTYManager(client *Client) *PTYManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &PTYManager{
		client:      client,
		restartCh:   make(chan struct{}, 1),
		ctx:         ctx,
		cancel:      cancel,
		initialSize: &pty.Winsize{Rows: 24, Cols: 80},
	}
}

// StartShell starts an interactive shell in a PTY with proper error handling
func (pm *PTYManager) StartShell() error {
	pm.ptyMu.Lock()
	defer pm.ptyMu.Unlock()

	// Clean up any existing PTY before starting a new one
	pm.cleanupLocked()

	// Determine shell based on OS
	var shell string
	var args []string

	if runtime.GOOS == "windows" {
		shell = "cmd.exe"
		args = []string{}
	} else {
		shell = os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}
		args = []string{"-i"}
	}

	pm.cmd = exec.Command(shell, args...)

	// Set environment with proper terminal type for TUI applications
	env := pm.buildEnvironment()
	pm.cmd.Env = env

	// Start PTY with initial size
	ptmx, err := pty.StartWithSize(pm.cmd, pm.initialSize)
	if err != nil {
		return fmt.Errorf("failed to start PTY: %w", err)
	}

	pm.pty = ptmx

	// Start monitor goroutine for shell exit
	pm.wg.Add(1)
	go pm.monitorShell()

	return nil
}

// buildEnvironment builds the environment variables for the shell
func (pm *PTYManager) buildEnvironment() []string {
	env := os.Environ()

	// Set TERM to support full TUI capabilities
	termType := "xterm-256color"
	if runtime.GOOS == "windows" {
		termType = "xterm"
	}
	env = append(env, fmt.Sprintf("TERM=%s", termType))

	// Ensure shell knows it's interactive
	env = append(env, "PS1=$ ")
	env = append(env, "PS2=> ")

	// Additional environment variables for TUI apps
	if runtime.GOOS != "windows" {
		env = append(env, "COLORTERM=truecolor")
	}

	// Remove variables that indicate non-interactive mode
	filteredEnv := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "BASH_NONINTERACTIVE=") &&
			!strings.HasPrefix(e, "NONINTERACTIVE=") {
			filteredEnv = append(filteredEnv, e)
		}
	}

	return filteredEnv
}

// monitorShell monitors the shell process and handles restarts
func (pm *PTYManager) monitorShell() {
	defer pm.wg.Done()

	for {
		// Wait for command to exit
		err := pm.cmd.Wait()

		// Check if we should exit
		select {
		case <-pm.ctx.Done():
			return
		default:
		}

		if err != nil {
			log.Printf("Shell exited with error: %v", err)
		} else {
			log.Printf("Shell exited normally, restarting...")
		}

		// Clean up old PTY
		pm.ptyMu.Lock()
		oldPty := pm.pty
		pm.pty = nil
		pm.ptyMu.Unlock()

		if oldPty != nil {
			oldPty.Close()
		}

		// Check if we should exit before restarting
		select {
		case <-pm.ctx.Done():
			return
		default:
		}

		// Brief delay before restart
		time.Sleep(100 * time.Millisecond)

		// Restart shell
		if err := pm.StartShell(); err != nil {
			log.Printf("Failed to restart shell: %v", err)
			// Signal restart failure
			select {
			case pm.restartCh <- struct{}{}:
			default:
			}
			// Wait before retrying
			time.Sleep(1 * time.Second)
			continue
		}

		log.Printf("Shell restarted successfully")
	}
}

// ReadOutput continuously reads from the PTY and sends output to the WebSocket
func (pm *PTYManager) ReadOutput(conn *websocket.Conn) {
	buf := make([]byte, 4096)

	for {
		// Check for cancellation
		select {
		case <-pm.ctx.Done():
			return
		default:
		}

		// Get current PTY (with minimal lock time)
		pm.ptyMu.RLock()
		pty := pm.pty
		pm.ptyMu.RUnlock()

		// Check if PTY is available
		if pty == nil {
			// PTY not available (shell restarting), wait and check again
			select {
			case <-pm.ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				continue
			}
		}

		// Set read deadline to allow cancellation
		readDeadline := time.Now().Add(1 * time.Second)
		if err := pty.SetReadDeadline(readDeadline); err != nil {
			log.Printf("Error setting PTY read deadline: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Read from PTY
		n, err := pty.Read(buf)
		if err != nil {
			if err == io.EOF {
				// PTY closed, wait for restart
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// Check if it's a timeout (expected for cancellation)
			if os.IsTimeout(err) {
				continue
			}
			log.Printf("PTY read error: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if n > 0 {
			// Send as binary message
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				log.Printf("Error writing terminal output: %v", err)
				return
			}
		}
	}
}

// WriteInput writes input to the PTY
func (pm *PTYManager) WriteInput(data []byte) error {
	pm.ptyMu.RLock()
	pty := pm.pty
	pm.ptyMu.RUnlock()

	if pty == nil {
		// PTY not available, try to restart
		if err := pm.StartShell(); err != nil {
			return fmt.Errorf("PTY not available and restart failed: %w", err)
		}
		// Get the new PTY
		pm.ptyMu.RLock()
		pty = pm.pty
		pm.ptyMu.RUnlock()
	}

	if pty == nil {
		return fmt.Errorf("PTY not available")
	}

	// Try to write, if it fails the PTY might be closed
	if _, err := pty.Write(data); err != nil {
		// Try to restart shell
		if restartErr := pm.StartShell(); restartErr != nil {
			return fmt.Errorf("write failed and restart failed: write=%v, restart=%w", err, restartErr)
		}
		return fmt.Errorf("write failed, shell restarted: %w", err)
	}

	return nil
}

// Resize resizes the PTY to the specified dimensions
func (pm *PTYManager) Resize(rows, cols int) error {
	pm.ptyMu.RLock()
	ptyFile := pm.pty
	pm.ptyMu.RUnlock()

	if ptyFile == nil {
		return fmt.Errorf("PTY not available")
	}

	// Update initial size for future restarts
	size := &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}
	pm.ptyMu.Lock()
	pm.initialSize = size
	pm.ptyMu.Unlock()

	// Resize current PTY
	if err := pty.Setsize(ptyFile, size); err != nil {
		return fmt.Errorf("failed to resize PTY: %w", err)
	}

	return nil
}

// cleanupLocked cleans up PTY resources (must be called with lock held)
func (pm *PTYManager) cleanupLocked() {
	if pm.pty != nil {
		pm.pty.Close()
		pm.pty = nil
	}
	if pm.cmd != nil && pm.cmd.Process != nil {
		pm.cmd.Process.Kill()
		pm.cmd.Wait() // Wait for process to exit
		pm.cmd = nil
	}
}

// Cleanup cleans up all PTY resources
func (pm *PTYManager) Cleanup() {
	pm.cancel() // Cancel context to stop goroutines

	pm.ptyMu.Lock()
	pm.cleanupLocked()
	pm.ptyMu.Unlock()

	// Wait for all goroutines to finish
	pm.wg.Wait()
}
