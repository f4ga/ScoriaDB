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

package rest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/auth"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/pkg/scoria"
)

func TestRestServer_GetPutDelete(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "rest-test-db")

	srv := NewServer(db, []byte("test-secret"))

	putBody := `{"value": "hello world"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/testkey", bytes.NewReader([]byte(putBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("PUT status = %d, want %d", w.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/kv/testkey", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["value"] != "hello world" {
		t.Errorf("GET value = %v, want 'hello world'", resp["value"])
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/kv/testkey", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("DELETE status = %d, want %d", w.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/kv/testkey", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GET after delete status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRestServer_Scan(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "rest-test-db")

	// Создаём сервер
	srv := NewServer(db, []byte("test-secret"))

	keys := []string{"user:alice", "user:bob", "other:charlie", "user:dave"}
	for _, k := range keys {
		body := fmt.Sprintf(`{"value": "value-%s"}`, k)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/"+k, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT failed for %s: %d", k, w.Code)
		}
	}

	scanBody := `{"prefix": "user:"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kv/scan", bytes.NewReader([]byte(scanBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("SCAN status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to decode scan response: %v", err)
	}
	items, ok := result["items"].([]interface{})
	if !ok {
		t.Fatalf("items field missing or not an array")
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestRestServer_CFOperations(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "rest-test-db")

	if err := db.CreateCF("testcf"); err != nil {
		t.Fatalf("Failed to create CF: %v", err)
	}

	srv := NewServer(db, []byte("test-secret"))

	putBody := `{"value": "cf value", "cf": "testcf"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/cfkey", bytes.NewReader([]byte(putBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("PUT with CF status = %d, want %d", w.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/kv/cfkey?cf=testcf", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET with CF status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["value"] != "cf value" {
		t.Errorf("GET value = %v, want 'cf value'", resp["value"])
	}
}

func TestRestServer_ErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "rest-test-db")

	srv := NewServer(db, []byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kv/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("GET non-existent status = %d, want %d", w.Code, http.StatusNotFound)
	}
	var errResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}
	if errResp["code"] != "NOT_FOUND" {
		t.Errorf("Error code = %v, want NOT_FOUND", errResp["code"])
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/kv/key", bytes.NewReader([]byte("{")))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Invalid JSON status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("Unknown path status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRestServer_CORS(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "rest-test-db")

	srv := NewServer(db, []byte("test-secret"))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/kv/key", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("OPTIONS status = %d, want %d", w.Code, http.StatusOK)
	}
	headers := w.Header()
	if headers.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS header missing")
	}
}

func TestRestServer_Batch(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "rest-test-db")

	srv := NewServer(db, []byte("test-secret"))

	batchBody := `{
		"ops": [
			{"op": "put", "key": "batch:1", "value": "val1"},
			{"op": "put", "key": "batch:2", "value": "val2"},
			{"op": "delete", "key": "batch:1"}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kv/batch", bytes.NewReader([]byte(batchBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("batch status = %d, want %d", w.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/kv/batch:2", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET batch:2 status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["value"] != "val2" {
		t.Errorf("expected value 'val2', got %v", resp["value"])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/kv/batch:1", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("GET batch:1 status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRestServer_Login(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "rest-test-db")

	// Create auth CF and a user
	if err := db.CreateCF("__auth__"); err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	srv := NewServer(db, []byte("test-secret"))

	// Create user via auth package
	err = auth.CreateUser(db, "testuser", "testpass", []string{auth.RoleReadOnly})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Login with correct credentials
	loginBody := `{"username": "testuser", "password": "testpass"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(loginBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("login status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["token"] == "" {
		t.Error("expected non-empty token")
	}

	// Login with wrong password
	loginBody = `{"username": "testuser", "password": "wrongpass"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(loginBody)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong password, got %d", w.Code)
	}

	// Login with invalid JSON
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte("{")))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestRestServer_AdminEndpoints(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "rest-test-db")

	if err := db.CreateCF("__auth__"); err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	srv := NewServer(db, []byte("test-secret"))

	// Create admin user
	err = auth.CreateUser(db, "admin", "adminpass", []string{auth.RoleAdmin})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Create a regular user
	err = auth.CreateUser(db, "regular", "pass", []string{auth.RoleReadOnly})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Test ListUsers (GET /api/v1/admin/users)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("ListUsers status = %d, want %d", w.Code, http.StatusOK)
	}
	var listResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	users, ok := listResp["users"].([]interface{})
	if !ok {
		t.Fatalf("users field missing or not an array")
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}

	// Test CreateUser (POST /api/v1/admin/users)
	createBody := `{"username": "newuser", "password": "newpass", "roles": ["readonly"]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader([]byte(createBody)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("CreateUser status = %d, want %d", w.Code, http.StatusCreated)
	}

	// Create duplicate user
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader([]byte(createBody)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate user, got %d", w.Code)
	}

	// Create user with invalid JSON
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader([]byte("{")))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestRestServer_BatchErrors(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "rest-test-db")

	srv := NewServer(db, []byte("test-secret"))

	// Batch with invalid JSON
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kv/batch", bytes.NewReader([]byte("{")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}

	// Batch with unknown operation
	batchBody := `{"ops": [{"op": "unknown", "key": "test"}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/kv/batch", bytes.NewReader([]byte(batchBody)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown op, got %d", w.Code)
	}
}

func TestRestServer_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "rest-test-db")

	srv := NewServer(db, []byte("test-secret"))

	// POST on a key path (not scan or batch)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kv/mykey", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST on key, got %d", w.Code)
	}
}

func TestRestServer_Health(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "rest-test-db")

	srv := NewServer(db, []byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want 'ok'", resp["status"])
	}
}
