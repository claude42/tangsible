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

package variables

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rivo/tview"

	"code.aw.net/claude/tangsible/internal/uikit"
)

// The file shown when Enter is pressed on a definition (design-docs/
// Variables.md, "The file view"): the file as it is on disk, the definition's
// lines marked, scrolled so they are on screen. The pure half; run.go owns the
// widgets.

// maxFileBytes keeps a huge file from freezing the screen; group_vars files
// are small.
const maxFileBytes = 1 << 20

// vaultHeader starts a file ansible-vault encrypted as a whole.
const vaultHeader = "$ANSIBLE_VAULT;"

// renderFile is the text of the file view for the file at path with the
// definition written at line (1-based; 0 when unknown) marked: tview-tagged,
// one text line per file line (the view does not wrap, so a line number is a
// row number). Anything that stops the file being shown is said instead of the
// file: the view never fails.
func renderFile(path string, line int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return notice("Could not read the file", err.Error())
	}
	if len(data) > maxFileBytes {
		return notice("The file is too large to show", fmt.Sprintf("%d bytes; open it with e", len(data)))
	}
	if strings.HasPrefix(string(data), vaultHeader) {
		return notice("This file is encrypted with ansible-vault", "its contents are not shown; open it with e (ansible-vault edit) or tangsible vault")
	}
	if strings.IndexByte(string(data), 0) >= 0 {
		return notice("This file is not text", "")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	shown := make([]string, len(lines))
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yml", ".yaml":
		hl := strings.Split(uikit.HighlightYAMLKeys(strings.Join(lines, "\n")), "\n")
		if len(hl) == len(lines) {
			copy(shown, hl)
			break
		}
		fallthrough
	default:
		for i, l := range lines {
			shown[i] = tview.Escape(l)
		}
	}
	if start, end := definitionSpan(lines, line); start >= 0 {
		for i := start; i <= end; i++ {
			text := lines[i]
			if text == "" {
				text = " "
			}
			shown[i] = styled(true, seg{text, ""})
		}
	}
	return strings.Join(shown, "\n")
}

func notice(title, detail string) string {
	out := "[" + uikit.AsyncErrorColor + "::b]" + tview.Escape(title) + "[-::-]"
	if detail != "" {
		out += "\n\n" + tview.Escape(detail)
	}
	return out
}

// definitionSpan is the first and last line (0-based) of the definition
// written at line: that line, and - for a top-level key - the more indented
// lines (and list items) that belong to its value. (-1, -1) when line is not
// in the file.
func definitionSpan(lines []string, line int) (start, end int) {
	if line < 1 || line > len(lines) {
		return -1, -1
	}
	start = line - 1
	end = start
	if t := lines[start]; t == "" || t[0] == ' ' || t[0] == '\t' {
		return start, end
	}
	for j := start + 1; j < len(lines); j++ {
		l := lines[j]
		if l == "" || l[0] == ' ' || l[0] == '\t' || strings.HasPrefix(l, "- ") {
			if l != "" {
				end = j
			}
			continue
		}
		break
	}
	return start, end
}

// scrollOffset is the first row to show so that the definition starting at
// start (0-based) is on screen in a view height rows tall: from the top of the
// file when it fits there, else with the definition a third of the way down.
func scrollOffset(start, height int) int {
	if start < 0 || start < height-2 {
		return 0
	}
	return max(0, start-max(height/3, 1))
}
