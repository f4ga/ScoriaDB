package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	scoriagrpc "scoriadb/internal/api/grpc"
	"scoriadb/pkg/scoria"
	"scoriadb/scoriadb/proto"
)

var (
	dataDir = flag.String("data-dir", "./data", "Directory for database files")
	port    = flag.Int("port", 50051, "Port to listen on")
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

	// Create gRPC server
	grpcServer := grpc.NewServer()
	proto.RegisterScoriaDBServer(grpcServer, scoriagrpc.NewServer(db))
	reflection.Register(grpcServer) // Enable reflection for debugging

	// Start listening
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Printf("Server starting on port %d", *port)

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("Shutting down server...")
		grpcServer.GracefulStop()
	}()

	// Serve
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}