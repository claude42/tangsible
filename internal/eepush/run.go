package eepush

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"code.aw.net/claude/tangsible/internal/eebuild"
)

// Run is `tangsible ee push` (args are what follows "push"); it returns the
// exit code.
func Run(args []string) int {
	opts, err := ParseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n%s\n", err, Usage)
		return 2
	}
	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	return push(opts, os.Stdin, interactive, os.Stdout, os.Stderr)
}

// tail keeps the last lines written to it, for the failure report and the
// hint, and passes everything on.
type tail struct {
	mu    sync.Mutex
	out   io.Writer
	lines []string
	part  string
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.part += string(p)
	for {
		i := strings.IndexByte(t.part, '\n')
		if i < 0 {
			break
		}
		if l := strings.TrimSpace(t.part[:i]); l != "" {
			t.lines = append(t.lines, l)
		}
		t.part = t.part[i+1:]
	}
	t.lines = eebuild.LastLines(t.lines, eebuild.TailLines)
	return t.out.Write(p)
}

func (t *tail) text() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.lines, "\n") + "\n" + t.part
}

func push(opts Options, stdin io.Reader, interactive bool, stdout, stderr io.Writer) int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "tangsible: "+format+"\n", a...)
		return 1
	}
	// The cheap refusals come before any work.
	if err := eebuild.CheckFile(opts.File); err != nil {
		return fail("%v", err)
	}
	if err := eebuild.CheckPodman(); err != nil {
		return fail("%v", err)
	}
	text, err := os.ReadFile(opts.File)
	if err != nil {
		return fail("%s: %v", opts.File, errors.Unwrap(err))
	}
	names, err := eebuild.Tags(text)
	if err != nil {
		return fail("%s: %v", opts.File, err)
	}
	if len(names) == 0 {
		return fail("%s names no image: set options.tags in the definition (tangsible ee init writes a commented tags stanza) and build it with tangsible ee build", opts.File)
	}

	// Every name is checked before any is pushed.
	var problems []string
	var images []Image
	var skipped []string
	for _, n := range names {
		if Local(n) {
			skipped = append(skipped, n)
			continue
		}
		if p := Problem(n); p != "" {
			problems = append(problems, p)
			continue
		}
		id, size, ok := eebuild.ImageOf(n)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: no such image here; build it with tangsible ee build", n))
			continue
		}
		images = append(images, Image{Name: n, ID: id, Size: size})
	}
	if len(problems) > 0 {
		fmt.Fprintf(stderr, "tangsible: nothing was pushed:\n")
		for _, p := range problems {
			fmt.Fprintf(stderr, "  %s\n", p)
		}
		return 1
	}

	if len(images) == 0 {
		return fail("nothing to push: no name in %s has a registry (%s); put the registry into options.tags, for example registry.example.com/team/name:tag", opts.File, strings.Join(skipped, ", "))
	}
	if !opts.Yes && !interactive {
		return fail("not pushing without a confirmation, and there is no terminal to ask on: use --yes")
	}
	fmt.Fprint(stderr, Summary(images, skipped))
	if !opts.Yes {
		fmt.Fprint(stderr, "Proceed? [y/N] ")
		answer, _ := bufio.NewReader(stdin).ReadString('\n')
		if !Accepts(answer) {
			return fail("not pushed")
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	var done []string
	for _, im := range images {
		fmt.Fprintf(stderr, "tangsible: pushing %s ...\n", im.Name)
		digest, out, err := pushOne(ctx, im.Name, stderr)
		switch {
		case ctx.Err() != nil:
			fmt.Fprintf(stderr, "tangsible: interrupted while pushing %s%s\n", im.Name, pushedList(done))
			return 130
		case err != nil:
			fmt.Fprintf(stderr, "tangsible: pushing %s failed: %v\n", im.Name, err)
			if AuthFailure(out) {
				fmt.Fprintf(stderr, "tangsible: %s\n", LoginHint(Registry(im.Name)))
			}
			if len(done) > 0 {
				fmt.Fprintf(stderr, "tangsible:%s\n", pushedList(done))
			}
			return 1
		}
		done = append(done, im.Name)
		if digest != "" {
			fmt.Fprintf(stdout, "Pushed %s (%s)\n", im.Name, digest)
		} else {
			fmt.Fprintf(stdout, "Pushed %s\n", im.Name)
		}
	}
	return 0
}

func pushedList(done []string) string {
	if len(done) == 0 {
		return ""
	}
	return " (already pushed: " + strings.Join(done, ", ") + ")"
}

// pushOne runs podman push for name, in its own process group so an interrupt
// stops it. It returns the digest podman wrote (empty when it wrote none) and
// the last lines of its output.
func pushOne(ctx context.Context, name string, progress io.Writer) (digest, output string, err error) {
	f, err := os.CreateTemp("", "tangsible-ee-push-")
	if err != nil {
		return "", "", err
	}
	digestFile := f.Name()
	f.Close()
	defer os.Remove(digestFile)

	t := &tail{out: progress}
	cmd := exec.CommandContext(ctx, "podman", "push", "--digestfile", digestFile, name)
	cmd.Stdout, cmd.Stderr = t, t
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT) }
	cmd.WaitDelay = 20 * time.Second
	err = cmd.Run()
	if b, rerr := os.ReadFile(digestFile); rerr == nil {
		digest = strings.TrimSpace(string(b))
	}
	return digest, t.text(), err
}
