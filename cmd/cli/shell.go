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
	"strings"

	"github.com/c-bata/go-prompt"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/spf13/cobra"
)

// shellState holds the state of the shell.
type shellState struct {
	client     *Client
	currentCF  string
	lastError  error
	cmdHistory []string
}

// shellCmd implements an interactive shell.
func shellCmd(cmd *cobra.Command, args []string) error {
	fmt.Println("ScoriaDB Interactive Shell")
	fmt.Println("Type 'help' for commands, 'exit' to quit.")
	fmt.Printf("Connected to %s\n", addr)

	client, err := NewClient(addr, token)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer errors.CloseWithLog(client, "gRPC client")

	state := &shellState{
		client:    client,
		currentCF: "default",
		lastError: nil,
	}

	p := prompt.New(
		state.executor,
		state.completer,
		prompt.OptionTitle("scoria-shell"),
		prompt.OptionPrefix("scoria> "),
		prompt.OptionPrefixTextColor(prompt.Yellow),
	)
	p.Run()
	return nil
}

// executor executes a command line.
func (s *shellState) executor(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	s.cmdHistory = append(s.cmdHistory, line)
	if len(s.cmdHistory) > 100 {
		s.cmdHistory = s.cmdHistory[1:]
	}

	parts := strings.Fields(line)
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "exit", "quit":
		fmt.Println("Goodbye!")
		os.Exit(0)

	case "help":
		s.help()

	case "clear":
		s.clearScreen()

	case "get":
		s.handleGet(args)
	case "set":
		s.handleSet(args)
	case "del":
		s.handleDel(args)
	case "scan":
		s.handleScan(args)

	case "cf":
		s.showCF()
	case "use":
		s.handleUse(args)
	case "list-cf":
		s.listCF()
	case "create-cf":
		s.handleCreateCF(args)
	case "delete-cf":
		s.handleDeleteCF(args)

	case "whoami":
		s.whoami()
	case "stats":
		s.stats()
	case "export":
		s.handleExport(args)

	case "history":
		s.history()
	case "last-error":
		s.lastErrorCmd()

	default:
		if cmd == "admin" {
			s.handleAdmin(args)
			return
		}
		fmt.Printf("Unknown command: %s. Type 'help' for list.\n", cmd)
	}
}

// completer provides suggestions for tab completion.
func (s *shellState) completer(d prompt.Document) []prompt.Suggest {
	text := d.TextBeforeCursor()
	words := strings.Fields(text)

	commands := []prompt.Suggest{
		{Text: "get", Description: "Get value for key"},
		{Text: "set", Description: "Set key-value pair"},
		{Text: "del", Description: "Delete key"},
		{Text: "scan", Description: "Scan keys with prefix"},
		{Text: "use", Description: "Set default column family"},
		{Text: "cf", Description: "Show current column family"},
		{Text: "list-cf", Description: "List all column families"},
		{Text: "create-cf", Description: "Create a new column family"},
		{Text: "delete-cf", Description: "Delete a column family"},
		{Text: "whoami", Description: "Show current user info"},
		{Text: "stats", Description: "Show database statistics"},
		{Text: "export", Description: "Export scan results to file"},
		{Text: "clear", Description: "Clear screen"},
		{Text: "history", Description: "Show command history"},
		{Text: "last-error", Description: "Show last error"},
		{Text: "admin", Description: "Administrative commands"},
		{Text: "help", Description: "Show help"},
		{Text: "exit", Description: "Exit shell"},
		{Text: "quit", Description: "Exit shell"},
	}

	adminCommands := []prompt.Suggest{
		{Text: "change-password", Description: "Change user password"},
		{Text: "user-add", Description: "Create new user"},
		{Text: "list-users", Description: "List all users"},
	}

	if len(words) > 0 && words[0] == "admin" {
		if len(words) == 1 {
			return prompt.FilterHasPrefix(adminCommands, d.GetWordBeforeCursor(), true)
		}
	}
	return prompt.FilterHasPrefix(commands, d.GetWordBeforeCursor(), true)
}

// newShellCmd creates the `shell` command.
func newShellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell",
		Short: "Start interactive shell",
		RunE:  shellCmd,
	}
}
