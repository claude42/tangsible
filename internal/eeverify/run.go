package eeverify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/eebuild"
	"code.aw.net/claude/tangsible/internal/eeinit"
	"code.aw.net/claude/tangsible/internal/runner"
)

// LogFile is where the build's output goes, under the project's .tangsible
// directory.
const LogFile = "ee-verify.log"

// Run is `tangsible ee verify` (args are what follows "verify"); it returns the
// exit code.
func Run(args []string) int {
	opts, err := ParseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n%s\n", err, Usage)
		return 2
	}
	return verify(opts, os.Stdout, os.Stderr)
}

func verify(opts Options, stdout, stderr io.Writer) int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "tangsible: "+format+"\n", a...)
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail("%v", err)
	}

	// The cheap refusals come before any work.
	if err := eebuild.CheckFile(opts.File); err != nil {
		return fail("%v", err)
	}
	if err := eebuild.CheckTools(); err != nil {
		return fail("%v", err)
	}
	playbook := opts.Playbook
	if playbook == "" {
		var src string
		if playbook, src = config.ResolvePlaybook(); playbook == "" {
			return fail("no playbook given, and no default found (TANGSIBLE_PLAYBOOK, default_playbook, ./site.yml)")
		}
		fmt.Fprintf(stderr, "tangsible: using %s from %s\n", playbook, src)
	}
	if _, err := os.Stat(playbook); err != nil {
		return fail("playbook %s: %v", playbook, errors.Unwrap(err))
	}
	proj, err := eeinit.ScanProject(playbook, cwd)
	if err != nil {
		return fail("%v", err)
	}
	settings := config.EEFromSettings(config.MergedSettingsConfig())

	// The build's log; the image is removed or kept below, whatever happens.
	logPath, logf, err := eebuild.OpenLog(LogFile)
	if err != nil {
		return fail("%v", err)
	}
	defer logf.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	tag := NewTag()
	res := Result{File: opts.File, LogPath: logPath, Tag: tag}
	before := eebuild.TakeSnapshot()
	built, keep := false, false
	defer func() {
		before.Cleanup(tag, built, keep)
	}()
	finish := func() int {
		text, code := Render(res)
		fmt.Fprint(stdout, text)
		return code
	}

	// 1. Build.
	fmt.Fprintf(stderr, "tangsible: building %s into a throw-away image (this can take several minutes; output in %s) ...\n", opts.File, logPath)
	var buildErr error
	output, stopOutput := eebuild.Output(logf, stderr, opts.ShowOutput)
	res.BuildDuration, buildErr = eebuild.Execute(ctx, eebuild.Spec{File: opts.File, Tags: []string{tag}, Network: settings.Network, NoCache: opts.NoCache}, output)
	stopOutput()
	if ctx.Err() != nil {
		res.Interrupted = true
		return finish()
	}
	if buildErr != nil {
		res.BuildErr = buildErr.Error()
		res.BuildTail = eebuild.TailOfFile(logPath, eebuild.TailLines)
		return finish()
	}
	built = true
	keep = opts.Keep
	res.Kept = opts.Keep

	// The image is local: run the checks in it the way a real run would.
	settings.Image = tag
	settings.PullPolicy = "never"
	runner.SetExecutionEnvironment(&settings)
	warnings, err := runner.ValidateExecutionEnvironment()
	if err != nil {
		return fail("%v", err)
	}
	for _, w := range warnings {
		fmt.Fprintf(stderr, "tangsible: execution environment: %s\n", w)
	}
	fmt.Fprintln(stderr, "tangsible: checking the image ...")

	// 2. Syntax check.
	res.SyntaxRan = true
	syntax, err := runner.NewToolCommand("ansible-playbook", append([]string{"--syntax-check"}, opts.Args...), runner.ToolOptions{Playbook: playbook})
	if err != nil {
		res.SyntaxOutput = err.Error()
	} else {
		out, runErr := syntax.CombinedOutput()
		res.SyntaxOK = runErr == nil
		res.SyntaxOutput = string(out)
	}
	if ctx.Err() != nil {
		res.Interrupted = true
		return finish()
	}

	// 3. Plugins and binaries, in one more run.
	binaries, _ := eeinit.ScanBinaries(proj.Commands, filepath.Dir(proj.Abs))
	names := Names{Modules: ModuleNames(proj.Uses), Lookups: LookupNames(proj.Lookups)}
	for _, b := range binaries {
		names.Binaries = append(names.Binaries, b.Name)
	}
	check, err := runner.NewToolCommand("sh", []string{"-c", CheckScript(names)}, runner.ToolOptions{})
	if err != nil {
		res.CheckErr = err.Error()
		return finish()
	}
	var errBuf strings.Builder
	check.Stderr = &errBuf
	out, runErr := check.Output()
	if ctx.Err() != nil {
		res.Interrupted = true
		return finish()
	}
	checked, perr := ParseCheck(string(out))
	switch {
	case perr != nil:
		res.CheckErr = perr.Error()
		if runErr != nil && strings.TrimSpace(errBuf.String()) != "" {
			res.CheckErr += ": " + firstLine(errBuf.String())
		}
	default:
		res.Checked = true
		res.ImageCore = checked.AnsibleCore
		res.HostCore = hostCore()
		res.Modules = ModuleCheck(proj.Uses, checked.Modules)
		res.Lookups = LookupCheck(proj.Lookups, checked.Lookups)
		res.Binaries = BinaryCheck(binaries, checked.MissingBinaries)
	}
	return finish()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// hostCore is the host's ansible-core version ("" when ansible-playbook says
// nothing usable); it runs on the host, not in the image.
func hostCore() string {
	out, err := exec.Command("ansible-playbook", "--version").Output()
	if err != nil {
		return ""
	}
	return ParseCore(string(out))
}
