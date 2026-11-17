package eeinit

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/execerr"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/source"
)

// OutputFile is where the draft goes, in the working directory.
const OutputFile = "execution-environment.yml"

// Options is the parsed command line of `tangsible ee init`.
type Options struct {
	Playbook string // "" means resolve it like run does
	Base     string
}

// ParseArgs reads the arguments after "ee": the subcommand init, an
// optional playbook and --base <image>.
func ParseArgs(args []string) (Options, error) {
	var o Options
	if len(args) == 0 || args[0] != "init" {
		return o, errors.New("expected the subcommand init")
	}
	args = args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--base":
			if i+1 >= len(args) {
				return o, errors.New("--base needs an image")
			}
			i++
			o.Base = args[i]
		case strings.HasPrefix(a, "--base="):
			o.Base = strings.TrimPrefix(a, "--base=")
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown option %s", a)
		case o.Playbook != "":
			return o, fmt.Errorf("unexpected argument %s", a)
		default:
			o.Playbook = a
		}
	}
	if strings.HasPrefix(o.Base, "-") || (o.Base == "" && containsBaseFlag(args)) {
		return o, errors.New("--base needs an image")
	}
	return o, nil
}

func containsBaseFlag(args []string) bool {
	for _, a := range args {
		if a == "--base" || strings.HasPrefix(a, "--base=") {
			return true
		}
	}
	return false
}

// RunEEVerb is `tangsible ee ...`; it returns the exit code.
func RunEEVerb(args []string) int {
	opts, err := ParseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n%s\n", err, config.UsageLine(config.VerbEE))
		return 2
	}
	if err := runInit(opts, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n", err)
		return 1
	}
	return 0
}

func runInit(opts Options, stdout, stderr io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// The cheap refusals come before any work.
	out := filepath.Join(cwd, OutputFile)
	if _, err := os.Lstat(out); err == nil {
		return fmt.Errorf("%s already exists; not overwriting it", OutputFile)
	}
	playbook := opts.Playbook
	if playbook == "" {
		var src string
		if playbook, src = config.ResolvePlaybook(); playbook == "" {
			return errors.New("no playbook given, and no default found (TANGSIBLE_PLAYBOOK, default_playbook, ./site.yml)")
		} else {
			fmt.Fprintf(stderr, "tangsible: using %s from %s\n", playbook, src)
		}
	}
	if _, err := os.Stat(playbook); err != nil {
		return fmt.Errorf("playbook %s: %w", playbook, errors.Unwrap(err))
	}
	if _, err := exec.LookPath("podman"); err != nil {
		if msg := execerr.NotFoundMessage("podman", err); msg != "" {
			return errors.New(msg)
		}
		return err
	}

	settings := config.EEFromSettings(config.MergedSettingsConfig())
	base, isDefault := ResolveBase(opts.Base, settings.BaseImage)
	fmt.Fprintf(stderr, "tangsible: base image %s\n", base)

	// The image is pulled by the same policy a run would use, then
	// inspected.
	settings.Image = base
	runner.SetExecutionEnvironment(&settings)
	if err := runner.EnsureImage(stderr); err != nil {
		return err
	}
	facts, err := ProbeBase(settings.Engine, base)
	if err != nil {
		return err
	}

	// What the playbook uses.
	abs, err := filepath.Abs(playbook)
	if err != nil {
		return err
	}
	var uses []ModuleUse
	var todos []Todo
	var roles []RoleUse
	seenRole := map[RoleUse]bool{}
	for _, f := range source.ReachableYAML(abs) {
		name := filepath.Base(f.Path)
		if name == "requirements.yml" || name == "requirements.yaml" || name == OutputFile {
			continue
		}
		data, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		var own []string
		if f.Collection != "" {
			own = []string{f.Collection}
			if r := (RoleUse{f.Role, f.Collection}); !seenRole[r] {
				seenRole[r] = true
				roles = append(roles, r)
			}
		}
		scan := ScanYAML(displayPath(f.Path, cwd), data, own)
		uses = append(uses, scan.Uses...)
		todos = append(todos, scan.Todos...)
	}
	found, err := LookupCollections(UniqueCandidates(uses))
	if err != nil {
		return err
	}
	needed, unresolved := Resolve(uses, roles, found)
	todos = append(todos, unresolved...)

	installed, err := InstalledCollections()
	if err != nil {
		fmt.Fprintf(stderr, "tangsible: no versions to pin: %v\n", err)
	}
	req, reqPath, err := FindRequirements(filepath.Dir(abs), cwd)
	if err != nil {
		return err
	}

	text, rep := Draft(Inputs{
		Playbook:         displayPath(abs, cwd),
		Base:             base,
		BaseIsDefault:    isDefault,
		Facts:            facts,
		Needed:           needed,
		Todos:            todos,
		Requirements:     req,
		RequirementsPath: reqPath,
		Installed:        installed,
	})
	// O_EXCL: a file that appeared meanwhile is not overwritten either.
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; not overwriting it", OutputFile)
		}
		return err
	}
	if _, err := io.WriteString(f, text); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Wrote %s: %d collection(s) (%d pinned from this host), %d TODO(s).\n", OutputFile, rep.Collections, rep.Pinned, rep.Todos)
	return nil
}
