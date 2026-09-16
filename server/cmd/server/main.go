package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Jeisson005/omni-remote-control/server/internal/api"
	"github.com/Jeisson005/omni-remote-control/server/internal/db"
	"github.com/Jeisson005/omni-remote-control/server/internal/push"
	"github.com/Jeisson005/omni-remote-control/server/internal/ws"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/omni?sslmode=disable"
	}

	log.Printf("Starting Omni Remote Control Server on port :%s", port)

	database, err := db.Connect(dbURL)
	if err != nil {
		log.Fatalf("Database initialization error: %v", err)
	}

	pushClient := push.NewFromEnv()
	hub := ws.NewHub(database, pushClient)
	serverAPI := api.NewServer(database, hub)
	router := serverAPI.SetupRoutes()

	httpServer := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Server run in goroutine
	go func() {
		log.Printf("Omni Server is listening on http://0.0.0.0:%s", port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting gracefully")
}
