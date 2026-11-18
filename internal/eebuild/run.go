package eebuild

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"code.aw.net/claude/tangsible/internal/config"
)

// LogFile is where the build's output goes, under the project's .tangsible
// directory.
const LogFile = "ee-build.log"

// Run is `tangsible ee build` (args are what follows "build"); it returns the
// exit code.
func Run(args []string) int {
	opts, err := ParseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n%s\n", err, Usage)
		return 2
	}
	return build(opts, os.Stdout, os.Stderr)
}

func build(opts Options, stdout, stderr io.Writer) int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "tangsible: "+format+"\n", a...)
		return 1
	}
	// The cheap refusals come before any work.
	if err := CheckFile(opts.File); err != nil {
		return fail("%v", err)
	}
	if err := CheckTools(); err != nil {
		return fail("%v", err)
	}
	text, err := os.ReadFile(opts.File)
	if err != nil {
		return fail("%s: %v", opts.File, errors.Unwrap(err))
	}
	tags := opts.Tags
	if len(tags) == 0 {
		if tags, err = Tags(text); err != nil {
			return fail("%s: %v", opts.File, err)
		}
	}
	if len(tags) == 0 {
		return fail("%s names no image: give one with -t, or set options.tags in the definition (tangsible ee init writes a commented tags stanza); ansible-builder would otherwise build ansible-execution-env:latest", opts.File)
	}
	settings := config.EEFromSettings(config.MergedSettingsConfig())

	logPath, logf, err := OpenLog(LogFile)
	if err != nil {
		return fail("%v", err)
	}
	defer logf.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	// What the names point at now, to report a replaced image.
	previous := map[string]string{}
	for _, t := range tags {
		if id, _, ok := ImageOf(t); ok {
			previous[t] = id
		}
	}
	before := TakeSnapshot()
	built := false
	// A successful build removes nothing: the image is the point. A failed or
	// interrupted one leaves containers and a "-tmp" image (Snapshot).
	defer func() { before.Cleanup("", built, true) }()

	fmt.Fprintf(stderr, "tangsible: building %s (this can take several minutes; output in %s) ...\n", opts.File, logPath)
	res := Result{LogPath: logPath}
	var buildErr error
	output, stopOutput := Output(logf, stderr, opts.ShowOutput)
	res.Duration, buildErr = Execute(ctx, Spec{File: opts.File, Tags: opts.Tags, Network: settings.Network, NoCache: opts.NoCache}, output)
	stopOutput()
	switch {
	case ctx.Err() != nil:
		res.Interrupted = true
	case buildErr != nil:
		res.BuildErr = buildErr.Error()
		res.BuildTail = TailOfFile(logPath, TailLines)
	default:
		built = true
		for _, t := range tags {
			im := Image{Tag: t}
			if id, size, ok := ImageOf(t); ok {
				im.ID, im.Size = id, size
				if prev := previous[t]; prev != "" && prev != id {
					im.Replaced = prev
				}
			}
			res.Images = append(res.Images, im)
		}
	}
	out, code := Render(res)
	fmt.Fprint(stdout, out)
	return code
}
