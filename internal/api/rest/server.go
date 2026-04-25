package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"scoriadb/pkg/scoria"
)

// Server представляет HTTP‑сервер REST API для ScoriaDB.
type Server struct {
	db scoria.CFDB
}

// NewServer создаёт новый REST‑сервер, использующий указанную базу данных.
func NewServer(db scoria.CFDB) *Server {
	return &Server{db: db}
}

// ServeHTTP реализует интерфейс http.Handler, маршрутизируя запросы.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Устанавливаем заголовки CORS для удобства разработки
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, DELETE, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Специальный маршрут для сканирования
	if r.URL.Path == "/api/v1/kv/scan" && r.Method == http.MethodPost {
		s.handleScan(w, r)
		return
	}

	// Маршрутизация для операций с конкретным ключом
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/kv/")
	if strings.HasPrefix(r.URL.Path, "/api/v1/kv/") && path != "" {
		key := path
		switch r.Method {
		case http.MethodGet:
			s.handleGet(w, r, key)
		case http.MethodPut:
			s.handlePut(w, r, key)
		case http.MethodDelete:
			s.handleDelete(w, r, key)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Если маршрут не найден
	http.NotFound(w, r)
}

// handleGet обрабатывает GET /api/v1/kv/{key}
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, key string) {
	cf := r.URL.Query().Get("cf")
	if cf == "" {
		cf = "default"
	}

	value, err := s.db.GetCF(cf, []byte(key))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("failed to get key: %v", err))
		return
	}

	if value == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "key not found")
		return
	}

	resp := map[string]interface{}{
		"key":   key,
		"value": string(value),
		"cf":    cf,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePut обрабатывает PUT /api/v1/kv/{key}
func (s *Server) handlePut(w http.ResponseWriter, r *http.Request, key string) {
	var req struct {
		Value string `json:"value"`
		CF    string `json:"cf"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}

	cf := req.CF
	if cf == "" {
		cf = "default"
	}

	if err := s.db.PutCF(cf, []byte(key), []byte(req.Value)); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("failed to put key: %v", err))
		return
	}

	resp := map[string]interface{}{
		"key":   key,
		"cf":    cf,
		"status": "ok",
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDelete обрабатывает DELETE /api/v1/kv/{key}
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, key string) {
	cf := r.URL.Query().Get("cf")
	if cf == "" {
		cf = "default"
	}

	if err := s.db.DeleteCF(cf, []byte(key)); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("failed to delete key: %v", err))
		return
	}

	resp := map[string]interface{}{
		"key":   key,
		"cf":    cf,
		"status": "deleted",
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleScan обрабатывает POST /api/v1/kv/scan
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prefix string `json:"prefix"`
		CF     string `json:"cf"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}

	cf := req.CF
	if cf == "" {
		cf = "default"
	}

	iter := s.db.ScanCF(cf, []byte(req.Prefix))
	defer iter.Close()

	items := []map[string]string{}
	for iter.Next() {
		items = append(items, map[string]string{
			"key":   string(iter.Key()),
			"value": string(iter.Value()),
		})
	}
	if err := iter.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("scan iteration error: %v", err))
		return
	}

	resp := map[string]interface{}{
		"cf":    cf,
		"prefix": req.Prefix,
		"items": items,
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeJSON записывает JSON‑ответ с указанным статус‑кодом.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError записывает JSON‑ответ с ошибкой в формате, описанном в Разделе 3.6.1 плана.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    code,
		"message": message,
	})
}