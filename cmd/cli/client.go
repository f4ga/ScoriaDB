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
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"scoriadb/scoriadb/proto"
)

// Ensure jwtCredentials implements credentials.PerRPCCredentials.
var _ credentials.PerRPCCredentials = jwtCredentials{}

// jwtCredentials implements credentials.PerRPCCredentials for JWT authentication.
type jwtCredentials struct {
	token string
}

// GetRequestMetadata returns the authorization header.
func (c jwtCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": "Bearer " + c.token,
	}, nil
}

// RequireTransportSecurity returns false because we're using insecure transport for simplicity.
func (c jwtCredentials) RequireTransportSecurity() bool {
	return false
}

// Client wraps the gRPC connection and provides convenient methods.
type Client struct {
	conn   *grpc.ClientConn
	client proto.ScoriaDBClient
	token  string
}

// NewClient creates a new gRPC client.
func NewClient(addr, token string) (*Client, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(jwtCredentials{token}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return &Client{
		conn:   conn,
		client: proto.NewScoriaDBClient(conn),
		token:  token,
	}, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Get performs a Get request.
func (c *Client) Get(ctx context.Context, key []byte, cfName string) (*proto.GetResponse, error) {
	req := &proto.GetRequest{
		Key:    key,
		CfName: cfName,
	}
	return c.client.Get(ctx, req)
}

// Put performs a Put request.
func (c *Client) Put(ctx context.Context, key, value []byte, cfName string) (*proto.PutResponse, error) {
	req := &proto.PutRequest{
		Key:    key,
		Value:  value,
		CfName: cfName,
	}
	return c.client.Put(ctx, req)
}

// Delete performs a Delete request.
func (c *Client) Delete(ctx context.Context, key []byte, cfName string) (*proto.DeleteResponse, error) {
	req := &proto.DeleteRequest{
		Key:    key,
		CfName: cfName,
	}
	return c.client.Delete(ctx, req)
}

// Scan performs a Scan request and returns all results.
func (c *Client) Scan(ctx context.Context, prefix []byte, cfName string) ([]*proto.ScanResponse, error) {
	req := &proto.ScanRequest{
		Prefix: prefix,
		CfName: cfName,
	}
	stream, err := c.client.Scan(ctx, req)
	if err != nil {
		return nil, err
	}

	var results []*proto.ScanResponse
	for {
		resp, err := stream.Recv()
		if err != nil {
			break
		}
		results = append(results, resp)
	}
	return results, nil
}

// BeginTxn starts a new transaction.
func (c *Client) BeginTxn(ctx context.Context) (*proto.BeginTxnResponse, error) {
	req := &proto.BeginTxnRequest{}
	return c.client.BeginTxn(ctx, req)
}

// CommitTxn commits a transaction.
func (c *Client) CommitTxn(ctx context.Context, txnID string, ops []*proto.TxnOp) (*proto.CommitTxnResponse, error) {
	req := &proto.CommitTxnRequest{
		TxnId: txnID,
		Ops:   ops,
	}
	return c.client.CommitTxn(ctx, req)
}

// RollbackTxn rolls back a transaction.
func (c *Client) RollbackTxn(ctx context.Context, txnID string) (*proto.RollbackTxnResponse, error) {
	req := &proto.RollbackTxnRequest{
		TxnId: txnID,
	}
	return c.client.RollbackTxn(ctx, req)
}

// CreateUser creates a new user.
func (c *Client) CreateUser(ctx context.Context, username, password string, roles []string) (*proto.CreateUserResponse, error) {
	req := &proto.CreateUserRequest{
		Username: username,
		Password: password,
		Roles:    roles,
	}
	return c.client.CreateUser(ctx, req)
}

// Authenticate performs authentication and returns JWT token.
func (c *Client) Authenticate(ctx context.Context, username, password string) (*proto.AuthResponse, error) {
	req := &proto.AuthRequest{
		Username: username,
		Password: password,
	}
	return c.client.Authenticate(ctx, req)
}

// defaultContext returns a context with timeout.
func defaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}
