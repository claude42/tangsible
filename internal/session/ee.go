package session

import (
	"fmt"
	"os"

	"code.aw.net/claude/tangsible/internal/config"
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
	if err := runner.EnsureImage(os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tangsible:", err)
		os.Exit(1)
	}
}

// rejectExecutionEnvironmentForDebug keeps debug on the host for now
// (design-docs/ExecutionEnvironments.md, "Debug mode in an EE"): an
// explicit --ee is a usage error, and so is a configured image unless
// --no-ee says the host run is intended - silently ignoring the config
// would defeat its purpose. It returns args without --no-ee.
func rejectExecutionEnvironmentForDebug(args []string) []string {
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
	if !o.Disable && config.ResolveEE(config.MergedSettingsConfig(), o) != nil {
		fmt.Fprintln(os.Stderr, "tangsible: debug does not support execution environments yet; an image is configured in [execution_environment] - pass --no-ee to debug on the host")
		os.Exit(2)
	}
	return rest
}
