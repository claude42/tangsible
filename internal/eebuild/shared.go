// Package eebuild is `tangsible ee build` (design-docs/EEInit.md, "Building a
// definition") and the build step `tangsible ee verify` shares with it: the
// ansible-builder command line, the podman snapshot and cleanup, the log, and
// the build run in its own process group. The argument parsing, the tag
// extraction and the report are pure; run.go is the verb.
package eebuild

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/execerr"
)

// TailLines is how much of a failed build's output is shown.
const TailLines = 40

// CheckFile fails when the definition is missing, hinting at `ee init`.
func CheckFile(file string) error {
	if _, err := os.Stat(file); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s not found (tangsible ee init drafts one)", file)
		}
		return fmt.Errorf("%s: %v", file, errors.Unwrap(err))
	}
	return nil
}

// CheckPodman fails when podman is not on PATH.
func CheckPodman() error { return checkTool("podman") }

// CheckTools fails naming the first of podman and ansible-builder that is not
// on PATH.
func CheckTools() error {
	for _, tool := range []string{"podman", "ansible-builder"} {
		if err := checkTool(tool); err != nil {
			return err
		}
	}
	return nil
}

func checkTool(tool string) error {
	if _, err := exec.LookPath(tool); err != nil {
		if msg := execerr.NotFoundMessage(tool, err); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

// OpenLog creates (truncating) the named log under the project's .tangsible
// directory, 0600, and returns its path.
func OpenLog(name string) (string, *os.File, error) {
	if err := os.MkdirAll(config.TangsibleDir, 0o755); err != nil {
		return "", nil, err
	}
	path := filepath.Join(config.TangsibleDir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	return path, f, err
}

// Spec is one build.
type Spec struct {
	File string
	// Tags replace the definition's own options.tags; none keeps them.
	Tags    []string
	Network string
	NoCache bool
}

// Execute runs ansible-builder for s in a private temporary context (removed
// afterwards, so nothing like context/ appears in the project), writing its
// output to log, and returns how long it took. ctx cancels it: the build is
// interrupted through its own process group, so the podman build it started
// stops too; the caller checks ctx.Err() to tell that from a failed build.
func Execute(ctx context.Context, s Spec, log io.Writer) (time.Duration, error) {
	ctxDir, err := os.MkdirTemp("", "tangsible-ee-build-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(ctxDir)
	start := time.Now()
	build := exec.CommandContext(ctx, "ansible-builder", BuildArgs(s.File, ctxDir, s.Tags, s.Network, s.NoCache)...)
	build.Stdout, build.Stderr = log, log
	build.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	build.Cancel = func() error { return syscall.Kill(-build.Process.Pid, syscall.SIGINT) }
	build.WaitDelay = 20 * time.Second
	err = build.Run()
	return time.Since(start), err
}

// TailOfFile is the last n non-empty lines of the file at path.
func TailOfFile(path string, n int) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return LastLines(lines, n)
}

// LastLines is the last n of lines.
func LastLines(lines []string, n int) []string {
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}
