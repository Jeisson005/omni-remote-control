package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Jeisson005/omni-remote-control/clients/windows/internal/config"
	"github.com/Jeisson005/omni-remote-control/clients/windows/internal/connection"
)

func main() {
	log.Println("Starting Omni Agent for Windows...")

	cfg := config.LoadConfig()
	log.Printf("Agent ID: %s", cfg.DeviceID)
	log.Printf("Server URL: %s", cfg.ServerURL)
	log.Printf("Metrics Interval: %v", cfg.MetricsInterval)
	log.Printf("Heartbeat Interval: %v", cfg.HeartbeatInterval)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := connection.NewAgentClient(cfg)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	go func() {
		<-quit
		log.Println("Received termination signal, shutting down Windows agent...")
		cancel()
	}()

	agent.Start(ctx)
	log.Println("Omni Windows Agent finished cleanly.")
}
