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

package keys

import "testing"

func TestCompareKeys(t *testing.T) {
	tests := []struct {
		name string
		a    []byte
		b    []byte
		want int
	}{
		{"equal", []byte("abc"), []byte("abc"), 0},
		{"a less than b", []byte("abc"), []byte("abd"), -1},
		{"a greater than b", []byte("abd"), []byte("abc"), 1},
		{"a is prefix of b", []byte("ab"), []byte("abc"), -1},
		{"b is prefix of a", []byte("abc"), []byte("ab"), 1},
		{"both empty", []byte{}, []byte{}, 0},
		{"a empty, b non-empty", []byte{}, []byte("a"), -1},
		{"a non-empty, b empty", []byte("a"), []byte{}, 1},
		{"first byte differs", []byte("x"), []byte("y"), -1},
		{"single byte equal", []byte("a"), []byte("a"), 0},
		{"nil vs nil", nil, nil, 0},
		{"nil vs empty", nil, []byte{}, 0},
		{"empty vs nil", []byte{}, nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareKeys(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("CompareKeys(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
