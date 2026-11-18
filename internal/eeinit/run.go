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
)

// OutputFile is where the draft goes, in the working directory.
const OutputFile = "execution-environment.yml"

// Options is the parsed command line of `tangsible ee init`.
type Options struct {
	Playbook string // "" means resolve it like run does
	Base     string
	// ResolvePackages looks up the packages for controller-side binaries
	// the base image lacks (it downloads repository metadata).
	ResolvePackages bool
}

// ParseArgs reads the arguments after "ee": the subcommand init, an
// optional playbook, --base <image> and --resolve-packages.
func ParseArgs(args []string) (Options, error) {
	var o Options
	if len(args) == 0 || args[0] != "init" {
		return o, errors.New("expected the subcommand init, verify or build")
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
		case a == "--resolve-packages":
			o.ResolvePackages = true
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

// Usage is the synopsis printed with a usage error.
const Usage = "usage: tangsible ee init [playbook.yml] [--base image] [--resolve-packages]"

// RunEEVerb is `tangsible ee init ...`; it returns the exit code. (ee verify
// is internal/eeverify's.)
func RunEEVerb(args []string) int {
	opts, err := ParseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: %v\n%s\n", err, Usage)
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

	// What the playbook uses.
	proj, err := ScanProject(playbook, cwd)
	if err != nil {
		return err
	}
	abs := proj.Abs
	uses, todos, commands, lookups, roles := proj.Uses, proj.Todos, proj.Commands, proj.Lookups, proj.Roles
	templatedLookups := proj.TemplatedLookups

	// The base image, asked about the binaries controller-side tasks run.
	binaries, templatedCommands := ScanBinaries(commands, filepath.Dir(abs))
	names := make([]string, len(binaries))
	for i, b := range binaries {
		names[i] = b.Name
	}
	facts, err := ProbeBase(settings.Engine, base, names)
	if err != nil {
		return err
	}
	var missing []BinaryNeed
	for _, b := range binaries {
		if contains(facts.Missing, b.Name) {
			missing = append(missing, BinaryNeed{Name: b.Name, Locs: b.Locs})
		}
	}
	var packagesFailed string
	if opts.ResolvePackages && len(missing) > 0 {
		fmt.Fprintf(stderr, "tangsible: looking up packages for %d binaries in the base image (this downloads repository metadata and can take a minute) ...\n", len(missing))
		missingNames := make([]string, len(missing))
		for i, b := range missing {
			missingNames[i] = b.Name
		}
		pkgs, err := ResolvePackages(settings.Engine, base, settings.Network, facts.Family(), missingNames)
		if err != nil {
			packagesFailed = err.Error()
			fmt.Fprintf(stderr, "tangsible: package lookup failed: %v\n", err)
		}
		for i := range missing {
			missing[i].Packages = pkgs[missing[i].Name]
		}
	}
	moduleDocs, err := LookupPlugins("module", UniqueCandidates(uses))
	if err != nil {
		return err
	}
	found := collectionsOfDocs(moduleDocs)
	needed, unresolved := Resolve(uses, roles, found)
	todos = append(todos, unresolved...)
	plugins := ModulePlugins(uses, found)

	// Lookups: their collections join the needed ones, and the rules look at
	// them and at the modules.
	lookupDocs, err := LookupPlugins("lookup", LookupCandidates(lookups))
	if err != nil {
		return err
	}
	lookupNeeded, lookupPlugins, lookupTodos := ResolveLookups(lookups, lookupDocs)
	needed = MergeNeeded(needed, lookupNeeded)
	todos = append(todos, lookupTodos...)
	plugins = append(plugins, lookupPlugins...)

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
		ImageTag:         SuggestTag(cwd),

		Binaries:          missing,
		ResolvePackages:   opts.ResolvePackages,
		PackagesFailed:    packagesFailed,
		TemplatedCommands: templatedCommands,

		Implied:          ImpliedPackages(plugins),
		TemplatedLookups: templatedLookups,
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
	if rep.Implied > 0 {
		fmt.Fprintf(stdout, "%d package(s) implied by lookups or modules; see the comment in the draft.\n", rep.Implied)
	}
	if rep.Binaries > 0 {
		if rep.Binaries == 1 {
			fmt.Fprintln(stdout, "1 binary used on the controller is missing from the base image; see the comment in the draft.")
		} else {
			fmt.Fprintf(stdout, "%d binaries used on the controller are missing from the base image; see the comment in the draft.\n", rep.Binaries)
		}
	}
	return nil
}
