package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"scoriadb/internal/api"
	scoriagrpc "scoriadb/internal/api/grpc"
	"scoriadb/internal/api/rest"
	"scoriadb/internal/api/ws"
	"scoriadb/pkg/scoria"
	"scoriadb/scoriadb/proto"
)

var (
	dataDir   = flag.String("data-dir", "./data", "Directory for database files")
	grpcPort  = flag.Int("grpc-port", 50051, "Port for gRPC server")
	httpPort  = flag.Int("http-port", 8080, "Port for HTTP/REST server")
)

func main() {
	flag.Parse()

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Open the database
	log.Printf("Opening database at %s", *dataDir)
	db, err := scoria.NewScoriaDB(*dataDir)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create WebSocket hub
	hub := ws.NewHub()
	defer hub.Close()

	// Wrap DB with notifier
	notifyingDB := api.NewNotifyingDB(db, hub)

	// Create gRPC server (using notifying DB to capture writes from gRPC as well)
	grpcServer := grpc.NewServer()
	proto.RegisterScoriaDBServer(grpcServer, scoriagrpc.NewServer(notifyingDB))
	reflection.Register(grpcServer) // Enable reflection for debugging

	// Create REST API server
	restServer := rest.NewServer(notifyingDB)

	// Create WebSocket server
	wsServer := ws.NewServer(hub)

	// Multiplex HTTP routes
	mux := http.NewServeMux()
	mux.Handle("/api/v1/kv/", restServer)
	mux.Handle("/ws", wsServer)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		// Simple readiness check: try to read a system key
		_, err := db.GetCF("__meta__", []byte("version"))
		if err != nil {
			http.Error(w, "DB not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("READY"))
	})

	// Start gRPC server
	grpcLis, err := net.Listen("tcp", fmt.Sprintf(":%d", *grpcPort))
	if err != nil {
		log.Fatalf("Failed to listen on gRPC port: %v", err)
	}
	go func() {
		log.Printf("gRPC server starting on port %d", *grpcPort)
		if err := grpcServer.Serve(grpcLis); err != nil {
			log.Fatalf("gRPC server failed: %v", err)
		}
	}()

	// Start HTTP server
	httpLis, err := net.Listen("tcp", fmt.Sprintf(":%d", *httpPort))
	if err != nil {
		log.Fatalf("Failed to listen on HTTP port: %v", err)
	}
	httpServer := &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("HTTP server starting on port %d", *httpPort)
		if err := httpServer.Serve(httpLis); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	log.Println("Shutting down servers...")

	// Shutdown HTTP server with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// Stop gRPC server gracefully
	grpcServer.GracefulStop()

	log.Println("Servers stopped.")
}