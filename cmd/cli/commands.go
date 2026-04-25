package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// newGetCmd creates the `get` command.
func newGetCmd() *cobra.Command {
	var cfName string
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get value for a key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			client, err := NewClient(addr, token)
			if err != nil {
				return err
			}
			defer client.Close()

			ctx, cancel := defaultContext()
			defer cancel()

			resp, err := client.Get(ctx, []byte(key), cfName)
			if err != nil {
				return fmt.Errorf("get failed: %w", err)
			}

			if resp.Found {
				fmt.Printf("%s\n", resp.Value)
			} else {
				fmt.Println("(not found)")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cfName, "cf", "default", "column family name")
	return cmd
}

// newSetCmd creates the `set` command.
func newSetCmd() *cobra.Command {
	var cfName string
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set value for a key",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]
			client, err := NewClient(addr, token)
			if err != nil {
				return err
			}
			defer client.Close()

			ctx, cancel := defaultContext()
			defer cancel()

			_, err = client.Put(ctx, []byte(key), []byte(value), cfName)
			if err != nil {
				return fmt.Errorf("set failed: %w", err)
			}
			fmt.Println("OK")
			return nil
		},
	}
	cmd.Flags().StringVar(&cfName, "cf", "default", "column family name")
	return cmd
}

// newDelCmd creates the `del` command.
func newDelCmd() *cobra.Command {
	var cfName string
	cmd := &cobra.Command{
		Use:   "del <key>",
		Short: "Delete a key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			client, err := NewClient(addr, token)
			if err != nil {
				return err
			}
			defer client.Close()

			ctx, cancel := defaultContext()
			defer cancel()

			_, err = client.Delete(ctx, []byte(key), cfName)
			if err != nil {
				return fmt.Errorf("delete failed: %w", err)
			}
			fmt.Println("OK")
			return nil
		},
	}
	cmd.Flags().StringVar(&cfName, "cf", "default", "column family name")
	return cmd
}

// newScanCmd creates the `scan` command.
func newScanCmd() *cobra.Command {
	var cfName string
	cmd := &cobra.Command{
		Use:   "scan [prefix]",
		Short: "Scan keys with prefix",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var prefix []byte
			if len(args) > 0 {
				prefix = []byte(args[0])
			}
			client, err := NewClient(addr, token)
			if err != nil {
				return err
			}
			defer client.Close()

			ctx, cancel := defaultContext()
			defer cancel()

			results, err := client.Scan(ctx, prefix, cfName)
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			for _, resp := range results {
				fmt.Printf("%s\t%s\n", resp.Key, resp.Value)
			}
			fmt.Printf("Total: %d keys\n", len(results))
			return nil
		},
	}
	cmd.Flags().StringVar(&cfName, "cf", "default", "column family name")
	return cmd
}

// newTxnCmd creates the `txn` command group.
func newTxnCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "txn",
		Short: "Transaction operations",
	}
	cmd.AddCommand(newTxnBeginCmd())
	cmd.AddCommand(newTxnCommitCmd())
	cmd.AddCommand(newTxnRollbackCmd())
	return cmd
}

// newTxnBeginCmd creates the `txn begin` command.
func newTxnBeginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "begin",
		Short: "Begin a new transaction",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := NewClient(addr, token)
			if err != nil {
				return err
			}
			defer client.Close()

			ctx, cancel := defaultContext()
			defer cancel()

			resp, err := client.BeginTxn(ctx)
			if err != nil {
				return fmt.Errorf("begin transaction failed: %w", err)
			}
			fmt.Printf("Transaction ID: %s\n", resp.TxnId)
			fmt.Printf("Start timestamp: %d\n", resp.StartTs)
			return nil
		},
	}
}

// newTxnCommitCmd creates the `txn commit` command.
func newTxnCommitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commit <txn_id>",
		Short: "Commit a transaction",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// For simplicity, we don't support interactive ops in MVP.
			// User can provide ops via flags or file in future.
			fmt.Fprintln(os.Stderr, "Note: MVP does not support interactive transaction operations yet.")
			fmt.Fprintln(os.Stderr, "Use gRPC directly for now.")
			return nil
		},
	}
}

// newTxnRollbackCmd creates the `txn rollback` command.
func newTxnRollbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback <txn_id>",
		Short: "Rollback a transaction",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			txnID := args[0]
			client, err := NewClient(addr, token)
			if err != nil {
				return err
			}
			defer client.Close()

			ctx, cancel := defaultContext()
			defer cancel()

			_, err = client.RollbackTxn(ctx, txnID)
			if err != nil {
				return fmt.Errorf("rollback failed: %w", err)
			}
			fmt.Println("OK")
			return nil
		},
	}
}

// newAdminCmd creates the `admin` command group.
func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administrative operations",
	}
	cmd.AddCommand(newAdminUserAddCmd())
	cmd.AddCommand(newAdminAuthCmd())
	return cmd
}

// newAdminUserAddCmd creates the `admin user add` command.
func newAdminUserAddCmd() *cobra.Command {
	var roles string
	cmd := &cobra.Command{
		Use:   "user-add <username> <password>",
		Short: "Create a new user",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			username, password := args[0], args[1]
			var roleList []string
			if roles != "" {
				roleList = strings.Split(roles, ",")
			} else {
				roleList = []string{"readwrite"}
			}
			client, err := NewClient(addr, token)
			if err != nil {
				return err
			}
			defer client.Close()

			ctx, cancel := defaultContext()
			defer cancel()

			_, err = client.CreateUser(ctx, username, password, roleList)
			if err != nil {
				return fmt.Errorf("create user failed: %w", err)
			}
			fmt.Println("User created")
			return nil
		},
	}
	cmd.Flags().StringVar(&roles, "roles", "", "comma-separated roles (admin,readwrite,readonly)")
	return cmd
}

// newAdminAuthCmd creates the `admin auth` command.
func newAdminAuthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "auth <username> <password>",
		Short: "Authenticate and get JWT token",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			username, password := args[0], args[1]
			client, err := NewClient(addr, "")
			if err != nil {
				return err
			}
			defer client.Close()

			ctx, cancel := defaultContext()
			defer cancel()

			resp, err := client.Authenticate(ctx, username, password)
			if err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
			fmt.Println(resp.JwtToken)
			return nil
		},
	}
}
