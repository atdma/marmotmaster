package static

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// FindStaticDir finds the static files directory relative to the executable
func FindStaticDir() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %v", err)
	}
	execDir := filepath.Dir(execPath)
	
	// Try multiple possible locations for static files
	staticDirs := []string{
		filepath.Join(execDir, "static"),           // bin/static (when running from bin/)
		filepath.Join(execDir, "..", "server", "static"), // server/static (when running from server/)
		"./static",                                  // Current directory
		"../server/static",                         // Relative to current dir
	}
	
	for _, dir := range staticDirs {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			log.Printf("Serving static files from: %s", dir)
			return dir, nil
		}
	}
	
	return "", fmt.Errorf("static directory not found. Tried: %v", staticDirs)
}

