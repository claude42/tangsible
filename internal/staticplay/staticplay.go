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

// Package staticplay reads a playbook the way ansible-playbook does but runs
// nothing: its plays, their host patterns and the tasks in execution order,
// each with the file and line it is defined at (design-docs/View.md). It is
// the static layer behind `tangsible view`; the parsing is ansible's own,
// done by an embedded Python script, so roles, blocks and imports resolve as
// ansible resolves them. Dynamic content (include_tasks, a templated name or
// host pattern) is not expanded.
package staticplay

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"code.aw.net/claude/tangsible/internal/runner"
)

//go:embed loader.py
var loaderScript string

// Task is one task as the playbook defines it.
type Task struct {
	Name   string   `json:"name"`
	Action string   `json:"action"`
	Path   string   `json:"path"`
	Line   int      `json:"line"`
	Role   string   `json:"role"`
	Tags   []string `json:"tags"`
	When   []string `json:"when"`
}

// Location is the task's source position in the form ansible reports a
// task's path ("<file>:<line>"), which the source index is keyed by.
func (t Task) Location() string {
	if t.Path == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", t.Path, t.Line)
}

// Play is one play with its host pattern as written (not templated).
type Play struct {
	Name  string `json:"name"`
	Hosts string `json:"hosts"`
	Tasks []Task `json:"tasks"`
}

// Playbook is everything Load found.
type Playbook struct {
	Plays []Play `json:"plays"`
}

// Parse decodes the loader script's output.
func Parse(out []byte) (Playbook, error) {
	var pb Playbook
	if err := json.Unmarshal(out, &pb); err != nil {
		return Playbook{}, fmt.Errorf("unexpected output from the playbook loader: %w", err)
	}
	return pb, nil
}

// Load reads playbook. The interpreter is the one ansible-playbook itself
// runs under (so ansible is importable also when it is installed in a
// virtualenv), python3 inside an execution environment.
func Load(playbook string) (Playbook, error) {
	python := "python3"
	if runner.ExecutionEnvironment() == nil {
		python = interpreterOf("ansible-playbook", python)
	}
	cmd, err := runner.NewToolCommand(python, []string{"-c", loaderScript, playbook}, runner.ToolOptions{})
	if err != nil {
		return Playbook{}, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return Playbook{}, errors.New(msg)
		}
		return Playbook{}, fmt.Errorf("couldn't run %s: %w", python, err)
	}
	return Parse(stdout.Bytes())
}

// interpreterOf returns the interpreter named by tool's shebang line, or
// fallback when the tool, or a usable shebang, isn't found.
func interpreterOf(tool, fallback string) string {
	path, err := exec.LookPath(tool)
	if err != nil {
		return fallback
	}
	f, err := os.Open(path)
	if err != nil {
		return fallback
	}
	defer f.Close()
	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	return shebangInterpreter(string(buf[:n]), fallback)
}

// shebangInterpreter extracts the interpreter from the first line of a
// script ("#!/usr/bin/python3 -s", "#!/usr/bin/env python3"), fallback when
// there is none.
func shebangInterpreter(head, fallback string) string {
	line, _, _ := strings.Cut(head, "\n")
	if !strings.HasPrefix(line, "#!") {
		return fallback
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) == 0 {
		return fallback
	}
	if strings.HasSuffix(fields[0], "/env") {
		if len(fields) > 1 && !strings.HasPrefix(fields[1], "-") {
			return fields[1]
		}
		return fallback
	}
	return fields[0]
}
