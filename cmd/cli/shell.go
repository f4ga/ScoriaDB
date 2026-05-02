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
	"encoding/base64"
    "encoding/json"
	"github.com/c-bata/go-prompt"
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
	defer client.Close()

	// Create shell state
	state := &shellState{
		client:    client,
		currentCF: "default",
		lastError: nil,
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

// executor executes a command line.
func (s *shellState) executor(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	// Сохраняем в историю
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
		s.set(args[0], strings.Join(args[1:], " "))

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

	case "cf":
		s.showCF()

	case "use":
		if len(args) < 1 {
			fmt.Println("Usage: use <column-family>")
			return
		}
		s.useCF(args[0])

	case "list-cf":
		s.listCF()
	
	case "create-cf":
		if len(args) < 1 {
			fmt.Println("Usage: create-cf <column-family>")
			return
		}
		s.createCF(args[0])

	case "delete-cf":
    if len(args) < 1 {
        fmt.Println("Usage: delete-cf <cf-name>")
        return
    }
    s.deleteCF(args[0])

	case "whoami":
		s.whoami()

	case "stats":
		s.stats()

	case "last-error":
		s.lastErrorCmd()

	case "history":
		s.history()

	case "export":
		if len(args) < 2 {
			fmt.Println("Usage: export <key-prefix> <filename>")
			fmt.Println("Example: export user: ./users.txt")
			return
		}
		s.export(args[0], args[1])

	default:
		// Попробуем обработать admin команды
		if cmd == "admin" {
			s.handleAdmin(args)
			return
		}
		fmt.Printf("Unknown command: %s. Type 'help' for list.\n", cmd)
	}
}

// handleAdmin обрабатывает admin подкоманды
func (s *shellState) handleAdmin(args []string) {
	if len(args) == 0 {
		fmt.Println("Admin subcommands: change-password, user-add, list-users")
		return
	}

	subCmd := args[0]
	switch subCmd {
	case "change-password":
		if len(args) != 3 {
			fmt.Println("Usage: admin change-password <username> <new-password>")
			return
		}
		s.changePassword(args[1], args[2])

	case "user-add":
		var roleList []string
		if len(args) < 3 {
			fmt.Println("Usage: admin user-add <username> <password> [--roles=admin,readwrite]")
			return
		}
		username := args[1]
		password := args[2]
		roleList = []string{"readwrite"}
		for i := 3; i < len(args); i++ {
			if strings.HasPrefix(args[i], "--roles=") {
				roleList = strings.Split(strings.TrimPrefix(args[i], "--roles="), ",")
				break
			}
		}
		s.userAdd(username, password, roleList)

	case "create-cf":
    if len(args) != 2 {
        fmt.Println("Usage: admin create-cf <cf-name>")
        return
    }
    s.createCF(args[1])

	case "list-users":
		s.listUsers()

	default:
		fmt.Printf("Unknown admin command: %s\n", subCmd)
		fmt.Println("Available: change-password, user-add, list-users")
	}
}

// completer provides suggestions for tab completion.
func (s *shellState) completer(d prompt.Document) []prompt.Suggest {
	text := d.TextBeforeCursor()
	words := strings.Fields(text)

	// Базовые команды
	commands := []prompt.Suggest{
		{Text: "get", Description: "Get value for key"},
		{Text: "set", Description: "Set key-value pair"},
		{Text: "del", Description: "Delete key"},
		{Text: "scan", Description: "Scan keys with prefix"},
		{Text: "use", Description: "Set default column family"},
		{Text: "cf", Description: "Show current column family"},
		{Text: "list-cf", Description: "List all column families"},
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

	// Админские подкоманды
	adminCommands := []prompt.Suggest{
		{Text: "change-password", Description: "Change user password"},
		{Text: "user-add", Description: "Create new user"},
		{Text: "list-users", Description: "List all users"},
	}

	// Если набираем admin, предлагаем подкоманды
	if len(words) > 0 && words[0] == "admin" {
		if len(words) == 1 {
			return prompt.FilterHasPrefix(adminCommands, d.GetWordBeforeCursor(), true)
		}
	}

	return prompt.FilterHasPrefix(commands, d.GetWordBeforeCursor(), true)
}

// help prints help message.
func (s *shellState) help() {func (s *shellState) help() {
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
// clearScreen clears the screen.
func (s *shellState) clearScreen() {
    var cmd *exec.Cmd
    if runtime.GOOS == "windows" {
        cmd = exec.Command("cmd", "/c", "cls")
    } else {
        cmd = exec.Command("clear")
    }
    cmd.Stdout = os.Stdout
    if err := cmd.Run(); err != nil {
        fmt.Printf("Error: %v\n", err)	
    }
}
// get executes get command.
func (s *shellState) get(key string) {
	ctx, cancel := defaultContext()
	defer cancel()

	resp, err := s.client.Get(ctx, []byte(key), s.currentCF)
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

// set executes set command.
func (s *shellState) set(key, value string) {
	ctx, cancel := defaultContext()
	defer cancel()

	_, err := s.client.Put(ctx, []byte(key), []byte(value), s.currentCF)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println("OK")
}

// del executes delete command.
func (s *shellState) del(key string) {
	ctx, cancel := defaultContext()
	defer cancel()

	_, err := s.client.Delete(ctx, []byte(key), s.currentCF)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Println("OK")
}

// scan executes scan command.
func (s *shellState) scan(prefix string) {
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

func (s *shellState) createCF(cfName string) {
    ctx, cancel := defaultContext()
    defer cancel()

    err := s.client.CreateCF(ctx, cfName)
    if err != nil {
        s.lastError = err
        fmt.Printf("Error: %v\n", err)
        return
    }
    fmt.Printf("Column family '%s' created\n", cfName)
}
// useCF sets default column family.
func (s *shellState) useCF(cfName string) {
	ctx, cancel := defaultContext()
	defer cancel()

	// Проверяем, существует ли CF
	_, err := s.client.Scan(ctx, []byte(""), cfName)
	if err != nil && strings.Contains(err.Error(), "CF") {
		fmt.Printf("Column family '%s' does not exist. Use 'list-cf' to see available.\n", cfName)
		return
	}
	s.currentCF = cfName
	fmt.Printf("Switched to column family: %s\n", cfName)
}

// listCF lists all column families.func (s *shellState) listCF() {
    ctx, cancel := defaultContext()
    defer cancel()

    cfs, err := s.client.ListCF(ctx)
    if err != nil {
        s.lastError = err
        fmt.Printf("Error: %v\n", err)
        return
    }

    if len(cfs) == 0 {
        fmt.Println("No column families")
        return
    }

    for _, cf := range cfs {
        fmt.Println(cf)
    }
}

func (s *shellState) deleteCF(cfName string) {
    ctx, cancel := defaultContext()
    defer cancel()

    err := s.client.DeleteCF(ctx, cfName)
    if err != nil {
        s.lastError = err
        fmt.Printf("Error: %v\n", err)
        return
    }

    // If we deleted the current CF, reset to default
    if s.currentCF == cfName {
        s.currentCF = "default"
        fmt.Printf("Current CF was '%s', reset to 'default'\n", cfName)
    }

    fmt.Printf("Column family '%s' deleted\n", cfName)
}

// whoami shows current user.
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
    fmt.Printf("Roles: %s\n", strings.Join(roles, ", "))
}

func parseJWT(token string) (username string, roles []string, err error) {
    parts := strings.Split(token, ".")
    if len(parts) != 3 {
        return "", nil, fmt.Errorf("invalid JWT format")
    }

    // Декодируем payload (вторая часть)
    payload, err := base64.RawURLEncoding.DecodeString(parts[1])
    if err != nil {
        return "", nil, err
    }

    var claims struct {
        Subject string   `json:"sub"`
        Roles   []string `json:"roles"`
    }
    if err := json.Unmarshal(payload, &claims); err != nil {
        return "", nil, err
    }

    return claims.Subject, claims.Roles, nil
}


// stats shows database statistics.
func (s *shellState) stats() {
	// Простое сканирование для подсчёта ключей
	ctx, cancel := defaultContext()
	defer cancel()

	results, err := s.client.Scan(ctx, []byte(""), s.currentCF)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error getting stats: %v\n", err)
		return
	}
	fmt.Printf("Current CF '%s': %d keys\n", s.currentCF, len(results))
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

// export exports scan results to file.
func (s *shellState) export(prefix, filename string) {
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
	defer file.Close()

	for _, resp := range results {
		line := fmt.Sprintf("%s\t%s\n", resp.Key, resp.Value)
		if _, err := file.WriteString(line); err != nil {
			fmt.Printf("Error writing to file: %v\n", err)
			return
		}
	}
	fmt.Printf("Exported %d keys to %s\n", len(results), filename)
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

// userAdd adds a new user.
func (s *shellState) userAdd(username, password string, roleList []string) {
	ctx, cancel := defaultContext()
	defer cancel()

	_, err := s.client.CreateUser(ctx, username, password, roleList)
	if err != nil {
		s.lastError = err
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("User '%s' created with roles: %v\n", username, roleList)
}

// listUsers lists all users.
func (s *shellState) listUsers() {
	// TODO: добавить метод ListUsers в gRPC
	fmt.Println("User listing not yet implemented in gRPC")
	fmt.Println("Use embedded API or direct auth package")
}

// newShellCmd creates the `shell` command.
func newShellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell",
		Short: "Start interactive shell",
		RunE:  shellCmd,
	}
}
