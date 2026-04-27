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
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"scoriadb/internal/auth"
	"scoriadb/pkg/scoria"
	"scoriadb/scoriadb/proto"
)

// server implements the ScoriaDB gRPC service.
type server struct {
	proto.UnimplementedScoriaDBServer
	db        scoria.CFDB
	jwtSecret []byte

	// In-memory transaction store (for demo purposes; in production would be more robust)
	txns   map[string]scoria.Transaction
	txnsMu sync.RWMutex
}

// NewServer creates a new gRPC server that delegates to the given CFDB.
func NewServer(db scoria.CFDB, jwtSecret []byte) *server {
	return &server{
		db:        db,
		jwtSecret: jwtSecret,
		txns:      make(map[string]scoria.Transaction),
	}
}

// Get handles Get RPC.
func (s *server) Get(ctx context.Context, req *proto.GetRequest) (*proto.GetResponse, error) {
	cfName := req.GetCfName()
	if cfName == "" {
		cfName = "default"
	}

	value, err := s.db.GetCF(cfName, req.GetKey())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get key: %v", err)
	}

	if value == nil {
		return &proto.GetResponse{Found: false}, nil
	}

	return &proto.GetResponse{
		Value: value,
		Found: true,
	}, nil
}

// Put handles Put RPC.
func (s *server) Put(ctx context.Context, req *proto.PutRequest) (*proto.PutResponse, error) {
	cfName := req.GetCfName()
	if cfName == "" {
		cfName = "default"
	}

	err := s.db.PutCF(cfName, req.GetKey(), req.GetValue())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to put key: %v", err)
	}

	// TODO: return actual commit timestamp
	return &proto.PutResponse{CommitTs: 1}, nil
}

// Delete handles Delete RPC.
func (s *server) Delete(ctx context.Context, req *proto.DeleteRequest) (*proto.DeleteResponse, error) {
	cfName := req.GetCfName()
	if cfName == "" {
		cfName = "default"
	}

	err := s.db.DeleteCF(cfName, req.GetKey())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete key: %v", err)
	}

	// TODO: return actual commit timestamp
	return &proto.DeleteResponse{CommitTs: 1}, nil
}

// Scan handles Scan RPC (server-side streaming).
func (s *server) Scan(req *proto.ScanRequest, stream grpc.ServerStreamingServer[proto.ScanResponse]) error {
	cfName := req.GetCfName()
	if cfName == "" {
		cfName = "default"
	}

	// Get iterator for prefix scanning
	iter := s.db.ScanCF(cfName, req.GetPrefix())
	defer iter.Close()

	for iter.Next() {
		err := stream.Send(&proto.ScanResponse{
			Key:   iter.Key(),
			Value: iter.Value(),
		})
		if err != nil {
			return err
		}
	}

	if err := iter.Err(); err != nil {
		return status.Errorf(codes.Internal, "scan iteration error: %v", err)
	}

	return nil
}

// BeginTxn handles BeginTxn RPC.
func (s *server) BeginTxn(ctx context.Context, req *proto.BeginTxnRequest) (*proto.BeginTxnResponse, error) {
	txn := s.db.NewTransaction()
	txnID := s.generateTxnID()

	s.txnsMu.Lock()
	s.txns[txnID] = txn
	s.txnsMu.Unlock()

	// TODO: generate actual start timestamp
	startTS := uint64(1)

	return &proto.BeginTxnResponse{
		TxnId:   txnID,
		StartTs: int64(startTS),
	}, nil
}

// CommitTxn handles CommitTxn RPC.
func (s *server) CommitTxn(ctx context.Context, req *proto.CommitTxnRequest) (*proto.CommitTxnResponse, error) {
	txnID := req.GetTxnId()

	s.txnsMu.Lock()
	txn, ok := s.txns[txnID]
	if !ok {
		s.txnsMu.Unlock()
		return nil, status.Errorf(codes.NotFound, "transaction %s not found", txnID)
	}
	delete(s.txns, txnID)
	s.txnsMu.Unlock()

	// Apply operations
	// TODO: actually apply the operations from req.GetOps()
	// For now, just commit the transaction
	err := txn.Commit()
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "transaction commit failed: %v", err)
	}

	return &proto.CommitTxnResponse{}, nil
}

// RollbackTxn handles RollbackTxn RPC.
func (s *server) RollbackTxn(ctx context.Context, req *proto.RollbackTxnRequest) (*proto.RollbackTxnResponse, error) {
	txnID := req.GetTxnId()

	s.txnsMu.Lock()
	txn, ok := s.txns[txnID]
	if !ok {
		s.txnsMu.Unlock()
		return nil, status.Errorf(codes.NotFound, "transaction %s not found", txnID)
	}
	delete(s.txns, txnID)
	s.txnsMu.Unlock()

	err := txn.Rollback()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "transaction rollback failed: %v", err)
	}

	return &proto.RollbackTxnResponse{}, nil
}

// CreateUser handles CreateUser RPC.
func (s *server) CreateUser(ctx context.Context, req *proto.CreateUserRequest) (*proto.CreateUserResponse, error) {
	// Проверяем, что вызывающий пользователь имеет роль admin
	claims, ok := auth.GetClaimsFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	if !auth.HasAnyRole(&auth.User{Roles: claims.Roles}, []string{auth.RoleAdmin}) {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}

	// Создаём пользователя
	err := auth.CreateUser(s.db, req.GetUsername(), req.GetPassword(), req.GetRoles())
	if err != nil {
		switch err {
		case auth.ErrUserAlreadyExists:
			return nil, status.Error(codes.AlreadyExists, "user already exists")
		case auth.ErrInvalidCredentials:
			return nil, status.Error(codes.InvalidArgument, "invalid credentials")
		default:
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &proto.CreateUserResponse{}, nil
}

// Authenticate handles Authenticate RPC.
func (s *server) Authenticate(ctx context.Context, req *proto.AuthRequest) (*proto.AuthResponse, error) {
	// Аутентификация не требует токена
	token, err := auth.Authenticate(s.db, req.GetUsername(), req.GetPassword(), s.jwtSecret)
	if err != nil {
		switch err {
		case auth.ErrUserNotFound, auth.ErrInvalidCredentials:
			return nil, status.Error(codes.Unauthenticated, "invalid username or password")
		default:
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &proto.AuthResponse{JwtToken: token}, nil
}

// generateTxnID generates a simple transaction ID (for demo purposes).
func (s *server) generateTxnID() string {
	// TODO: use proper UUID
	return fmt.Sprintf("txn-%d", len(s.txns)+1)
}