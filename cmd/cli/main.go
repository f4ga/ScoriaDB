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

	"github.com/spf13/cobra"
)

var (
	addr  string
	token string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "scoria",
		Short: "ScoriaDB CLI client",
		Long:  "Command-line interface for ScoriaDB key-value database",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Validate global flags if needed
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&addr, "addr", "localhost:50051", "gRPC server address")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "JWT token for authentication")

	// Add subcommands
	rootCmd.AddCommand(newGetCmd())
	rootCmd.AddCommand(newSetCmd())
	rootCmd.AddCommand(newDelCmd())
	rootCmd.AddCommand(newScanCmd())
	rootCmd.AddCommand(newTxnCmd())
	rootCmd.AddCommand(newAdminCmd())
	rootCmd.AddCommand(newInspectCmd())
	rootCmd.AddCommand(newShellCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}