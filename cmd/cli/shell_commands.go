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
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/f4ga/ScoriaDB/internal/errors"
)

// help prints help message.
func (s *shellState) help() {
	fmt.Println("Available commands:")
	fmt.Println("  get <key>                 - Get value for key")
	fmt.Println("  set <key> <value>         - Set key-value pair")
	fmt.Println("  del <key>                 - Delete key")
	fmt.Println("  scan [prefix]             - Scan keys with prefix")
	fmt.Println("  use <cf>                  - Set default column family")
	fmt.Println("  cf                        - Show current column family")
	fmt.Println("  list-cf                   - List all column families")
	fmt.Println("  create-cf <name>          - Create a new column family")
	fmt.Println("  delete-cf <name>          - Delete a column family (cannot delete system CFs)")
	fmt.Println("  whoami                    - Show current user info")
	fmt.Println("  stats                     - Show database statistics")
	fmt.Println("  export <prefix> <file>    - Export scan results to file")
	fmt.Println("  clear                     - Clear screen")
	fmt.Println("  history                   - Show command history")
	fmt.Println("  last-error                - Show last error")
	fmt.Println("")
	fmt.Println("Admin commands:")
	fmt.Println("  admin change-password <user> <pass>  - Change user password")
	fmt.Println("  admin user-add <user> <pass> [--roles=...] - Create new user")
	fmt.Println("  admin list-users          - List all users")
	fmt.Println("")
	fmt.Println("  help                      - Show this help")
	fmt.Println("  exit, quit                - Exit shell")
}

// clearScreen clears the terminal screen.
func (s *shellState) clearScreen() {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "cls")
	} else {
		cmd = exec.Command("clear")
	}
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		s.lastError = err
		fmt.Printf("Error clearing screen: %v\n", err)
	}
}

// handleGet executes get command.
func (s *shellState) handleGet(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: get <key>")
		return
	}
	ctx, cancel := defaultContext()
	defer cancel()

	resp, err := s.client.Get(ctx, []byte(args[0]), s.currentCF)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	if resp.Found {
		fmt.Printf("%s\n", resp.Value)
	} else {
		fmt.Println("(not found)")
	}
}

// handleSet executes set command.
func (s *shellState) handleSet(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: set <key> <value>")
		return
	}
	ctx, cancel := defaultContext()
	defer cancel()

	_, err := s.client.Put(ctx, []byte(args[0]), []byte(strings.Join(args[1:], " ")), s.currentCF)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println("OK")
}

// handleDel executes delete command.
func (s *shellState) handleDel(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: del <key>")
		return
	}
	ctx, cancel := defaultContext()
	defer cancel()

	_, err := s.client.Delete(ctx, []byte(args[0]), s.currentCF)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println("OK")
}

// handleScan executes scan command.
func (s *shellState) handleScan(args []string) {
	prefix := ""
	if len(args) > 0 {
		prefix = args[0]
	}
	ctx, cancel := defaultContext()
	defer cancel()

	results, err := s.client.Scan(ctx, []byte(prefix), s.currentCF)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	for _, resp := range results {
		fmt.Printf("%s\t%s\n", resp.Key, resp.Value)
	}
	fmt.Printf("Total: %d keys\n", len(results))
}

// showCF shows current column family.
func (s *shellState) showCF() {
	fmt.Printf("Current column family: %s\n", s.currentCF)
}

// handleUse sets default column family.
func (s *shellState) handleUse(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: use <column-family>")
		return
	}
	cfName := args[0]
	ctx, cancel := defaultContext()
	defer cancel()

	_, err := s.client.Scan(ctx, []byte(""), cfName)
	if err != nil && strings.Contains(err.Error(), "CF") {
		fmt.Printf("Column family '%s' does not exist. Use 'list-cf' to see available.\n", cfName)
		return
	}
	s.currentCF = cfName
	fmt.Printf("Switched to column family: %s\n", cfName)
}

// listCF lists all column families.
func (s *shellState) listCF() {
	ctx, cancel := defaultContext()
	defer cancel()

	cfs, err := s.client.ListCF(ctx)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	for _, cf := range cfs {
		fmt.Println(cf)
	}
}

// handleCreateCF creates a new column family.
func (s *shellState) handleCreateCF(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: create-cf <cf-name>")
		return
	}
	ctx, cancel := defaultContext()
	defer cancel()

	err := s.client.CreateCF(ctx, args[0])
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Column family '%s' created\n", args[0])
}

// handleDeleteCF deletes a column family.
func (s *shellState) handleDeleteCF(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: delete-cf <cf-name>")
		return
	}
	ctx, cancel := defaultContext()
	defer cancel()

	err := s.client.DeleteCF(ctx, args[0])
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	if s.currentCF == args[0] {
		s.currentCF = "default"
		fmt.Printf("Current CF was '%s', reset to 'default'\n", args[0])
	}
	fmt.Printf("Column family '%s' deleted\n", args[0])
}

// stats shows key count in current CF.
func (s *shellState) stats() {
	ctx, cancel := defaultContext()
	defer cancel()

	results, err := s.client.Scan(ctx, []byte(""), s.currentCF)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Current CF '%s': %d keys\n", s.currentCF, len(results))
}

// handleExport exports scan results to file.
func (s *shellState) handleExport(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: export <key-prefix> <filename>")
		fmt.Println("Example: export user: ./users.txt")
		return
	}
	prefix, filename := args[0], args[1]
	ctx, cancel := defaultContext()
	defer cancel()

	results, err := s.client.Scan(ctx, []byte(prefix), s.currentCF)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error scanning: %v\n", err)
		return
	}
	file, err := os.Create(filename)
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		return
	}
	defer errors.CloseWithLog(file, "export file")

	for _, resp := range results {
		line := fmt.Sprintf("%s\t%s\n", resp.Key, resp.Value)
		if _, err := file.WriteString(line); err != nil {
			fmt.Printf("Error writing to file: %v\n", err)
			return
		}
	}
	fmt.Printf("Exported %d keys to %s\n", len(results), filename)
}
