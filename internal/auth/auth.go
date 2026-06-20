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

// Package auth provides authentication and authorization for ScoriaDB,
// including user management, JWT token generation/validation, and role-based
// access control (RBAC).
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/f4ga/ScoriaDB/pkg/scoria"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// User represents a system user with credentials and roles.
type User struct {
	Username     string   `json:"username"`
	PasswordHash string   `json:"password_hash"`
	Roles        []string `json:"roles"`
	CreatedAt    int64    `json:"created_at"` // Unix timestamp
}

// Claims represents JWT claims containing user information.
type Claims struct {
	Username string   `json:"sub"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

// Role constants for RBAC.
const (
	RoleAdmin     = "admin"
	RoleReadWrite = "readwrite"
	RoleReadOnly  = "readonly"
)

// AuthCF is the system Column Family for storing user credentials and roles.
const AuthCF = "__auth__"

// userKey returns the CF key prefix for a given username.
func userKey(username string) []byte {
	return []byte("user:" + username)
}

// CreateUser creates a new user with the given credentials and roles.
// It validates that the username and password are non-empty, checks role validity,
// hashes the password with bcrypt, and stores the user in the `__auth__` CF.
func CreateUser(cfdb scoria.CFDB, username, password string, roles []string) error {
	if username == "" {
		return errors.New("username cannot be empty")
	}
	if password == "" {
		return errors.New("password cannot be empty")
	}

	// Нормализуем username (нижний регистр)
	username = strings.ToLower(strings.TrimSpace(username))

	// Проверяем валидность ролей
	for _, role := range roles {
		if !isValidRole(role) {
			return fmt.Errorf("invalid role: %s", role)
		}
	}

	// Проверяем, не существует ли уже пользователь с таким именем
	existing, err := GetUser(cfdb, username)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return fmt.Errorf("failed to check existing user: %w", err)
	}
	if existing != nil {
		return ErrUserAlreadyExists
	}

	// Хешируем пароль
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Создаём объект пользователя
	user := User{
		Username:     username,
		PasswordHash: string(hash),
		Roles:        roles,
		CreatedAt:    time.Now().Unix(),
	}

	// Сериализуем в JSON
	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}

	// Сохраняем в CF
	err = cfdb.PutCF(AuthCF, userKey(username), data)
	if err != nil {
		return fmt.Errorf("failed to store user: %w", err)
	}

	return nil
}

// Authenticate verifies credentials and returns a JWT token on success.
func Authenticate(cfdb scoria.CFDB, username, password string, jwtSecret []byte) (string, error) {
	username = strings.ToLower(strings.TrimSpace(username))

	user, err := GetUser(cfdb, username)
	if err != nil {
		return "", err
	}

	// Сравниваем пароль
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return "", ErrInvalidCredentials
	}

	// Генерируем JWT
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		Username: user.Username,
		Roles:    user.Roles,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   user.Username,
		},
	})

	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// ValidateToken validates a JWT token and returns its claims.
func ValidateToken(tokenStr string, jwtSecret []byte) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return jwtSecret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token claims")
}

// GetUser returns a user by username.
func GetUser(cfdb scoria.CFDB, username string) (*User, error) {
	username = strings.ToLower(strings.TrimSpace(username))

	data, err := cfdb.GetCF(AuthCF, userKey(username))
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve user: %w", err)
	}
	if len(data) == 0 {
		return nil, ErrUserNotFound
	}

	var user User
	err = json.Unmarshal(data, &user)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}

	return &user, nil
}

// ListUsers returns a list of all registered users.
func ListUsers(cfdb scoria.CFDB) ([]User, error) {
	iter := cfdb.ScanCF(AuthCF, []byte("user:"))
	defer iter.Close()

	var users []User
	count := 0
	for iter.Next() {
		count++
		var user User
		err := json.Unmarshal(iter.Value(), &user)
		if err != nil {
			continue // пропускаем повреждённые записи
		}
		users = append(users, user)
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("scan error: %w", err)
	}
	// Debug: log count (remove later)
	fmt.Printf("[DEBUG] ListUsers scanned %d entries\n", count)

	return users, nil
}

// DeleteUser deletes a user by username.
func DeleteUser(cfdb scoria.CFDB, username string) error {
	username = strings.ToLower(strings.TrimSpace(username))
	return cfdb.DeleteCF(AuthCF, userKey(username))
}

// UpdateUserRoles updates the roles of an existing user.
func UpdateUserRoles(cfdb scoria.CFDB, username string, roles []string) error {
	user, err := GetUser(cfdb, username)
	if err != nil {
		return err
	}

	for _, role := range roles {
		if !isValidRole(role) {
			return fmt.Errorf("invalid role: %s", role)
		}
	}

	user.Roles = roles
	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}

	return cfdb.PutCF(AuthCF, userKey(username), data)
}

// ChangePassword changes the password of an existing user.
func ChangePassword(db scoria.CFDB, username, newPassword string) error {
	if newPassword == "" {
		return ErrInvalidCredentials
	}

	key := []byte("user:" + username)
	val, err := db.GetCF("__auth__", key)
	if err != nil {
		return err
	}
	if len(val) == 0 {
		return ErrUserNotFound
	}

	var user struct {
		PasswordHash string   `json:"password_hash"`
		Roles        []string `json:"roles"`
	}
	if err := json.Unmarshal(val, &user); err != nil {
		return err
	}

	// Генерируем новый хеш
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hashed)

	newVal, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return db.PutCF("__auth__", key, newVal)
}

// isValidRole checks whether the given role is a valid role constant.
func isValidRole(role string) bool {
	switch role {
	case RoleAdmin, RoleReadWrite, RoleReadOnly:
		return true
	default:
		return false
	}
}

// HasAnyRole checks if the user has at least one of the required roles.
func HasAnyRole(user *User, requiredRoles []string) bool {
	for _, userRole := range user.Roles {
		for _, required := range requiredRoles {
			if userRole == required {
				return true
			}
		}
	}
	return false
}

// HasAllRoles checks if the user has all of the required roles.
func HasAllRoles(user *User, requiredRoles []string) bool {
	for _, required := range requiredRoles {
		found := false
		for _, userRole := range user.Roles {
			if userRole == required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// extractBearerToken extracts a Bearer token from the "Authorization" header.
func extractBearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// Authentication errors.
var (
	ErrUserNotFound           = errors.New("user not found")
	ErrUserAlreadyExists      = errors.New("user already exists")
	ErrInvalidCredentials     = errors.New("invalid credentials")
	ErrInsufficientPrivileges = errors.New("insufficient privileges")
)
