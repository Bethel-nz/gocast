package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"ren.local/gocast/pkg/server"
)

func main() {
	config := server.DefaultConfig()

	// Check if directory exists first
	if _, err := os.Stat(config.VideoDir); os.IsNotExist(err) {
		log.Printf("Videos directory doesn't exist, creating '%s'...\n", config.VideoDir)
		if err := os.MkdirAll(config.VideoDir, 0755); err != nil {
			log.Fatal("Failed to create videos directory:", err)
		}
	}

	// Initialize and start the server
	server := server.New(config)
	if err := server.Start(); err != nil {
		log.Fatal(err)
	}

	log.Printf("Server running on %s\n", config.Port)
	log.Printf("Place video files in the '%s' directory\n", config.VideoDir)

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	// Graceful shutdown
	log.Println("Shutting down server...")
	server.Stop()
	log.Println("Server stopped")
}
