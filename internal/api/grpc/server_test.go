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
