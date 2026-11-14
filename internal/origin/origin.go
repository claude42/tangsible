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

// Package origin turns where a top-level variable's value comes from into a
// display label (design-docs/Data Tagging.md, "Show where a variable
// originated"): for the debug watch panel and the host verb's Variables tab.
// The child-side half is callback/tangsible_events.py.
package origin

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Origin is one entry of tangsible_snapshot's "origins": a file (and
// line) the value was written in, a description (a command-line option),
// or just a coarse kind for what carries no origin tag of its own.
type Origin struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Desc string `json:"desc"`
	Kind string `json:"kind"` // "fact", "builtin", "registered"
}

// Labels turns raw origins into display labels ("vars/main.yml:3",
// "fact", "-e", ...). copyDir/playbookDir map the debug shadow copy back to
// the real tree; base is what paths are shown relative to (the project, i.e.
// the working directory).
func Labels(origins map[string]Origin, copyDir, playbookDir, base string) map[string]string {
	labels := make(map[string]string, len(origins))
	for name, o := range origins {
		if l := Label(o, copyDir, playbookDir, base); l != "" {
			labels[name] = l
		}
	}
	return labels
}

func Label(o Origin, copyDir, playbookDir, base string) string {
	switch {
	case o.Path != "":
		path, line := o.Path, o.Line
		if copyDir != "" && playbookDir != "" && strings.HasPrefix(path, copyDir+string(filepath.Separator)) {
			line = UnspliceLine(path, line)
			path = playbookDir + strings.TrimPrefix(path, copyDir)
		}
		if rel, err := filepath.Rel(base, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
		if line > 0 {
			return path + ":" + strconv.Itoa(line)
		}
		return path
	case o.Desc != "":
		if strings.Contains(o.Desc, "'-e'") || strings.Contains(o.Desc, "--extra-vars") {
			return "-e"
		}
		return strings.Trim(o.Desc, "<>")
	}
	switch o.Kind {
	case "fact":
		return "fact"
	case "builtin":
		return "built-in"
	case "registered":
		return "registered"
	}
	return ""
}

// UnspliceLine maps a line of a file in the debug shadow copy back to the
// original file: debugcopy inserts one "debugger: always" line per task, so
// the original line is the copy's minus the inserted lines above it. A file
// that can't be read keeps the copy's own line.
func UnspliceLine(copyPath string, line int) int {
	if line <= 0 {
		return line
	}
	data, err := os.ReadFile(copyPath)
	if err != nil {
		return line
	}
	lines := strings.Split(string(data), "\n")
	if line > len(lines) {
		line = len(lines)
	}
	shift := 0
	for _, l := range lines[:line-1] {
		if strings.TrimSpace(l) == "debugger: always" {
			shift++
		}
	}
	return line - shift
}
