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

// findBinDir finds the bin directory relative to the executable
func findBinDir() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %v", err)
	}
	execDir := filepath.Dir(execPath)
	
	// Try multiple possible locations for bin directory
	binDirs := []string{
		execDir,                                    // bin/ (when running from bin/)
		filepath.Join(execDir, "..", "bin"),       // ../bin (when running from server/)
		filepath.Join(execDir, "bin"),             // bin/ (when running from root)
		"./bin",                                    // Current directory
		"../bin",                                   // Relative to current dir
	}
	
	for _, dir := range binDirs {
		clientPath := filepath.Join(dir, "marmotmaster-client")
		if info, err := os.Stat(clientPath); err == nil && !info.IsDir() {
			log.Printf("Found client binary at: %s", clientPath)
			return dir, nil
		}
	}
	
	return "", fmt.Errorf("bin directory not found. Tried: %v", binDirs)
}

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
	
	// Find bin directory for client binaries
	binDir, err := findBinDir()
	if err != nil {
		log.Printf("Warning: Bin directory not found, client downloads will not be available: %v", err)
	} else {
		log.Printf("Client binaries available at: https://%s/download/client", listenAddr)
		// Serve client binaries at /download/client (no authentication required)
		http.HandleFunc("/download/client", func(w http.ResponseWriter, r *http.Request) {
			clientPath := filepath.Join(binDir, "marmotmaster-client")
			// Check if file exists
			if _, err := os.Stat(clientPath); os.IsNotExist(err) {
				http.NotFound(w, r)
				return
			}
			// Set headers for file download
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", "marmotmaster-client"))
			// Serve the file
			http.ServeFile(w, r, clientPath)
		})
	}
	
	// Serve static files
	fs := http.FileServer(http.Dir(staticDir))
	http.Handle("/", fs)

	// WebSocket endpoints
	http.HandleFunc("/ws/client", server.HandleClientConnection)
	http.HandleFunc("/ws/ui", server.HandleWebUIConnection)

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
