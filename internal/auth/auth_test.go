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

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/pkg/scoria"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// mockServerStream implements grpc.ServerStream for testing.
type mockServerStream struct {
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func (m *mockServerStream) SendMsg(msg interface{}) error {
	return nil
}

func (m *mockServerStream) RecvMsg(msg interface{}) error {
	return nil
}

func (m *mockServerStream) SetHeader(metadata.MD) error {
	return nil
}

func (m *mockServerStream) SendHeader(metadata.MD) error {
	return nil
}

func (m *mockServerStream) SetTrailer(metadata.MD) {
}

func TestCreateUserAndLogin(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	jwtSecret := []byte("test-secret")

	err = CreateUser(db, "alice", "password123", []string{RoleReadWrite})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	token, err := Authenticate(db, "alice", "password123", jwtSecret)
	if err != nil {
		t.Fatalf("Authenticate failed with correct credentials: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}

	claims, err := ValidateToken(token, jwtSecret)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims.Username != "alice" {
		t.Errorf("expected username 'alice', got %s", claims.Username)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != RoleReadWrite {
		t.Errorf("expected role 'readwrite', got %v", claims.Roles)
	}

	_, err = Authenticate(db, "alice", "wrong", jwtSecret)
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}

	_, err = Authenticate(db, "bob", "password", jwtSecret)
	if err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestDuplicateUser(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	err = CreateUser(db, "alice", "pass", []string{RoleReadOnly})
	if err != nil {
		t.Fatalf("first CreateUser failed: %v", err)
	}

	err = CreateUser(db, "alice", "pass2", []string{RoleAdmin})
	if err != ErrUserAlreadyExists {
		t.Errorf("expected ErrUserAlreadyExists, got %v", err)
	}
}

func TestGetUser(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	err = CreateUser(db, "bob", "secret", []string{RoleAdmin, RoleReadWrite})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	user, err := GetUser(db, "bob")
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if user.Username != "bob" {
		t.Errorf("expected username 'bob', got %s", user.Username)
	}
	if len(user.Roles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(user.Roles))
	}

	_, err = GetUser(db, "nonexistent")
	if err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestListUsers(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	users, err := ListUsers(db)
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users, got %d", len(users))
	}

	err = CreateUser(db, "user1", "pass1", []string{RoleReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	err = CreateUser(db, "user2", "pass2", []string{RoleReadWrite})
	if err != nil {
		t.Fatal(err)
	}

	users, err = ListUsers(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}

	names := map[string]bool{}
	for _, u := range users {
		names[u.Username] = true
	}
	if !names["user1"] || !names["user2"] {
		t.Errorf("missing expected users, got %v", names)
	}
}

func TestUpdateUserRoles(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	err = CreateUser(db, "charlie", "pass", []string{RoleReadOnly})
	if err != nil {
		t.Fatal(err)
	}

	err = UpdateUserRoles(db, "charlie", []string{RoleAdmin, RoleReadWrite})
	if err != nil {
		t.Fatalf("UpdateUserRoles failed: %v", err)
	}

	user, err := GetUser(db, "charlie")
	if err != nil {
		t.Fatal(err)
	}
	if len(user.Roles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(user.Roles))
	}
	roleMap := map[string]bool{}
	for _, r := range user.Roles {
		roleMap[r] = true
	}
	if !roleMap[RoleAdmin] || !roleMap[RoleReadWrite] {
		t.Errorf("missing expected roles, got %v", user.Roles)
	}

	err = UpdateUserRoles(db, "charlie", []string{"superuser"})
	if err == nil {
		t.Error("expected error for invalid role")
	}
}

func TestDeleteUser(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	err = CreateUser(db, "dave", "pass", []string{RoleReadOnly})
	if err != nil {
		t.Fatal(err)
	}

	err = DeleteUser(db, "dave")
	if err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	_, err = GetUser(db, "dave")
	if err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound after deletion, got %v", err)
	}
}

func TestTokenExpiration(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	jwtSecret := []byte("test-secret")

	err = CreateUser(db, "eva", "pass", []string{RoleReadOnly})
	if err != nil {
		t.Fatal(err)
	}

	expiredTime := time.Now().Add(-1 * time.Hour)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Username: "eva",
		Roles:    []string{RoleReadOnly},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiredTime),
			IssuedAt:  jwt.NewNumericDate(expiredTime),
			Subject:   "eva",
		},
	})
	tokenStr, err := token.SignedString(jwtSecret)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ValidateToken(tokenStr, jwtSecret)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestRoleChecks(t *testing.T) {
	user := &User{
		Username: "test",
		Roles:    []string{RoleReadOnly, RoleAdmin},
	}

	if !HasAnyRole(user, []string{RoleReadOnly}) {
		t.Error("HasAnyRole should return true for readwrite")
	}
	if !HasAnyRole(user, []string{RoleAdmin}) {
		t.Error("HasAnyRole should return true for admin")
	}
	if HasAnyRole(user, []string{RoleReadWrite}) {
		t.Error("HasAnyRole should return false for readwrite (user doesn't have it)")
	}

	if !HasAllRoles(user, []string{RoleReadOnly}) {
		t.Error("HasAllRoles should return true for single role")
	}
	if !HasAllRoles(user, []string{RoleReadOnly, RoleAdmin}) {
		t.Error("HasAllRoles should return true for both roles")
	}
	if HasAllRoles(user, []string{RoleReadOnly, RoleReadWrite}) {
		t.Error("HasAllRoles should return false when missing readwrite")
	}
}

func TestInvalidRoleValidation(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	err = CreateUser(db, "invalid", "pass", []string{"superuser"})
	if err == nil {
		t.Error("expected error for invalid role")
	}
}

func TestChangePasswordSuccess(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	jwtSecret := []byte("test-secret")

	err = CreateUser(db, "alice", "oldpass", []string{RoleReadWrite})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	err = ChangePassword(db, "alice", "newpass")
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	_, err = Authenticate(db, "alice", "oldpass", jwtSecret)
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials for old password, got %v", err)
	}

	token, err := Authenticate(db, "alice", "newpass", jwtSecret)
	if err != nil {
		t.Fatalf("auth with new password failed: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
}

func TestChangePasswordUserNotFound(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	err = ChangePassword(db, "nonexistent", "anypass")
	if err != ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestChangePasswordPreservesRoles(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	jwtSecret := []byte("test-secret")

	err = CreateUser(db, "bob", "pass123", []string{RoleAdmin, RoleReadWrite})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	err = ChangePassword(db, "bob", "newpass456")
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	token, err := Authenticate(db, "bob", "newpass456", jwtSecret)
	if err != nil {
		t.Fatalf("auth failed: %v", err)
	}

	claims, err := ValidateToken(token, jwtSecret)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	hasAdmin := false
	hasReadWrite := false
	for _, role := range claims.Roles {
		if role == RoleAdmin {
			hasAdmin = true
		}
		if role == RoleReadWrite {
			hasReadWrite = true
		}
	}
	if !hasAdmin {
		t.Error("admin role not preserved after password change")
	}
	if !hasReadWrite {
		t.Error("readwrite role not preserved after password change")
	}
}

func TestChangePasswordEmptyNewPassword(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	err = CreateUser(db, "charlie", "oldpass", []string{RoleReadOnly})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	err = ChangePassword(db, "charlie", "")
	if err != ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials for empty password, got %v", err)
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{"valid bearer token", "Bearer mytoken123", "mytoken123"},
		{"bearer with extra spaces", "Bearer   token-with-spaces  ", "token-with-spaces"},
		{"no bearer prefix", "Basic dXNlcjpwYXNz", ""},
		{"empty header", "", ""},
		{"bearer only no token", "Bearer ", ""},
		{"lowercase bearer", "bearer token123", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBearerToken(tt.header)
			if result != tt.expected {
				t.Errorf("extractBearerToken(%q) = %q, want %q", tt.header, result, tt.expected)
			}
		})
	}
}

func TestValidateTokenInvalidSignature(t *testing.T) {
	jwtSecret := []byte("test-secret")
	wrongSecret := []byte("wrong-secret")

	claims := Claims{
		Username: "testuser",
		Roles:    []string{RoleReadOnly},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(jwtSecret)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ValidateToken(tokenStr, wrongSecret)
	if err == nil {
		t.Error("expected error when validating with wrong secret")
	}
}

func TestValidateTokenMalformed(t *testing.T) {
	jwtSecret := []byte("test-secret")

	_, err := ValidateToken("not-a-valid-token", jwtSecret)
	if err == nil {
		t.Error("expected error for malformed token")
	}

	_, err = ValidateToken("", jwtSecret)
	if err == nil {
		t.Error("expected error for empty token")
	}
}

func TestCreateUserEmptyFields(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	defer errors.CloseWithFatal(db, "auth-test-db")

	err = db.CreateCF(AuthCF)
	if err != nil {
		t.Fatalf("failed to create auth CF: %v", err)
	}

	err = CreateUser(db, "", "pass", []string{RoleReadOnly})
	if err == nil {
		t.Error("expected error for empty username")
	}

	err = CreateUser(db, "validuser", "", []string{RoleReadOnly})
	if err == nil {
		t.Error("expected error for empty password")
	}
}

func TestGetClaimsFromContext(t *testing.T) {
	ctx := context.Background()

	_, ok := GetClaimsFromContext(ctx)
	if ok {
		t.Error("expected false when no claims in context")
	}

	claims := &Claims{Username: "testuser", Roles: []string{RoleAdmin}}
	ctx = context.WithValue(ctx, ContextKeyUser, claims)

	result, ok := GetClaimsFromContext(ctx)
	if !ok {
		t.Error("expected true when claims are in context")
	}
	if result.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %q", result.Username)
	}
}

func TestGetClaimsFromHTTPRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	_, ok := GetClaimsFromHTTPRequest(r)
	if ok {
		t.Error("expected false when no claims in request")
	}

	claims := &Claims{Username: "testuser", Roles: []string{RoleReadWrite}}
	ctx := context.WithValue(r.Context(), HTTPContextKeyUser, claims)
	r = r.WithContext(ctx)

	result, ok := GetClaimsFromHTTPRequest(r)
	if !ok {
		t.Error("expected true when claims are in request")
	}
	if result.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %q", result.Username)
	}
}

func TestAuthMiddlewareValidToken(t *testing.T) {
	jwtSecret := []byte("test-secret")

	claims := Claims{
		Username: "testuser",
		Roles:    []string{RoleReadOnly},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(jwtSecret)
	if err != nil {
		t.Fatal(err)
	}

	middleware := AuthMiddleware(jwtSecret, nil)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaimsFromHTTPRequest(r)
		if !ok {
			t.Error("expected claims in context")
		}
		if claims.Username != "testuser" {
			t.Errorf("expected username 'testuser', got %q", claims.Username)
		}
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/kv/mykey", nil)
	r.Header.Set("Authorization", "Bearer "+tokenStr)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for valid token, got %d", w.Code)
	}
}

func TestAuthInterceptorValidToken(t *testing.T) {
	secret := []byte("test-secret")

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Username: "testuser",
		Roles:    []string{RoleAdmin},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   "testuser",
		},
	})
	tokenStr, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	interceptor := AuthInterceptor(secret, nil)
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		claims, ok := GetClaimsFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Internal, "no claims in context")
		}
		return claims.Username, nil
	}

	md := metadata.Pairs("authorization", "Bearer "+tokenStr)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	resp, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Method"}, handler)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp != "testuser" {
		t.Errorf("expected 'testuser', got %v", resp)
	}
}

func TestAuthInterceptorSkipMethod(t *testing.T) {
	secret := []byte("test-secret")
	skipMethods := map[string]bool{"/test.Method": true}
	interceptor := AuthInterceptor(secret, skipMethods)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	resp, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/test.Method"}, handler)
	if err != nil {
		t.Fatalf("expected no error for skipped method, got %v", err)
	}
	if resp != "ok" {
		t.Errorf("expected 'ok', got %v", resp)
	}
}

func TestAuthInterceptorMissingMetadata(t *testing.T) {
	secret := []byte("test-secret")
	interceptor := AuthInterceptor(secret, nil)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/test.Method"}, handler)
	if err == nil {
		t.Fatal("expected error for missing metadata")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestAuthInterceptorMissingAuthHeader(t *testing.T) {
	secret := []byte("test-secret")
	interceptor := AuthInterceptor(secret, nil)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	md := metadata.Pairs("other", "value")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Method"}, handler)
	if err == nil {
		t.Fatal("expected error for missing auth header")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestAuthInterceptorInvalidToken(t *testing.T) {
	secret := []byte("test-secret")
	interceptor := AuthInterceptor(secret, nil)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	md := metadata.Pairs("authorization", "Bearer invalid-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Method"}, handler)
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestAuthInterceptorInsufficientRole(t *testing.T) {
	secret := []byte("test-secret")
	interceptor := AuthInterceptor(secret, nil)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Username: "readonly",
		Roles:    []string{RoleReadOnly},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   "readonly",
		},
	})
	tokenStr, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	md := metadata.Pairs("authorization", "Bearer "+tokenStr)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err = interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/scoriadb.ScoriaDB/CreateUser"}, handler)
	if err == nil {
		t.Fatal("expected error for insufficient role")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", status.Code(err))
	}
}

func TestStreamAuthInterceptorSkipMethod(t *testing.T) {
	secret := []byte("test-secret")
	skipMethods := map[string]bool{"/test.Stream": true}
	interceptor := StreamAuthInterceptor(secret, skipMethods)

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	err := interceptor(nil, nil, &grpc.StreamServerInfo{FullMethod: "/test.Stream"}, handler)
	if err != nil {
		t.Fatalf("expected no error for skipped method, got %v", err)
	}
}

func TestStreamAuthInterceptorMissingMetadata(t *testing.T) {
	secret := []byte("test-secret")
	interceptor := StreamAuthInterceptor(secret, nil)

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	ss := &mockServerStream{ctx: context.Background()}
	err := interceptor(nil, ss, &grpc.StreamServerInfo{FullMethod: "/test.Stream"}, handler)
	if err == nil {
		t.Fatal("expected error for missing metadata")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestStreamAuthInterceptorMissingAuthHeader(t *testing.T) {
	secret := []byte("test-secret")
	interceptor := StreamAuthInterceptor(secret, nil)

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	md := metadata.Pairs("other", "value")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ss := &mockServerStream{ctx: ctx}

	err := interceptor(nil, ss, &grpc.StreamServerInfo{FullMethod: "/test.Stream"}, handler)
	if err == nil {
		t.Fatal("expected error for missing auth header")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestStreamAuthInterceptorInvalidToken(t *testing.T) {
	secret := []byte("test-secret")
	interceptor := StreamAuthInterceptor(secret, nil)

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	md := metadata.Pairs("authorization", "Bearer invalid-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ss := &mockServerStream{ctx: ctx}

	err := interceptor(nil, ss, &grpc.StreamServerInfo{FullMethod: "/test.Stream"}, handler)
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestStreamAuthInterceptorValidToken(t *testing.T) {
	secret := []byte("test-secret")
	interceptor := StreamAuthInterceptor(secret, nil)

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		claims, ok := GetClaimsFromContext(ss.Context())
		if !ok {
			return status.Error(codes.Internal, "no claims in context")
		}
		if claims.Username != "testuser" {
			return status.Error(codes.PermissionDenied, "wrong user")
		}
		return nil
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Username: "testuser",
		Roles:    []string{RoleAdmin},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   "testuser",
		},
	})
	tokenStr, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	md := metadata.Pairs("authorization", "Bearer "+tokenStr)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ss := &mockServerStream{ctx: ctx}

	err = interceptor(nil, ss, &grpc.StreamServerInfo{FullMethod: "/test.Method"}, handler)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestStreamAuthInterceptorInsufficientRole(t *testing.T) {
	secret := []byte("test-secret")
	interceptor := StreamAuthInterceptor(secret, nil)

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		return nil
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Username: "readonly",
		Roles:    []string{RoleReadOnly},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   "readonly",
		},
	})
	tokenStr, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	md := metadata.Pairs("authorization", "Bearer "+tokenStr)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ss := &mockServerStream{ctx: ctx}

	err = interceptor(nil, ss, &grpc.StreamServerInfo{FullMethod: "/scoriadb.ScoriaDB/CreateUser"}, handler)
	if err == nil {
		t.Fatal("expected error for insufficient role")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", status.Code(err))
	}
}

func TestHasRequiredRoleForMethod(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		roles    []string
		expected bool
	}{
		{
			name:     "Get with read only",
			method:   "/scoriadb.ScoriaDB/Get",
			roles:    []string{RoleReadOnly},
			expected: true,
		},
		{
			name:     "Put with read only",
			method:   "/scoriadb.ScoriaDB/Put",
			roles:    []string{RoleReadOnly},
			expected: false,
		},
		{
			name:     "Put with read write",
			method:   "/scoriadb.ScoriaDB/Put",
			roles:    []string{RoleReadWrite},
			expected: true,
		},
		{
			name:     "Delete with admin",
			method:   "/scoriadb.ScoriaDB/Delete",
			roles:    []string{RoleAdmin},
			expected: true,
		},
		{
			name:     "CreateUser with read write",
			method:   "/scoriadb.ScoriaDB/CreateUser",
			roles:    []string{RoleReadWrite},
			expected: false,
		},
		{
			name:     "CreateUser with admin",
			method:   "/scoriadb.ScoriaDB/CreateUser",
			roles:    []string{RoleAdmin},
			expected: true,
		},
		{
			name:     "Authenticate always allowed",
			method:   "/scoriadb.ScoriaDB/Authenticate",
			roles:    []string{},
			expected: true,
		},
		{
			name:     "Unknown method requires admin",
			method:   "/scoriadb.ScoriaDB/UnknownMethod",
			roles:    []string{RoleReadOnly},
			expected: false,
		},
		{
			name:     "Unknown method with admin",
			method:   "/scoriadb.ScoriaDB/UnknownMethod",
			roles:    []string{RoleAdmin},
			expected: true,
		},
		{
			name:     "Scan with read only",
			method:   "/scoriadb.ScoriaDB/Scan",
			roles:    []string{RoleReadOnly},
			expected: true,
		},
		{
			name:     "BeginTxn with read only",
			method:   "/scoriadb.ScoriaDB/BeginTxn",
			roles:    []string{RoleReadOnly},
			expected: false,
		},
		{
			name:     "BeginTxn with read write",
			method:   "/scoriadb.ScoriaDB/BeginTxn",
			roles:    []string{RoleReadWrite},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasRequiredRoleForMethod(tt.method, tt.roles)
			if result != tt.expected {
				t.Errorf("hasRequiredRoleForMethod(%q, %v) = %v, want %v", tt.method, tt.roles, result, tt.expected)
			}
		})
	}
}

func TestHasRequiredRoleForPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		method   string
		roles    []string
		expected bool
	}{
		{
			name:     "GET kv with read only",
			path:     "/api/v1/kv/mykey",
			method:   "GET",
			roles:    []string{RoleReadOnly},
			expected: true,
		},
		{
			name:     "PUT kv with read only",
			path:     "/api/v1/kv/mykey",
			method:   "PUT",
			roles:    []string{RoleReadOnly},
			expected: false,
		},
		{
			name:     "PUT kv with read write",
			path:     "/api/v1/kv/mykey",
			method:   "PUT",
			roles:    []string{RoleReadWrite},
			expected: true,
		},
		{
			name:     "DELETE kv with admin",
			path:     "/api/v1/kv/mykey",
			method:   "DELETE",
			roles:    []string{RoleAdmin},
			expected: true,
		},
		{
			name:     "POST scan with read only",
			path:     "/api/v1/kv/scan",
			method:   "POST",
			roles:    []string{RoleReadOnly},
			expected: true,
		},
		{
			name:     "login always allowed",
			path:     "/api/v1/auth/login",
			method:   "POST",
			roles:    []string{},
			expected: true,
		},
		{
			name:     "admin path requires admin",
			path:     "/api/v1/admin/users",
			method:   "GET",
			roles:    []string{RoleReadWrite},
			expected: false,
		},
		{
			name:     "admin path with admin role",
			path:     "/api/v1/admin/users",
			method:   "GET",
			roles:    []string{RoleAdmin},
			expected: true,
		},
		{
			name:     "unknown path requires admin",
			path:     "/api/v1/unknown",
			method:   "GET",
			roles:    []string{RoleReadOnly},
			expected: false,
		},
		{
			name:     "POST batch with read only",
			path:     "/api/v1/kv/batch",
			method:   "POST",
			roles:    []string{RoleReadOnly},
			expected: false,
		},
		{
			name:     "POST batch with read write",
			path:     "/api/v1/kv/batch",
			method:   "POST",
			roles:    []string{RoleReadWrite},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasRequiredRoleForPath(tt.path, tt.method, tt.roles)
			if result != tt.expected {
				t.Errorf("hasRequiredRoleForPath(%q, %q, %v) = %v, want %v", tt.path, tt.method, tt.roles, result, tt.expected)
			}
		})
	}
}

func TestGetErrorCode(t *testing.T) {
	tests := []struct {
		status   int
		expected string
	}{
		{status: http.StatusUnauthorized, expected: "UNAUTHORIZED"},
		{status: http.StatusForbidden, expected: "FORBIDDEN"},
		{status: http.StatusInternalServerError, expected: "AUTH_ERROR"},
		{status: http.StatusBadRequest, expected: "AUTH_ERROR"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := getErrorCode(tt.status)
			if result != tt.expected {
				t.Errorf("getErrorCode(%d) = %q, want %q", tt.status, result, tt.expected)
			}
		})
	}
}

func TestWriteAuthError(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
	}{
		{
			name:    "unauthorized",
			status:  http.StatusUnauthorized,
			message: "missing token",
		},
		{
			name:    "forbidden",
			status:  http.StatusForbidden,
			message: "insufficient privileges",
		},
		{
			name:    "internal error",
			status:  http.StatusInternalServerError,
			message: "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeAuthError(w, tt.status, tt.message)

			if w.Code != tt.status {
				t.Errorf("expected status %d, got %d", tt.status, w.Code)
			}

			contentType := w.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", contentType)
			}

			body := w.Body.String()
			if body == "" {
				t.Error("expected non-empty body")
			}
		})
	}
}

func TestAuthMiddlewareSkipPath(t *testing.T) {
	jwtSecret := []byte("test-secret")
	skipPaths := map[string]bool{
		"/api/v1/auth/login": true,
	}

	middleware := AuthMiddleware(jwtSecret, skipPaths)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for skipped path, got %d", w.Code)
	}
}

func TestAuthMiddlewareMissingHeader(t *testing.T) {
	jwtSecret := []byte("test-secret")
	skipPaths := map[string]bool{}

	middleware := AuthMiddleware(jwtSecret, skipPaths)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/kv/mykey", nil)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for missing header, got %d", w.Code)
	}
}

func TestAuthMiddlewareInvalidToken(t *testing.T) {
	jwtSecret := []byte("test-secret")
	skipPaths := map[string]bool{}

	middleware := AuthMiddleware(jwtSecret, skipPaths)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/kv/mykey", nil)
	r.Header.Set("Authorization", "Bearer invalid-token")
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for invalid token, got %d", w.Code)
	}
}

func TestAuthMiddlewareInsufficientRole(t *testing.T) {
	jwtSecret := []byte("test-secret")
	skipPaths := map[string]bool{}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Username: "readonly",
		Roles:    []string{RoleReadOnly},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   "readonly",
		},
	})
	tokenStr, err := token.SignedString(jwtSecret)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	middleware := AuthMiddleware(jwtSecret, skipPaths)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Try to access admin-only path with readonly role
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/api/v1/kv/mykey", nil)
	r.Header.Set("Authorization", "Bearer "+tokenStr)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403 for insufficient role, got %d", w.Code)
	}
}
