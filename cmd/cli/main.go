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