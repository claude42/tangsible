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

// Run export (design-docs/Revisit.md, "Export runs"): one run as a single
// plain-text file that a human can read without tangsible - header, the
// ansible output (colors stripped), stderr - followed by the saved event
// log verbatim, which "tangsible revisit <file>" replays. Events come last
// and unescaped so the existing line scanner reads them unchanged.
package revisit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/runner"
)

const (
	exportMagic        = "# tangsible run export v1"
	exportOutputMarker = "==== output ===="
	exportStderrMarker = "==== stderr ===="
	exportEventsMarker = "==== tangsible events (jsonl, verbatim) ===="
)

// ExportedRun is what ReadExport recovers from an export file: the entry's
// own fields, plus the saved data a local run would have read from
// .tangsible/runs/ (stderr and output are the color-stripped text).
type ExportedRun struct {
	Entry     RevisitEntry
	Output    []string
	HasOutput bool
	Stderr    []string
	Events    []byte
}

// WriteExport writes e as an export file to w. statusLabel is the exit
// code's human label (RevisitStatusLabel); hasOutput false omits the output
// section (a run saved before .stdout existed).
func WriteExport(w io.Writer, e RevisitEntry, statusLabel string, output []string, hasOutput bool, stderr []string, events io.Reader) error {
	oneLine := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	var b strings.Builder
	b.WriteString(exportMagic + "\n")
	fmt.Fprintf(&b, "# command:  %s\n", oneLine(RevisitCommandText(e)))
	if e.Name != "" {
		fmt.Fprintf(&b, "# name:     %s\n", oneLine(e.Name))
	}
	fmt.Fprintf(&b, "# started:  %s\n", e.Time)
	fmt.Fprintf(&b, "# exit:     %d (%s)\n", e.ExitCode, statusLabel)
	if e.Role != "" {
		fmt.Fprintf(&b, "# role:     %s\n", oneLine(e.Role))
	} else {
		fmt.Fprintf(&b, "# playbook: %s\n", oneLine(e.Playbook))
	}
	fmt.Fprintf(&b, "# args:     %s\n", oneLine(e.Args))

	section := func(marker string, lines []string) {
		b.WriteString("\n" + marker + "\n")
		for _, l := range lines {
			b.WriteString(runner.StripANSI(l) + "\n")
		}
	}
	if hasOutput {
		section(exportOutputMarker, output)
	}
	if len(stderr) > 0 {
		section(exportStderrMarker, stderr)
	}
	b.WriteString("\n" + exportEventsMarker + "\n")
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	_, err := io.Copy(w, events)
	return err
}

// ReadExport parses an export file's content.
func ReadExport(data []byte) (*ExportedRun, error) {
	r := &ExportedRun{}
	section := "header"
	var cur *[]string
	first := true
	for pos := 0; pos < len(data); {
		end := bytes.IndexByte(data[pos:], '\n')
		next := len(data)
		if end >= 0 {
			end += pos
			next = end + 1
		} else {
			end = len(data)
		}
		line := strings.TrimSuffix(string(data[pos:end]), "\r")
		if first {
			if line != exportMagic {
				return nil, errors.New("not a tangsible run export")
			}
			first = false
			pos = next
			continue
		}
		switch line {
		case exportOutputMarker:
			section, cur, r.HasOutput = "output", &r.Output, true
		case exportStderrMarker:
			section, cur = "stderr", &r.Stderr
		case exportEventsMarker:
			r.Events = data[next:]
			for _, s := range []*[]string{&r.Output, &r.Stderr} {
				// each section is preceded by a blank separator line, which
				// belongs to the following marker, not the section.
				if n := len(*s); n > 0 && (*s)[n-1] == "" {
					*s = (*s)[:n-1]
				}
			}
			return r, r.finish()
		default:
			switch section {
			case "header":
				if err := r.headerLine(line); err != nil {
					return nil, err
				}
			default:
				*cur = append(*cur, line)
			}
		}
		pos = next
	}
	return nil, errors.New("export has no events section")
}

var exportHeaderRe = regexp.MustCompile(`^# ([a-z]+):\s*(.*)$`)

func (r *ExportedRun) headerLine(line string) error {
	m := exportHeaderRe.FindStringSubmatch(line)
	if m == nil {
		return nil // blank separator, or a line from a newer version - ignore
	}
	val := strings.TrimSpace(m[2])
	switch m[1] {
	case "name":
		r.Entry.Name = val
	case "started":
		r.Entry.Time = val
	case "exit":
		code, _, _ := strings.Cut(val, " ")
		n, err := strconv.Atoi(code)
		if err != nil {
			return fmt.Errorf("bad exit code %q in export header", val)
		}
		r.Entry.ExitCode = n
	case "playbook":
		r.Entry.Playbook = val
	case "role":
		r.Entry.Role = val
	case "args":
		r.Entry.Args = val
	}
	return nil
}

func (r *ExportedRun) finish() error {
	if r.Entry.Playbook == "" && r.Entry.Role == "" {
		return errors.New("export header has neither a playbook nor a role")
	}
	if _, err := time.Parse(time.RFC3339, r.Entry.Time); err != nil {
		return fmt.Errorf("bad start time %q in export header", r.Entry.Time)
	}
	r.Entry.Imported = r
	return nil
}

// IsExportFile reports whether path is a regular file that starts with the
// export format's first line - how "tangsible revisit <arg>" tells an export
// from a playbook name.
func IsExportFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(exportMagic))
	if _, err := io.ReadFull(f, buf); err != nil {
		return false
	}
	return string(buf) == exportMagic
}

var exportUnsafeRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// ExportFileName suggests a file name for e: its name if it has one, else
// its start time and command line, reduced to filename-safe characters.
func ExportFileName(e RevisitEntry) string {
	base := e.Name
	if base == "" {
		stamp := e.Time
		if t, err := time.Parse(time.RFC3339, e.Time); err == nil {
			stamp = t.Local().Format("2006-01-02_15-04-05")
		}
		base = stamp + "_" + strings.TrimPrefix(RevisitCommandText(e), "tangsible ")
	}
	base = strings.Trim(exportUnsafeRe.ReplaceAllString(base, "_"), "_.")
	if len(base) > 80 {
		base = strings.TrimRight(base[:80], "_.")
	}
	if base == "" {
		base = "tangsible-run"
	}
	return base + ".txt"
}

// ExportRun writes e's saved run to path, refusing to overwrite an existing
// file. A failed write leaves no partial file behind.
func ExportRun(e RevisitEntry, path string) error {
	jsonlPath, _ := config.RunLogPaths(config.TangsibleStatePath, e.RunID)
	events, err := os.Open(jsonlPath)
	if err != nil {
		return fmt.Errorf("saved run data is gone: %w", err)
	}
	defer events.Close()

	var unreachable *bool
	if e.ExitCode == tqmUnreachableHostsBit {
		unreachable = resolveHadUnreachable(e)
	}
	output, hasOutput := config.ReadRunOutput(config.TangsibleStatePath, e.RunID)
	stderr := config.ReadRunStderr(config.TangsibleStatePath, e.RunID)
	for len(stderr) > 0 && stderr[len(stderr)-1] == "" {
		stderr = stderr[:len(stderr)-1]
	}

	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return errors.New("file exists - choose another name")
		}
		return err
	}
	err = WriteExport(out, e, RevisitStatusLabel(e.ExitCode, unreachable), output, hasOutput, stderr, events)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}
