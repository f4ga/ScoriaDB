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
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"scoriadb/internal/engine"
	"scoriadb/internal/engine/vfs"
)

// inspectCmd is the actual implementation of the inspect command.
func inspectCmd(cmd *cobra.Command, args []string) error {
	dir, _ := cmd.Flags().GetString("dir")
	if dir == "" {
		dir = "./data"
	}

	// Check if directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("directory %s does not exist", dir)
	}

	fmt.Printf("Inspecting database at: %s\n", dir)
	fmt.Println(strings.Repeat("=", 50))

	// Use default VFS
	vfs := vfs.NewDefaultVFS()

	// Open manifest
	manifestPath := filepath.Join(dir, "MANIFEST")
	manifest, err := engine.NewManifest(vfs, manifestPath)
	if err != nil {
		// If manifest doesn't exist, maybe it's a fresh database
		fmt.Printf("MANIFEST not found or error: %v\n", err)
		fmt.Println("Assuming empty database.")
		return nil
	}
	defer manifest.Close()

	// Get levels
	levels := manifest.GetLevels()
	totalFiles := 0
	totalSize := uint64(0)

	fmt.Println("SSTable Levels:")
	for level, infos := range levels {
		if len(infos) == 0 {
			continue
		}
		levelSize := uint64(0)
		for _, info := range infos {
			levelSize += info.Size
		}
		fmt.Printf("  Level %d: %d files, %d bytes\n", level, len(infos), levelSize)
		totalFiles += len(infos)
		totalSize += levelSize
	}
	fmt.Printf("Total SSTables: %d files, %d bytes\n", totalFiles, totalSize)

	// Check VLog
	vlogPath := filepath.Join(dir, "vlog.db")
	if fi, err := os.Stat(vlogPath); err == nil {
		fmt.Printf("VLog: %s, size: %d bytes\n", vlogPath, fi.Size())
	} else {
		fmt.Println("VLog: not found")
	}

	// Check WAL
	walPath := filepath.Join(dir, "wal.log")
	if fi, err := os.Stat(walPath); err == nil {
		fmt.Printf("WAL: %s, size: %d bytes\n", walPath, fi.Size())
	} else {
		fmt.Println("WAL: not found")
	}

	// List all files in directory
	fmt.Println("\nAll files in directory:")
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}
	for _, f := range files {
		info, err := f.Info()
		if err != nil {
			continue
		}
		fmt.Printf("  %s %d bytes\n", f.Name(), info.Size())
	}

	return nil
}

// newInspectCmd creates the `inspect` command with proper implementation.
func newInspectCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect database files offline",
		RunE:  inspectCmd,
	}
	cmd.Flags().StringVar(&dir, "dir", "./data", "database directory")
	return cmd
}