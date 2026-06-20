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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// whoami shows current user info from JWT.
func (s *shellState) whoami() {
	if token == "" {
		fmt.Println("Not authenticated")
		return
	}
	username, roles, err := parseJWT(token)
	if err != nil {
		fmt.Printf("Error parsing token: %v\n", err)
		return
	}
	fmt.Printf("Username: %s\n", username)
	fmt.Printf("Roles: %s\n", roles)
}

// parseJWT decodes the JWT token and returns username and roles.
func parseJWT(tokenStr string) (string, string, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("invalid JWT format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", err
	}
	var claims struct {
		Subject string   `json:"sub"`
		Roles   []string `json:"roles"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", err
	}
	return claims.Subject, strings.Join(claims.Roles, ", "), nil
}

// lastErrorCmd shows last error.
func (s *shellState) lastErrorCmd() {
	if s.lastError == nil {
		fmt.Println("No error")
	} else {
		fmt.Printf("Last error: %v\n", s.lastError)
	}
}

// history shows command history.
func (s *shellState) history() {
	if len(s.cmdHistory) == 0 {
		fmt.Println("No commands in history")
		return
	}
	for i, cmd := range s.cmdHistory {
		fmt.Printf("%3d  %s\n", i+1, cmd)
	}
}
