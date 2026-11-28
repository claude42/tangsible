package session

import (
	"fmt"
	"os"

	"code.aw.net/claude/tangsible/internal/config"
	eepkg "code.aw.net/claude/tangsible/internal/ee"
	"code.aw.net/claude/tangsible/internal/runner"
)

// setupExecutionEnvironment is run/rerun/role's startup step for
// design-docs/ExecutionEnvironments.md: it strips --ee/--no-ee from args
// (Tangsible's own synthetic flags, never part of recorded or replayed
// passthrough), resolves them against [execution_environment], activates
// the result for every generation this process spawns, and makes sure the
// image is present - pulling it now, before any TUI owns the terminal.
// Exits on a usage or pull error, like the surrounding startup code.
func setupExecutionEnvironment(v config.Verb, args []string) []string {
	o, rest := extractEEFlags(v, args)
	activateExecutionEnvironment(o, "")
	return rest
}

// extractEEFlags strips --ee/--no-ee from args, exiting on a usage error.
func extractEEFlags(v config.Verb, args []string) (config.EEOverride, []string) {
	o, rest, err := config.ExtractEEFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, config.UsageLine(v))
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	return o, rest
}

// activateExecutionEnvironment resolves o against the configuration (and,
// for a rerun, what the earlier run recorded), makes the result the one
// every generation spawns with, and ensures its image is present.
func activateExecutionEnvironment(o config.EEOverride, recorded string) {
	s := config.ResolveEEWithHistory(config.MergedSettingsConfig(), o, recorded)
	runner.SetExecutionEnvironment(s)
	warnings, err := runner.ValidateExecutionEnvironment()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tangsible:", err)
		fmt.Fprintln(os.Stderr, "tangsible: hint: [execution_environment] extra_mounts entries read SOURCE:DESTINATION[:OPTIONS], OPTIONS a comma-separated list of ro, rw, z, Z, O (~ and $VAR are expanded); pass_env lists variable names or AWS_*-style prefixes, set_env is a table of NAME = \"value\"")
		os.Exit(2)
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "tangsible: warning:", w)
	}
	if err := runner.EnsureImage(os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tangsible:", err)
		os.Exit(1)
	}
}

// debugExecutionEnvironment keeps debug on the host for now
// (design-docs/ExecutionEnvironments.md, "Debug mode in an EE"): an
// explicit --ee is a usage error, but an image that is only configured in
// [execution_environment] is ignored - the config says where playbooks run,
// debug is a separate activity. It returns args without --no-ee, and the
// configured image's short name when one was ignored (empty otherwise, also
// for an explicit --no-ee) so the top bar can say debug is not in it.
func debugExecutionEnvironment(args []string) (rest []string, ignored string) {
	o, rest, err := config.ExtractEEFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, config.UsageLine(config.VerbDebug))
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if o.Image != "" {
		fmt.Fprintln(os.Stderr, "tangsible: debug does not support execution environments yet (--ee)")
		os.Exit(2)
	}
	if !o.Disable {
		if ee := config.ResolveEE(config.MergedSettingsConfig(), o); ee != nil {
			ignored = eepkg.ShortImageName(ee.Image)
		}
	}
	return rest, ignored
}
