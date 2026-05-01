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

	"scoriadb/pkg/scoria"
)

func TestRestServer_GetPutDelete(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	srv := NewServer(db, []byte("test-secret"))

	// Test PUT
	putBody := `{"value": "hello world"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/testkey", bytes.NewReader([]byte(putBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("PUT status = %d, want %d", w.Code, http.StatusOK)
	}

	// Test GET
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

	// Test DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/kv/testkey", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("DELETE status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify key is gone
	req = httptest.NewRequest(http.MethodGet, "/api/v1/kv/testkey", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GET after delete status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRestServer_Scan(t *testing.T) {
	// Scanning is not yet implemented; skip this test for now.
	t.Skip("Scanning not implemented yet")

	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	srv := NewServer(db, []byte("test-secret"))

	// Insert a few keys with prefix "user:"
	keys := []string{"user:alice", "user:bob", "other:charlie"}
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

	// Scan with prefix "user:"
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
	// Expect empty items for now
	items, ok := result["items"].([]interface{})
	if !ok {
		t.Fatalf("items field missing or not an array")
	}
	if len(items) != 0 {
		t.Errorf("Expected 0 items (scan not implemented), got %d", len(items))
	}
}

func TestRestServer_CFOperations(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Create a new CF
	if err := db.CreateCF("testcf"); err != nil {
		t.Fatalf("Failed to create CF: %v", err)
	}

	srv := NewServer(db, []byte("test-secret"))

	// Put into testcf
	putBody := `{"value": "cf value", "cf": "testcf"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/kv/cfkey", bytes.NewReader([]byte(putBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("PUT with CF status = %d, want %d", w.Code, http.StatusOK)
	}

	// Get from testcf using query param
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
	defer db.Close()

	srv := NewServer(db, []byte("test-secret"))

	// GET non-existent key
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

	// Invalid JSON body
	req = httptest.NewRequest(http.MethodPut, "/api/v1/kv/key", bytes.NewReader([]byte("{")))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Invalid JSON status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	// Invalid path
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
	defer db.Close()

	srv := NewServer(db, []byte("test-secret"))

	// OPTIONS request
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
