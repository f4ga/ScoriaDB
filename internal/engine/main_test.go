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

package engine

import (
	"os"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/logger"
)

// TestMain sets log level to ERROR for all tests and benchmarks in this package.
// This prevents flush/compaction/startup logs from cluttering terminal output.
func TestMain(m *testing.M) {
	logger.SetLevel(logger.ERROR)
	os.Exit(m.Run())
}
