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
	"scoriadb/internal/auth"
	"scoriadb/pkg/scoria"
	"scoriadb/scoriadb/proto"
)

var (
	dataDir    = flag.String("data-dir", "./data", "Directory for database files")
	grpcPort   = flag.Int("grpc-port", 50051, "Port for gRPC server")
	httpPort   = flag.Int("http-port", 8080, "Port for HTTP/REST server")
	jwtSecret  = flag.String("jwt-secret", "default-secret-change-in-production", "JWT signing secret")
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

	// Ensure __auth__ column family exists and seed default admin user
	if err := ensureAuthCFAndSeedUser(db, *jwtSecret); err != nil {
		log.Fatalf("Failed to setup authentication: %v", err)
	}

	// Create WebSocket hub
	hub := ws.NewHub()
	defer hub.Close()

	// Wrap DB with notifier
	notifyingDB := api.NewNotifyingDB(db, hub)

	// Create gRPC server with authentication interceptor
	skipMethods := map[string]bool{
		"/scoriadb.ScoriaDB/Authenticate": true,
		"/scoriadb.ScoriaDB/Get":          false, // requires auth
		// Health checks could be added later
	}
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(auth.AuthInterceptor([]byte(*jwtSecret), skipMethods)),
		grpc.StreamInterceptor(auth.StreamAuthInterceptor([]byte(*jwtSecret), skipMethods)),
	)
	proto.RegisterScoriaDBServer(grpcServer, scoriagrpc.NewServer(notifyingDB, []byte(*jwtSecret)))
	reflection.Register(grpcServer) // Enable reflection for debugging

	// Create REST API server with authentication middleware
	restServer := rest.NewServer(notifyingDB, []byte(*jwtSecret))

	// Create WebSocket server
	wsServer := ws.NewServer(hub)

	// Multiplex HTTP routes
	mux := http.NewServeMux()
	mux.Handle("/api/v1/kv/", restServer)
	mux.Handle("/ws", wsServer)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			log.Printf("failed to write health response: %v", err)
		}
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		// Simple readiness check: try to read a system key
		_, err := db.GetCF("__meta__", []byte("version"))
		if err != nil {
			http.Error(w, "DB not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("READY")); err != nil {
			log.Printf("failed to write ready response: %v", err)
		}
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

// ensureAuthCFAndSeedUser создаёт Column Family __auth__ если её нет и добавляет
// пользователя admin с паролем admin, если в системе ещё нет пользователей.
func ensureAuthCFAndSeedUser(db scoria.CFDB, jwtSecret string) error {
	// Создаём CF __auth__ если её нет
	cfs := db.ListCFs()
	authCFExists := false
	for _, cf := range cfs {
		if cf == auth.AuthCF {
			authCFExists = true
			break
		}
	}
	if !authCFExists {
		if err := db.CreateCF(auth.AuthCF); err != nil {
			return fmt.Errorf("failed to create auth CF: %w", err)
		}
		log.Printf("Created column family %s", auth.AuthCF)
	}

	// Проверяем, есть ли уже пользователи
	users, err := auth.ListUsers(db)
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}

	if len(users) == 0 {
		// Создаём пользователя admin с паролем admin
		err = auth.CreateUser(db, "admin", "admin", []string{auth.RoleAdmin})
		if err != nil {
			return fmt.Errorf("failed to create admin user: %w", err)
		}
		log.Printf("Created default admin user (username: admin, password: admin)")
	}

	return nil
}