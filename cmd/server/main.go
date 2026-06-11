package main

import (
	"log"
	"net/http"
	"os"

	"github.com/f4ga/ScoriaDB/internal/api/rest"
	"github.com/f4ga/ScoriaDB/internal/auth"
	"github.com/f4ga/ScoriaDB/pkg/scoria"
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
	defer db.Close()

	// Создаём CF для аутентификации
	if err := db.CreateCF(auth.AuthCF); err != nil {
		log.Printf("[SERVER] Note: __auth__ CF creation: %v", err)
	}

	// Создаём admin пользователя (если нет)
	ensureAdminUser(db)

	restServer := rest.NewServer(db, jwtSecret)

	skipPaths := map[string]bool{
		"/api/v1/auth/login": true,
		"/health":            true,
		"/ready":             true,
	}
	authMiddleware := auth.AuthMiddleware(jwtSecret, skipPaths)

	mux := http.NewServeMux()
	mux.Handle("/api/", authMiddleware(restServer))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		_, err := db.Get([]byte("__scoria_health__"))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"not ready"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})

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
	err = auth.CreateUser(cfdb, "admin", "2027", []string{auth.RoleAdmin})
	if err != nil {
		log.Printf("[SERVER] WARNING: failed to create admin user: %v", err)
		return
	}
	log.Println("[SERVER] ✅ Admin user created with default password: 2027")
}
