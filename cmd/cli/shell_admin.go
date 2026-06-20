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
	"fmt"
	"strings"
)

// handleAdmin processes admin subcommands
func (s *shellState) handleAdmin(args []string) {
	if len(args) == 0 {
		fmt.Println("Admin subcommands: change-password, user-add, list-users")
		return
	}

	switch args[0] {
	case "change-password":
		if len(args) != 3 {
			fmt.Println("Usage: admin change-password <username> <new-password>")
			return
		}
		s.changePassword(args[1], args[2])

	case "user-add":
		if len(args) < 3 {
			fmt.Println("Usage: admin user-add <username> <password> [--roles=admin,readwrite]")
			return
		}
		username, password := args[1], args[2]
		roles := []string{"readwrite"}
		for i := 3; i < len(args); i++ {
			if strings.HasPrefix(args[i], "--roles=") {
				roles = strings.Split(strings.TrimPrefix(args[i], "--roles="), ",")
				break
			}
		}
		s.userAdd(username, password, roles)

	case "list-users":
		s.listUsers()

	default:
		fmt.Printf("Unknown admin command: %s\n", args[0])
		fmt.Println("Available: change-password, user-add, list-users")
	}
}

// changePassword changes user password.
func (s *shellState) changePassword(username, newPassword string) {
	ctx, cancel := defaultContext()
	defer cancel()

	err := s.client.ChangePassword(ctx, username, newPassword)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Password changed for user '%s'\n", username)
}

// userAdd creates a new user.
func (s *shellState) userAdd(username, password string, roles []string) {
	ctx, cancel := defaultContext()
	defer cancel()

	_, err := s.client.CreateUser(ctx, username, password, roles)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("User '%s' created with roles: %v\n", username, roles)
}

// listUsers lists all users.
func (s *shellState) listUsers() {
	ctx, cancel := defaultContext()
	defer cancel()

	users, err := s.client.ListUsers(ctx)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	for _, u := range users {
		fmt.Printf("%s %v\n", u.Username, u.Roles)
	}
}
