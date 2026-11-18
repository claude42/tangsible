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

// Package eeverify is `tangsible ee verify`, design-docs/EEInit.md
// ("Verifying a definition"): it builds an execution-environment.yml into a
// throw-away image, checks that image against what the playbook uses, and
// reports what is missing. The argument parsing, the ansible-builder command
// line, the check script and its output parser, and the report are pure;
// run.go does the building, running and cleaning up.
package eeverify

import (
	"errors"
	"fmt"
	"strings"

	"code.aw.net/claude/tangsible/internal/config"
)

// Usage is the synopsis printed with a usage error.
const Usage = "usage: tangsible ee verify [playbook.yml] [-f file] [--keep] [--no-cache] [ansible-playbook args ...]"

// DefaultFile is the definition verified when -f is not given.
const DefaultFile = "execution-environment.yml"

// Options is the parsed command line of `tangsible ee verify`.
type Options struct {
	// Playbook is "" when it should be resolved like run does.
	Playbook string
	// File is the definition to build.
	File string
	// Keep leaves the built image in place.
	Keep bool
	// NoCache builds without podman's build cache (and leaves none).
	NoCache bool
	// Args are passed to ansible-playbook for the syntax check.
	Args []string
}

// ParseArgs reads the arguments after "verify". -f/--file, --keep and --no-cache are
// tangsible's own; the rest is the playbook and ansible-playbook arguments,
// split like run's. The password prompts are refused: a syntax check needs no
// secrets, and a prompt would have nowhere to go.
func ParseArgs(args []string) (Options, error) {
	o := Options{File: DefaultFile}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-f" || a == "--file":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return o, fmt.Errorf("%s needs a file", a)
			}
			i++
			o.File = args[i]
		case strings.HasPrefix(a, "--file="):
			if o.File = strings.TrimPrefix(a, "--file="); o.File == "" {
				return o, errors.New("--file needs a file")
			}
		case a == "--keep":
			o.Keep = true
		case a == "--no-cache":
			o.NoCache = true
		default:
			rest = append(rest, a)
		}
	}
	playbook, passthrough, _ := config.SplitPlaybookArgs(rest)
	o.Playbook = playbook
	if len(passthrough) > 0 {
		o.Args = passthrough
	}
	if config.HasInteractiveCredentialFlag(o.Args) {
		return o, errors.New("--ask-pass, --ask-become-pass and --ask-vault-pass are not supported here (a syntax check needs none; use a vault password file)")
	}
	return o, nil
}
