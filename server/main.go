package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"marmotmaster/server/server"
	"marmotmaster/server/cert"
	"marmotmaster/server/static"
)

func main() {
	// Command-line flags
	host := flag.String("host", "", "Host address to bind to (default: all interfaces, 0.0.0.0)")
	port := flag.Int("port", 8443, "Port to listen on (default: 8443)")
	uiPasswordHash := flag.String("hash", "", "Bcrypt hash for web UI access (default: no password)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -host 0.0.0.0 -port 8443\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -host 192.168.1.100 -port 443 -hash '$2a$10$...'\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -port 8080\n", os.Args[0])
	}
	flag.Parse()

	server := server.NewServer()
	if *uiPasswordHash != "" {
		if err := server.SetUIPasswordHash(*uiPasswordHash); err != nil {
			log.Fatalf("Failed to set UI password hash: %v", err)
		}
		log.Printf("Web UI password protection enabled")
	}
	go server.Run()

	// Find static directory
	staticDir, err := static.FindStaticDir()
	if err != nil {
		log.Fatalf("Static directory error: %v", err)
	}
	
	// Serve static files
	fs := http.FileServer(http.Dir(staticDir))
	http.Handle("/", fs)

	// WebSocket endpoints
	http.HandleFunc("/ws/client", server.HandleClientConnection)
	http.HandleFunc("/ws/ui", server.HandleWebUIConnection)

	// Determine certificate paths
	certDir := "."
	certPath := filepath.Join(certDir, "cert.pem")
	keyPath := filepath.Join(certDir, "key.pem")

	// Load or generate certificate
	tlsCert, err := cert.LoadOrGenerateCert(certPath, keyPath)
	if err != nil {
		log.Fatalf("Failed to setup TLS: %v", err)
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*tlsCert},
		MinVersion:   tls.VersionTLS12,
	}

	// Build listen address
	listenHost := *host
	if listenHost == "" {
		listenHost = "0.0.0.0" // Listen on all interfaces by default
	}
	listenAddr := net.JoinHostPort(listenHost, strconv.Itoa(*port))

	// Create HTTP server with TLS
	srv := &http.Server{
		Addr:      listenAddr,
		TLSConfig: tlsConfig,
		Handler:   nil,
	}

	log.Printf("Server starting on https://%s", listenAddr)
	log.Printf("Using self-signed certificate (browser will show security warning)")
	log.Printf("Certificate: %s", certPath)
	log.Printf("Private Key: %s", keyPath)
	log.Fatal(srv.ListenAndServeTLS("", ""))
}
