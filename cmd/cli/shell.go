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
	"github.com/spf13/cobra"
)

// shellCmd implements an interactive shell.
func shellCmd(cmd *cobra.Command, args []string) error {
	fmt.Println("ScoriaDB Interactive Shell")
	fmt.Println("Type 'help' for commands, 'exit' to quit.")
	fmt.Printf("Connected to %s\n", addr)

	client, err := NewClient(addr, token)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	// Create shell state
	state := &shellState{
		client: client,
	}

	// Run prompt loop
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

// shellState holds the state of the shell.
type shellState struct {
	client *Client
}

// executor executes a command line.
func (s *shellState) executor(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if line == "exit" {
		fmt.Println("Bye")
		os.Exit(0)
		return

	}
	if line == "help" {
		s.help()
		return
	}

	// Parse simple commands
	parts := strings.Fields(line)
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "get":
		if len(args) < 1 {
			fmt.Println("Usage: get <key>")
			return
		}
		s.get(args[0])
	case "set":
		if len(args) < 2 {
			fmt.Println("Usage: set <key> <value>")
			return
		}
		s.set(args[0], args[1])
	case "del":
		if len(args) < 1 {
			fmt.Println("Usage: del <key>")
			return
		}
		s.del(args[0])
	case "scan":
		prefix := ""
		if len(args) > 0 {
			prefix = args[0]
		}
		s.scan(prefix)
	default:
		fmt.Printf("Unknown command: %s. Type 'help' for list.\n", cmd)
	}
}

// completer provides suggestions for tab completion.
func (s *shellState) completer(d prompt.Document) []prompt.Suggest {
	text := d.TextBeforeCursor()
	if strings.TrimSpace(text) == "" {
		return []prompt.Suggest{}
	}

	// Basic command completions
	commands := []prompt.Suggest{
		{Text: "get", Description: "Get value for key"},
		{Text: "set", Description: "Set key-value pair"},
		{Text: "del", Description: "Delete key"},
		{Text: "scan", Description: "Scan keys with prefix"},
		{Text: "help", Description: "Show help"},
		{Text: "exit", Description: "Exit shell"},
		{Text: "quit", Description: "Exit shell"},
	}

	// TODO: Add key completion by scanning prefix
	return prompt.FilterHasPrefix(commands, d.GetWordBeforeCursor(), true)
}

// help prints help message.
func (s *shellState) help() {
	fmt.Println("Available commands:")
	fmt.Println("  get <key>          - Get value for key")
	fmt.Println("  set <key> <value>  - Set key-value pair")
	fmt.Println("  del <key>          - Delete key")
	fmt.Println("  scan [prefix]      - Scan keys with optional prefix")
	fmt.Println("  help               - Show this help")
	fmt.Println("  exit, quit         - Exit shell")
}

// get executes get command.
func (s *shellState) get(key string) {
	ctx, cancel := defaultContext()
	defer cancel()

	resp, err := s.client.Get(ctx, []byte(key), "default")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if resp.Found {
		fmt.Printf("%s\n", resp.Value)
	} else {
		fmt.Println("(not found)")
	}
}

// set executes set command.
func (s *shellState) set(key, value string) {
	ctx, cancel := defaultContext()
	defer cancel()

	_, err := s.client.Put(ctx, []byte(key), []byte(value), "default")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println("OK")
}

// del executes delete command.
func (s *shellState) del(key string) {
	ctx, cancel := defaultContext()
	defer cancel()

	_, err := s.client.Delete(ctx, []byte(key), "default")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println("OK")
}

// scan executes scan command.
func (s *shellState) scan(prefix string) {
	ctx, cancel := defaultContext()
	defer cancel()

	results, err := s.client.Scan(ctx, []byte(prefix), "default")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	for _, resp := range results {
		fmt.Printf("%s\t%s\n", resp.Key, resp.Value)
	}
	fmt.Printf("Total: %d keys\n", len(results))
}

// newShellCmd creates the `shell` command.
func newShellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell",
		Short: "Start interactive shell",
		RunE:  shellCmd,
	}
}
