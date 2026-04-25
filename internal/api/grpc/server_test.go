package grpc

import (
	"context"
	"testing"

	"scoriadb/pkg/scoria"
	"scoriadb/scoriadb/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServer_GetPut(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Create server
	srv := NewServer(db)

	// Test Put
	ctx := context.Background()
	putReq := &proto.PutRequest{
		Key:   []byte("test-key"),
		Value: []byte("test-value"),
		CfName: "default",
	}
	putResp, err := srv.Put(ctx, putReq)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if putResp.GetCommitTs() == 0 {
		t.Error("Expected commit timestamp > 0")
	}

	// Test Get
	getReq := &proto.GetRequest{
		Key:   []byte("test-key"),
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

	// Test Get non-existent key
	getReq2 := &proto.GetRequest{
		Key:   []byte("non-existent"),
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
	defer db.Close()

	srv := NewServer(db)
	ctx := context.Background()

	// First put a key
	_, err = srv.Put(ctx, &proto.PutRequest{
		Key:   []byte("to-delete"),
		Value: []byte("value"),
		CfName: "default",
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Delete it
	_, err = srv.Delete(ctx, &proto.DeleteRequest{
		Key:   []byte("to-delete"),
		CfName: "default",
	})
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	resp, err := srv.Get(ctx, &proto.GetRequest{
		Key:   []byte("to-delete"),
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
	defer db.Close()

	srv := NewServer(db)
	ctx := context.Background()

	// Begin transaction
	beginResp, err := srv.BeginTxn(ctx, &proto.BeginTxnRequest{})
	if err != nil {
		t.Fatalf("BeginTxn failed: %v", err)
	}
	txnID := beginResp.GetTxnId()
	if txnID == "" {
		t.Error("Expected non-empty transaction ID")
	}

	// Commit transaction (empty ops)
	_, err = srv.CommitTxn(ctx, &proto.CommitTxnRequest{
		TxnId: txnID,
	})
	if err != nil {
		t.Fatalf("CommitTxn failed: %v", err)
	}

	// Try to commit again (should fail)
	_, err = srv.CommitTxn(ctx, &proto.CommitTxnRequest{
		TxnId: txnID,
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("Expected NotFound error, got %v", err)
	}
}

func TestServer_UnimplementedMethods(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := scoria.NewScoriaDB(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	srv := NewServer(db)
	ctx := context.Background()

	// CreateUser should return Unimplemented
	_, err = srv.CreateUser(ctx, &proto.CreateUserRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("Expected Unimplemented error, got %v", err)
	}

	// Authenticate should return Unimplemented
	_, err = srv.Authenticate(ctx, &proto.AuthRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("Expected Unimplemented error, got %v", err)
	}
}