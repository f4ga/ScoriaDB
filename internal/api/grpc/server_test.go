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

package grpc

import (
	"context"
	"fmt"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/auth"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/pkg/scoria"
	"github.com/f4ga/ScoriaDB/scoriadb/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// mockScanStream implements proto.ScoriaDB_ScanServer for testing.
type mockScanStream struct {
	ctx       context.Context
	responses []*proto.ScanResponse
	sent      int
}

func (m *mockScanStream) Send(resp *proto.ScanResponse) error {
	m.responses = append(m.responses, resp)
	m.sent++
	return nil
}

func (m *mockScanStream) Context() context.Context {
	return m.ctx
}

func (m *mockScanStream) RecvMsg(msg interface{}) error {
	return nil
}

func (m *mockScanStream) SendMsg(msg interface{}) error {
	return nil
}

func (m *mockScanStream) SendHeader(metadata.MD) error {
	return nil
}

func (m *mockScanStream) SetTrailer(metadata.MD) {}

func (m *mockScanStream) SetHeader(metadata.MD) error {
	return nil
}

func TestServer_GetPut(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	srv := NewServer(db, []byte("test-secret"))

	ctx := context.Background()
	putReq := &proto.PutRequest{
		Key:    []byte("test-key"),
		Value:  []byte("test-value"),
		CfName: "default",
	}
	putResp, err := srv.Put(ctx, putReq)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if putResp.GetCommitTs() == 0 {
		t.Error("Expected commit timestamp > 0")
	}

	getReq := &proto.GetRequest{
		Key:    []byte("test-key"),
		CfName: "default",
	}
	getResp, err := srv.Get(ctx, getReq)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !getResp.GetFound() {
		t.Error("Expected key to be found")
	}
	if string(getResp.GetValue()) != "test-value" {
		t.Errorf("Expected value 'test-value', got %q", string(getResp.GetValue()))
	}

	getReq2 := &proto.GetRequest{
		Key:    []byte("non-existent"),
		CfName: "default",
	}
	getResp2, err := srv.Get(ctx, getReq2)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if getResp2.GetFound() {
		t.Error("Expected key not to be found")
	}
}

func TestServer_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	_, err = srv.Put(ctx, &proto.PutRequest{
		Key:    []byte("to-delete"),
		Value:  []byte("value"),
		CfName: "default",
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	_, err = srv.Delete(ctx, &proto.DeleteRequest{
		Key:    []byte("to-delete"),
		CfName: "default",
	})
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	resp, err := srv.Get(ctx, &proto.GetRequest{
		Key:    []byte("to-delete"),
		CfName: "default",
	})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if resp.GetFound() {
		t.Error("Expected key to be deleted")
	}
}

func TestServer_BeginCommitTxn(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	beginResp, err := srv.BeginTxn(ctx, &proto.BeginTxnRequest{})
	if err != nil {
		t.Fatalf("BeginTxn failed: %v", err)
	}
	txnID := beginResp.GetTxnId()
	if txnID == "" {
		t.Error("Expected non-empty transaction ID")
	}

	_, err = srv.CommitTxn(ctx, &proto.CommitTxnRequest{
		TxnId: txnID,
	})
	if err != nil {
		t.Fatalf("CommitTxn failed: %v", err)
	}

	_, err = srv.CommitTxn(ctx, &proto.CommitTxnRequest{
		TxnId: txnID,
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("Expected NotFound error, got %v", err)
	}
}

func TestServer_Scan(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("scan:%d", i))
		if err := db.Put(key, []byte(fmt.Sprintf("value:%d", i))); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	req := &proto.ScanRequest{
		Prefix: []byte("scan:"),
		CfName: "default",
	}
	stream := &mockScanStream{ctx: ctx}
	err = srv.Scan(req, stream)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(stream.responses) != 10 {
		t.Errorf("expected 10 responses, got %d", len(stream.responses))
	}
	for i, resp := range stream.responses {
		expectedKey := fmt.Sprintf("scan:%d", i)
		if string(resp.Key) != expectedKey {
			t.Errorf("response %d: expected key %s, got %s", i, expectedKey, resp.Key)
		}
	}
}

func TestServer_ScanEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	req := &proto.ScanRequest{
		Prefix: []byte("nonexistent:"),
		CfName: "default",
	}
	stream := &mockScanStream{ctx: ctx}
	err = srv.Scan(req, stream)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(stream.responses) != 0 {
		t.Errorf("expected 0 responses, got %d", len(stream.responses))
	}
}

func TestServer_CreateCF(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	req := &proto.CreateCFRequest{Name: "testcf"}
	_, err = srv.CreateCF(ctx, req)
	if err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	_, err = srv.Get(ctx, &proto.GetRequest{
		Key:    []byte("test"),
		CfName: "testcf",
	})
	if err != nil {
		t.Fatalf("Get from new CF failed: %v", err)
	}
}

func TestServer_ListCF(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	_, err = srv.CreateCF(ctx, &proto.CreateCFRequest{Name: "cf1"})
	if err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}
	_, err = srv.CreateCF(ctx, &proto.CreateCFRequest{Name: "cf2"})
	if err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	resp, err := srv.ListCF(ctx, &proto.ListCFRequest{})
	if err != nil {
		t.Fatalf("ListCF failed: %v", err)
	}

	found := make(map[string]bool)
	for _, name := range resp.CfNames {
		found[name] = true
	}
	if !found["default"] {
		t.Error("expected 'default' CF in list")
	}
	if !found["cf1"] {
		t.Error("expected 'cf1' CF in list")
	}
	if !found["cf2"] {
		t.Error("expected 'cf2' CF in list")
	}
}

func TestServer_DeleteCF(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	_, err = srv.CreateCF(ctx, &proto.CreateCFRequest{Name: "todelete"})
	if err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	_, err = srv.DeleteCF(ctx, &proto.DeleteCFRequest{Name: "todelete"})
	if err != nil {
		t.Fatalf("DeleteCF failed: %v", err)
	}
}

func TestServer_RollbackTxn(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	beginResp, err := srv.BeginTxn(ctx, &proto.BeginTxnRequest{})
	if err != nil {
		t.Fatalf("BeginTxn failed: %v", err)
	}
	txnID := beginResp.GetTxnId()

	_, err = srv.RollbackTxn(ctx, &proto.RollbackTxnRequest{
		TxnId: txnID,
	})
	if err != nil {
		t.Fatalf("RollbackTxn failed: %v", err)
	}

	// Second rollback should fail (txn already removed)
	_, err = srv.RollbackTxn(ctx, &proto.RollbackTxnRequest{
		TxnId: txnID,
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("Expected NotFound error, got %v", err)
	}
}

func TestServer_Authenticate(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	// Create auth CF and a user
	err = db.CreateCF("__auth__")
	if err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	// Create user via auth package directly
	err = auth.CreateUser(db, "testuser", "testpass", []string{auth.RoleReadOnly})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Authenticate with correct credentials
	resp, err := srv.Authenticate(ctx, &proto.AuthRequest{
		Username: "testuser",
		Password: "testpass",
	})
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if resp.GetJwtToken() == "" {
		t.Error("expected non-empty JWT token")
	}

	// Authenticate with wrong password
	_, err = srv.Authenticate(ctx, &proto.AuthRequest{
		Username: "testuser",
		Password: "wrongpass",
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", err)
	}

	// Authenticate with non-existent user
	_, err = srv.Authenticate(ctx, &proto.AuthRequest{
		Username: "nonexistent",
		Password: "pass",
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", err)
	}
}

func TestServer_CreateUser(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	err = db.CreateCF("__auth__")
	if err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	srv := NewServer(db, []byte("test-secret"))

	// Create admin user first
	err = auth.CreateUser(db, "admin", "adminpass", []string{auth.RoleAdmin})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Create context with admin claims
	adminClaims := &auth.Claims{Username: "admin", Roles: []string{auth.RoleAdmin}}
	ctx := context.WithValue(context.Background(), auth.ContextKeyUser, adminClaims)

	// Create a new user
	_, err = srv.CreateUser(ctx, &proto.CreateUserRequest{
		Username: "newuser",
		Password: "newpass",
		Roles:    []string{auth.RoleReadOnly},
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Create duplicate user
	_, err = srv.CreateUser(ctx, &proto.CreateUserRequest{
		Username: "newuser",
		Password: "newpass",
		Roles:    []string{auth.RoleReadOnly},
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Errorf("expected AlreadyExists, got %v", err)
	}

	// Create user without auth context
	_, err = srv.CreateUser(context.Background(), &proto.CreateUserRequest{
		Username: "unauth",
		Password: "pass",
		Roles:    []string{auth.RoleReadOnly},
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", err)
	}

	// Create user with non-admin role
	readonlyClaims := &auth.Claims{Username: "reader", Roles: []string{auth.RoleReadOnly}}
	readonlyCtx := context.WithValue(context.Background(), auth.ContextKeyUser, readonlyClaims)
	_, err = srv.CreateUser(readonlyCtx, &proto.CreateUserRequest{
		Username: "another",
		Password: "pass",
		Roles:    []string{auth.RoleReadOnly},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", err)
	}
}

func TestServer_ChangePassword(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	err = db.CreateCF("__auth__")
	if err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	srv := NewServer(db, []byte("test-secret"))

	// Create admin and a regular user
	err = auth.CreateUser(db, "admin", "adminpass", []string{auth.RoleAdmin})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	err = auth.CreateUser(db, "targetuser", "oldpass", []string{auth.RoleReadOnly})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	adminClaims := &auth.Claims{Username: "admin", Roles: []string{auth.RoleAdmin}}
	ctx := context.WithValue(context.Background(), auth.ContextKeyUser, adminClaims)

	// Change password
	_, err = srv.ChangePassword(ctx, &proto.ChangePasswordRequest{
		Username:    "targetuser",
		NewPassword: "newpass",
	})
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	// Verify new password works
	_, err = srv.Authenticate(context.Background(), &proto.AuthRequest{
		Username: "targetuser",
		Password: "newpass",
	})
	if err != nil {
		t.Errorf("auth with new password failed: %v", err)
	}

	// Change password for non-existent user
	_, err = srv.ChangePassword(ctx, &proto.ChangePasswordRequest{
		Username:    "nonexistent",
		NewPassword: "newpass",
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}

	// Change password without auth
	_, err = srv.ChangePassword(context.Background(), &proto.ChangePasswordRequest{
		Username:    "targetuser",
		NewPassword: "newpass",
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", err)
	}
}

func TestServer_ListUsers(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	err = db.CreateCF("__auth__")
	if err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	srv := NewServer(db, []byte("test-secret"))

	// Create users
	err = auth.CreateUser(db, "user1", "pass1", []string{auth.RoleReadOnly})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	err = auth.CreateUser(db, "user2", "pass2", []string{auth.RoleReadWrite})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	adminClaims := &auth.Claims{Username: "admin", Roles: []string{auth.RoleAdmin}}
	ctx := context.WithValue(context.Background(), auth.ContextKeyUser, adminClaims)

	resp, err := srv.ListUsers(ctx, &proto.ListUsersRequest{})
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}

	if len(resp.Users) != 2 {
		t.Errorf("expected 2 users, got %d", len(resp.Users))
	}

	// List users without auth
	_, err = srv.ListUsers(context.Background(), &proto.ListUsersRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", err)
	}

	// List users with non-admin role
	readonlyClaims := &auth.Claims{Username: "reader", Roles: []string{auth.RoleReadOnly}}
	readonlyCtx := context.WithValue(context.Background(), auth.ContextKeyUser, readonlyClaims)
	_, err = srv.ListUsers(readonlyCtx, &proto.ListUsersRequest{})
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", err)
	}
}

func TestServer_CreateCF_EmptyName(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	_, err = srv.CreateCF(ctx, &proto.CreateCFRequest{Name: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", err)
	}
}

func TestServer_GetWithCF(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	// Create custom CF first
	_, err = srv.CreateCF(ctx, &proto.CreateCFRequest{Name: "customcf"})
	if err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	// Put with custom CF
	_, err = srv.Put(ctx, &proto.PutRequest{
		Key:    []byte("key1"),
		Value:  []byte("val1"),
		CfName: "customcf",
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get with custom CF
	resp, err := srv.Get(ctx, &proto.GetRequest{
		Key:    []byte("key1"),
		CfName: "customcf",
	})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !resp.GetFound() {
		t.Error("expected key to be found")
	}
	if string(resp.GetValue()) != "val1" {
		t.Errorf("expected 'val1', got %q", string(resp.GetValue()))
	}

	// Get with empty CF name (should default to "default")
	resp, err = srv.Get(ctx, &proto.GetRequest{
		Key:    []byte("key1"),
		CfName: "",
	})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if resp.GetFound() {
		t.Error("expected key not to be found in default CF")
	}
}

func TestServer_DeleteCF_System(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer errors.CloseWithFatal(db, "grpc-test-db")

	srv := NewServer(db, []byte("test-secret"))
	ctx := context.Background()

	_, err = srv.DeleteCF(ctx, &proto.DeleteCFRequest{Name: "__auth__"})
	if err == nil {
		t.Error("expected error when deleting system CF")
	}
}
