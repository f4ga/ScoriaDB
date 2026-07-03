// Copyright 2026 Ekaterina Godulyan
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"log"
	"net"
	"net/http"
	"os"

	grpcapi "github.com/f4ga/ScoriaDB/internal/api/grpc"
	"github.com/f4ga/ScoriaDB/internal/api/rest"
	"github.com/f4ga/ScoriaDB/internal/auth"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/pkg/scoria"
	"github.com/f4ga/ScoriaDB/scoriadb/proto"
	"google.golang.org/grpc"
)

func main() {
	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(jwtSecret) == 0 {
		jwtSecret = []byte("scoriadb-default-secret")
		log.Println("[SERVER] WARNING: using default JWT secret, set JWT_SECRET environment variable")
	}

	addr := os.Getenv("REST_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data"
	}

	db, err := scoria.NewScoriaDB(dbPath)
	if err != nil {
		log.Fatalf("[SERVER] failed to open database: %v", err)
	}
	defer errors.CloseWithLog(db, "database")

	if err := db.CreateCF(auth.AuthCF); err != nil {
		log.Printf("[SERVER] Note: __auth__ CF creation: %v", err)
	}

	ensureAdminUser(db)

	// ====== gRPC SERVER ======
	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(auth.AuthInterceptor(jwtSecret, map[string]bool{
			"/scoriadb.ScoriaDB/Authenticate": true,
		})),
	)
	proto.RegisterScoriaDBServer(grpcSrv, grpcapi.NewServer(db, jwtSecret))

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Printf("[SERVER] ERROR: failed to listen on :50051: %v", err)
		log.Printf("[SERVER] WARNING: gRPC server disabled due to listen error")
		// Не падаем, продолжаем работу с REST
	} else {
		go func() {
			log.Printf("[SERVER] gRPC server starting on :50051")
			if err := grpcSrv.Serve(lis); err != nil {
				log.Printf("[SERVER] ERROR: gRPC server failed: %v", err)
			}
		}()
	}
	go func() {
		log.Printf("[SERVER] gRPC server starting on :50051")
		if err := grpcSrv.Serve(lis); err != nil {
			log.Fatalf("[SERVER] gRPC server failed: %v", err)
		}
	}()

	// ====== REST SERVER ======
	restServer := rest.NewServer(db, jwtSecret)

	skipPaths := map[string]bool{
		"/api/v1/auth/login": true,
		"/health":            true,
		"/ready":             true,
	}
	authMiddleware := auth.AuthMiddleware(jwtSecret, skipPaths)

	mux := http.NewServeMux()
	mux.Handle("/api/", authMiddleware(restServer))
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", readyHandler(db))

	log.Printf("[SERVER] REST API starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[SERVER] failed to start: %v", err)
	}
}

func ensureAdminUser(cfdb scoria.CFDB) {
	_, err := auth.GetUser(cfdb, "admin")
	if err == nil {
		log.Println("[SERVER] Admin user already exists")
		return
	}
	if err := auth.CreateUser(cfdb, "admin", "2027", []string{auth.RoleAdmin}); err != nil {
		log.Printf("[SERVER] WARNING: failed to create admin user: %v", err)
		return
	}
	log.Println("[SERVER] ✅ Admin user created with default password: 2027")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		log.Printf("WARNING: failed to write health response: %v", err)
	}
}

func readyHandler(db scoria.CFDB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, err := db.Get([]byte("__scoria_health__"))
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			if _, err := w.Write([]byte(`{"status":"not ready"}`)); err != nil {
				log.Printf("WARNING: failed to write not ready response: %v", err)
			}
			return
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status":"ready"}`)); err != nil {
			log.Printf("WARNING: failed to write ready response: %v", err)
		}
	}
}
