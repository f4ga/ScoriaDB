package main

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"scoriadb/scoriadb/proto"
)

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
		grpc.WithUnaryInterceptor(authUnaryInterceptor(token)),
		grpc.WithStreamInterceptor(authStreamInterceptor(token)),
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

// authUnaryInterceptor adds Authorization header to unary RPCs.
func authUnaryInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if token != "" {
			ctx = context.WithValue(ctx, "authorization", "Bearer "+token)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// authStreamInterceptor adds Authorization header to streaming RPCs.
func authStreamInterceptor(token string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if token != "" {
			ctx = context.WithValue(ctx, "authorization", "Bearer "+token)
		}
		return streamer(ctx, desc, cc, method, opts...)
	}
}

// defaultContext returns a context with timeout.
func defaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}