package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"marmotmaster/client/client"
	"marmotmaster/client/config"
)

func main() {
	// Command-line flags
	host := flag.String("host", "", "Server hostname or IP address (default: localhost)")
	port := flag.Int("port", 0, "Server port (default: 8080)")
	clientIDFlag := flag.String("id", "", "Client ID (default: auto-generated)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -host 192.168.1.100 -port 8080\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -host example.com -port 443 -id my-client\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nEnvironment variables (used if flags not provided):\n")
		fmt.Fprintf(os.Stderr, "  MARMOTMASTER_SERVER_URL  - Full WebSocket URL (e.g., ws://192.168.1.100:8080)\n")
		fmt.Fprintf(os.Stderr, "  MARMOTMASTER_CLIENT_ID   - Client identifier\n")
	}
	flag.Parse()

	// Determine server URL and client ID
	serverURL := config.GetServerURL(*host, *port)
	clientID := config.GetClientID(*clientIDFlag)

	log.Printf("Connecting to server: %s", serverURL)
	log.Printf("Client ID: %s", clientID)

	c := client.NewClient(serverURL, clientID)

	// Handle graceful shutdown
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-interrupt
		log.Println("Shutting down...")
		// Cleanup is handled by defer in Run()
		os.Exit(0)
	}()

	// Connect and run
	if err := c.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	// Run in a goroutine to allow reconnection
	go func() {
		c.Run()
		log.Println("Connection lost, attempting to reconnect...")
		c.Reconnect()
	}()

	// Keep main goroutine alive
	select {}
}
