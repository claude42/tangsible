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
	// NoBackup replaces an existing file without keeping its old content.
	NoBackup bool
}

// ParseArgs reads the arguments after "ee": the subcommand init, an
// optional playbook, --base <image>, --resolve-packages and --no-backup.
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
		case a == "--no-backup":
			o.NoBackup = true
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
const Usage = "usage: tangsible ee init [playbook.yml] [--base image] [--resolve-packages] [--no-backup]"

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
	// The cheap refusals come before any work. An existing file is replaced
	// (WriteDraft), unless it is not a regular file.
	out := filepath.Join(cwd, OutputFile)
	if err := CheckOutputPath(out); err != nil {
		return err
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
	// The file operations come last, after all the slow work: an interrupt
	// before this point touches nothing.
	res, err := WriteDraft(out, []byte(text), !opts.NoBackup)
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(stderr, "tangsible: %s\n", w)
	}
	// An empty line on stderr, so the summary is set off from the progress
	// lines above it on a terminal (and stdout stays just the summary).
	fmt.Fprintln(stderr)
	fmt.Fprint(stdout, Summary(res, rep, opts.NoBackup))
	return nil
}

// Summary is what `ee init` prints at the end, in blocks separated by an empty
// line: what happened to the file; what the draft holds and needs (collections,
// implied packages, missing binaries); and how many TODOs are left in it. A
// block with nothing to say is left out.
func Summary(res WriteResult, rep Report, noBackup bool) string {
	var file strings.Builder
	switch {
	case res.Unchanged:
		fmt.Fprintf(&file, "%s is unchanged.", OutputFile)
	default:
		fmt.Fprintf(&file, "Created %s.", OutputFile)
		switch {
		case res.Backup != "":
			fmt.Fprintf(&file, " The previous file is saved as %s.", res.Backup)
		case res.Replaced && noBackup:
			file.WriteString(" The previous file was replaced without a backup (--no-backup).")
		}
		if len(res.Pruned) > 0 {
			fmt.Fprintf(&file, "\nRemoved the oldest backup(s): %s.", strings.Join(res.Pruned, ", "))
		}
	}
	blocks := []string{file.String()}

	var needs []string
	if rep.Collections > 0 {
		needs = append(needs, fmt.Sprintf("%d collection(s) (%d pinned from this host).", rep.Collections, rep.Pinned))
	}
	if rep.Implied > 0 {
		needs = append(needs, fmt.Sprintf("%d package(s) implied by lookups or modules; see the comment in the draft.", rep.Implied))
	}
	switch {
	case rep.Binaries == 1:
		needs = append(needs, "1 binary used on the controller is missing from the base image; see the comment in the draft.")
	case rep.Binaries > 1:
		needs = append(needs, fmt.Sprintf("%d binaries used on the controller are missing from the base image; see the comment in the draft.", rep.Binaries))
	}
	if len(needs) > 0 {
		blocks = append(blocks, strings.Join(needs, "\n"))
	}
	if rep.Todos > 0 {
		blocks = append(blocks, fmt.Sprintf("%d TODO(s) to resolve in this file.", rep.Todos))
	}
	return strings.Join(blocks, "\n\n") + "\n"
}
