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

package debugcopy

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"code.aw.net/claude/tangsible/internal/source"
	"gopkg.in/yaml.v3"
)

var strategyLine = regexp.MustCompile(`^(\s*(?:-\s+)?strategy\s*:\s*).*$`)

// ForceLinearStrategy rewrites every play-level "strategy:" other than
// "linear" in the shadow copy's own playbook files to "linear" -
// design-docs/DebugMode.md's "strategy: free": debug's tree is seeded from
// a --list-tasks skeleton and resolved by position, which only a lockstep
// strategy keeps meaningful (and F10's "next task" only has one answer
// under). The value is replaced in place on its own line, so no line
// number shifts (BuildPathMapping stays valid). Files examined are the
// playbook itself, every file in locations, and any playbook those pull in
// via a literal import_playbook; only playbook-shaped files are touched.
// Returns whether anything was rewritten. Best-effort per file.
func ForceLinearStrategy(playbookDir, copyDir, playbookPath string, locations []source.TaskLocation) bool {
	seen := map[string]bool{}
	var queue []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			queue = append(queue, p)
		}
	}
	add(playbookPath)
	for _, l := range locations {
		add(l.Path)
	}
	changed := false
	for i := 0; i < len(queue); i++ {
		orig := queue[i]
		rel, err := filepath.Rel(playbookDir, orig)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		path := filepath.Join(copyDir, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc yaml.Node
		if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.SequenceNode {
			continue
		}
		var lineNos []int
		for _, play := range doc.Content[0].Content {
			if play.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(play.Content); j += 2 {
				k, v := play.Content[j], play.Content[j+1]
				switch k.Value {
				case "strategy":
					if v.Kind == yaml.ScalarNode && v.Value != "linear" && v.Line == k.Line {
						lineNos = append(lineNos, k.Line)
					}
				case "import_playbook":
					if v.Kind == yaml.ScalarNode && !strings.Contains(v.Value, "{{") {
						add(filepath.Join(filepath.Dir(orig), v.Value))
					}
				}
			}
		}
		if len(lineNos) == 0 {
			continue
		}
		lines := strings.Split(string(data), "\n")
		sort.Ints(lineNos)
		edited := false
		for _, n := range lineNos {
			if n < 1 || n > len(lines) {
				continue
			}
			if m := strategyLine.FindStringSubmatch(lines[n-1]); m != nil {
				lines[n-1] = m[1] + "linear"
				edited = true
			}
		}
		if !edited {
			continue
		}
		// Break the hardlink first - see spliceFile.
		if os.Remove(path) != nil || os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644) != nil {
			continue
		}
		changed = true
	}
	return changed
}
