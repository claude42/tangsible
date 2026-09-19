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

// Implements the drill-down view's "File" tab (design-docs/
// ShowFileContents.md): a task-modified remote file's *current* contents,
// pulled via a throwaway ansible-playbook run using ansible.builtin.fetch -
// the same reasoning resolved.go's resolveTaskValues already applies to
// resolving a task's own values (ansible's own mechanisms, on ansible's own
// terms, rather than reimplementing an SSH/become/vault-aware file transfer
// ourselves).
package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
)

// isBinaryContent reports whether data looks like binary content rather
// than text - a NUL byte anywhere in the first 8000 bytes (git's own
// long-standing heuristic for the same question), same "documented, not
// chased to 100%" tolerance as this package's other content heuristics
// (e.g. wrapJinjaDefaults' regex-based Jinja matching). Deliberately not an
// error: fetchRemoteFileContents treats "binary" as "nothing to show,"
// identical to any other reason the File tab stays hidden.
func isBinaryContent(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(data[:n], 0) != -1
}

// readLocalFileContents reads path directly off the control host's own
// filesystem - used instead of fetchRemoteFileContents whenever
// uikit.RemoteFilePath reports the task actually ran with
// delegate_to: localhost (see uikit.DelegatedToLocalhost): the file never
// left the control host in the first place, so spawning a throwaway
// ansible-playbook run against the named host would be pointless (and
// could even fail outright, e.g. an unreachable host whose only real work
// was always delegated locally). Same binary-content handling as
// fetchRemoteFileContents - ("", nil), not an error, so the File tab hides
// exactly the same way.
func readLocalFileContents(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if isBinaryContent(content) {
		return "", nil
	}
	return string(content), nil
}

// fetchRemoteFileContents pulls remotePath's current content off host via a
// throwaway one-task ansible-playbook run using ansible.builtin.fetch,
// mirroring resolveTaskValues' own "temp file(s) + stub playbook + scan
// stdout for this host's own result" shape almost exactly - swapping
// ansible.builtin.template for ansible.builtin.fetch, and with no Jinja-
// wrapping step (there's no template source to render here). rest is the
// current generation's own passthrough args (importantly -i/-e, so the stub
// sees the same inventory/connection context the real run did) - see
// resolveTaskValues' own doc comment for why this can't collide with the
// stub's own "hosts: <host>" line the way an appended -l/--limit might.
//
// Returns ("", nil) - not an error - when the fetched content looks binary
// (isBinaryContent): the File tab treats that identically to any other
// "nothing to show" case (uikit.FileTabHidden), per design-docs/
// ShowFileContents.md's "don't display it" for binary content.
//
// err is non-nil for a genuine failure to fetch anything at all: an
// interactive credential prompt would otherwise be required (see
// config.HasInteractiveCredentialFlag's own doc comment for why that's
// refused outright rather than attempted), the host was unreachable, the
// fetch task itself failed, or a temp-file/exec-level error. Every one of
// these just means the File tab stays hidden too (uikit.FileTabHidden makes
// no distinction based on Err, per design-docs/ShowFileContents.md) - Err is
// carried on ResolvedRender only for potential future diagnostic use, not
// because anything currently displays it.
func fetchRemoteFileContents(remotePath, host string, rest []string) (string, error) {
	if config.HasInteractiveCredentialFlag(rest) {
		return "", fmt.Errorf("run used an interactive credential prompt")
	}

	outFile, err := os.CreateTemp("", "tangsible-fetch-out-*")
	if err != nil {
		return "", err
	}
	destPath := outFile.Name()
	outFile.Close()
	defer os.Remove(destPath)

	var b strings.Builder
	fmt.Fprintf(&b, "- hosts: %s\n", host)
	b.WriteString("  gather_facts: false\n")
	b.WriteString("  ignore_unreachable: true\n")
	b.WriteString("  tasks:\n")
	b.WriteString("    - name: fetch file contents\n")
	b.WriteString("      ansible.builtin.fetch:\n")
	fmt.Fprintf(&b, "        src: %s\n", quoteYAMLString(remotePath))
	fmt.Fprintf(&b, "        dest: %s\n", destPath)
	b.WriteString("        flat: true\n")

	stubFile, err := os.CreateTemp("", "tangsible-fetch-stub-*.yml")
	if err != nil {
		return "", err
	}
	stubPath := stubFile.Name()
	defer os.Remove(stubPath)
	if _, err := stubFile.WriteString(b.String()); err != nil {
		stubFile.Close()
		return "", err
	}
	stubFile.Close()

	cmd := exec.Command("ansible-playbook", append([]string{stubPath}, rest...)...)
	cmd.Env = append(os.Environ(),
		"ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl",
		"ANSIBLE_JSON_INDENT=0",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()

	// Same event-scanning approach as resolveTaskValues/RenderTemplate:
	// scan every line for our one host's own result on the one task this
	// stub ever runs, keeping only the last match.
	var raw json.RawMessage
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev playbook.RawEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch ev.Event {
		case "v2_runner_on_ok", "v2_runner_on_failed", "v2_runner_on_unreachable":
			if hostRaw, ok := ev.Hosts[host]; ok {
				raw = hostRaw
			}
		}
	}

	if raw == nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		if msg == "" {
			msg = fmt.Sprintf("no result reported for host %q", host)
		}
		return "", fmt.Errorf("%s", msg)
	}

	decoded := playbook.DecodeHostResult(raw)
	if decoded.Failed || decoded.Unreachable {
		var full map[string]interface{}
		_ = json.Unmarshal(raw, &full)
		_, msg := uikit.PrimaryOutputField(full)
		if msg == "" {
			msg = decoded.Msg
		}
		return "", fmt.Errorf("%s", msg)
	}

	content, err := os.ReadFile(destPath)
	if err != nil {
		return "", fmt.Errorf("fetch reported success but the file couldn't be read back: %w", err)
	}
	if isBinaryContent(content) {
		return "", nil
	}
	return string(content), nil
}

// quoteYAMLString renders s as a YAML double-quoted scalar - Go's %q
// produces C-style backslash escaping that YAML's own double-quote syntax
// is a superset of for the common case (quotes, backslashes, control
// characters), so this is good enough for an arbitrary remote path without
// pulling in a full YAML encoder just for one scalar - same "good enough,
// not chased further" tolerance this package already applies elsewhere
// (e.g. wrapJinjaDefaults' regex-based Jinja matching).
func quoteYAMLString(s string) string {
	return fmt.Sprintf("%q", s)
}
