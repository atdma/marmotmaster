package config

import (
	"fmt"
	"os"
	"time"
)

// GetServerURL determines the server URL from command-line args or environment variables
func GetServerURL(host string, port int) string {
	if host != "" || port != 0 {
		// Use command-line arguments
		hostname := host
		if hostname == "" {
			hostname = "localhost"
		}
		serverPort := port
		if serverPort == 0 {
			serverPort = 8443 // Default to HTTPS port
		}
		// Determine protocol based on port
		protocol := "ws"
		if serverPort == 443 || serverPort == 8443 {
			protocol = "wss"
		}
		return fmt.Sprintf("%s://%s:%d", protocol, hostname, serverPort)
	} else if url := os.Getenv("MARMOTMASTER_SERVER_URL"); url != "" {
		// Fall back to environment variable
		return url
	} else {
		// Default to HTTPS/WSS
		return "wss://localhost:8443"
	}
}

// GetClientID determines the client ID from command-line args or environment variables
func GetClientID(clientIDFlag string) string {
	if clientIDFlag != "" {
		return clientIDFlag
	} else if id := os.Getenv("MARMOTMASTER_CLIENT_ID"); id != "" {
		return id
	} else {
		hostname := getHostname()
		return fmt.Sprintf("client-%s-%d", hostname, time.Now().Unix())
	}
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

