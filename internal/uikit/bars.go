// Copyright 2026 Klaus Wissmann
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

package uikit

import "strings"

// eighthBlocks[i] is a block i/8 of a cell wide, filled from the left
// (U+258F..U+2588), so a bar can end on a fraction of a cell.
var eighthBlocks = []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉', '█'}

// BarEighths renders fraction (0..1) of width cells as a horizontal bar: full
// blocks plus one partial block, 1/8 cell resolution. A positive fraction
// always shows at least the thinnest block, so a small value never vanishes.
// The result is never wider than width cells and has no trailing padding.
func BarEighths(fraction float64, width int) string {
	if width <= 0 || fraction <= 0 {
		return ""
	}
	if fraction > 1 {
		fraction = 1
	}
	eighths := int(fraction*float64(width)*8 + 0.5)
	if eighths < 1 {
		eighths = 1
	}
	var b strings.Builder
	for i := 0; i < eighths/8; i++ {
		b.WriteRune(eighthBlocks[8])
	}
	if rest := eighths % 8; rest > 0 {
		b.WriteRune(eighthBlocks[rest])
	}
	return b.String()
}
