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

// Package varscrawl lists every place a host's variables are defined, in
// ansible's own order of precedence, and says which definition wins - the
// data behind `tangsible variables` (design-docs/Variables.md). Nothing runs:
// an embedded Python script makes the calls ansible's VariableManager makes,
// one source at a time, so each level of the precedence order is a layer with
// the file and line of every value in it. The script also asks ansible for
// its own merged answer; merge compares the two (ansible is always right).
// The tview-free half is pure and tested; Run is the only part that spawns.
package varscrawl

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/staticplay"
)

//go:embed loader.py
var loaderScript string

// Definition is one place a variable is defined.
type Definition struct {
	Level  string          // "inventory group_vars", "role defaults", "extra vars", ...
	Entity string          // the group, host or role it belongs to ("-e" for extra vars)
	Play   string          // for role levels: the play that applies the role
	Value  json.RawMessage // vaulted values arrive masked as "<vaulted>"
	Path   string          // the file, absolute; empty when there is none
	Line   int
	Desc   string // an origin that is no file ("<CLI option '-e'>")
	// FromAnsible marks a definition the crawl did not find: it comes from
	// ansible's own answer alone.
	FromAnsible bool
}

// Variable is one variable with all its definitions, highest precedence first.
type Variable struct {
	Name string
	Defs []Definition
	// Checked says ansible's own answer was available to compare with;
	// Disagree that it named a different definition than the crawl ranked
	// first (the crawl's order is then the uncertain part).
	Checked  bool
	Disagree bool
}

// Result is everything Run found for one host.
type Result struct {
	Host      string
	Groups    []string // the host's groups, lowest precedence first (all excluded)
	Plays     []string // the plays that apply to the host
	Notes     []string // what the crawl could not look at
	Variables []Variable
}

type rawDef struct {
	Value json.RawMessage `json:"value"`
	Path  string          `json:"path"`
	Line  int             `json:"line"`
	Desc  string          `json:"desc"`
}

type rawLayer struct {
	Level     string            `json:"level"`
	Entity    string            `json:"entity"`
	Play      string            `json:"play"`
	Variables map[string]rawDef `json:"variables"`
}

type rawResult struct {
	Host    string            `json:"host"`
	Groups  []string          `json:"groups"`
	Layers  []rawLayer        `json:"layers"`
	Winners map[string]rawDef `json:"winners"`
	Plays   []string          `json:"plays"`
	Notes   []string          `json:"notes"`
}

// Parse decodes the loader script's output and merges it (see merge).
func Parse(out []byte) (Result, error) {
	var raw rawResult
	if err := json.Unmarshal(out, &raw); err != nil {
		return Result{}, fmt.Errorf("unexpected output from the variables loader: %w", err)
	}
	return merge(raw), nil
}

// merge turns the layers (lowest precedence first) into one Variable per
// name, definitions highest precedence first, and checks the winner against
// ansible's own answer.
func merge(raw rawResult) Result {
	byName := map[string][]Definition{}
	for _, layer := range raw.Layers {
		for name, d := range layer.Variables {
			byName[name] = append(byName[name], Definition{
				Level: layer.Level, Entity: layer.Entity, Play: layer.Play,
				Value: d.Value, Path: d.Path, Line: d.Line, Desc: d.Desc,
			})
		}
	}
	res := Result{Host: raw.Host, Groups: raw.Groups, Plays: raw.Plays, Notes: raw.Notes}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		defs := byName[name]
		for i, j := 0, len(defs)-1; i < j; i, j = i+1, j-1 {
			defs[i], defs[j] = defs[j], defs[i]
		}
		v := Variable{Name: name, Defs: defs}
		if w, ok := raw.Winners[name]; ok {
			v.Checked = true
			v.Defs, v.Disagree = applyWinner(defs, w)
		}
		res.Variables = append(res.Variables, v)
	}
	return res
}

// applyWinner puts ansible's answer on top. When the crawl ranked the same
// definition first nothing changes; when it ranked another first, the matching
// definition (or, failing that, one made of ansible's answer alone) moves to
// the top and disagree is true.
func applyWinner(defs []Definition, w rawDef) ([]Definition, bool) {
	if agrees(defs[0], w) {
		return defs, false
	}
	for i, d := range defs {
		if agrees(d, w) {
			out := append([]Definition{d}, defs[:i]...)
			return append(out, defs[i+1:]...), true
		}
	}
	top := Definition{Level: "ansible's answer", Value: w.Value, Path: w.Path, Line: w.Line, Desc: w.Desc, FromAnsible: true}
	return append([]Definition{top}, defs...), true
}

// agrees: both name the same origin, or - ansible cannot tag a boolean or a
// null with one - the winner has none and the values are equal.
func agrees(d Definition, w rawDef) bool {
	if w.Path == "" && w.Desc == "" {
		return bytes.Equal(compact(d.Value), compact(w.Value))
	}
	return d.Path == w.Path && d.Line == w.Line && d.Desc == w.Desc
}

func compact(raw json.RawMessage) []byte {
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return raw
	}
	return b.Bytes()
}

// Run crawls the variables of hostname. playbook may be empty (only the
// inventory-side sources are listed); rest are ansible-playbook's own
// options (-i, -e, --vault-password-file, ...). The interpreter is the one
// ansible-playbook runs under, on the host: like the host verb, this never
// goes through an execution environment.
func Run(hostname, playbook string, rest []string) (Result, error) {
	dir, err := runner.ResolveCallbackPluginDir()
	if err != nil {
		return Result{}, err
	}
	pb := playbook
	if pb == "" {
		pb = "-"
	}
	python := staticplay.InterpreterOf("ansible-playbook", "python3")
	args := append([]string{"-c", loaderScript, filepath.Join(dir, "tangsible_events.py"), hostname, pb}, rest...)
	cmd := exec.Command(python, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return Result{}, errors.New(msg)
		}
		return Result{}, fmt.Errorf("couldn't run %s: %w", python, err)
	}
	return Parse(stdout.Bytes())
}
