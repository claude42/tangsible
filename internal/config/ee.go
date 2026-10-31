package config

import (
	"errors"
	"strings"
)

// EESettings is the resolved [execution_environment] table
// (design-docs/ExecutionEnvironments.md) with every default applied. Image
// "" means no execution environment is configured.
type EESettings struct {
	Image       string
	Engine      string
	Network     string
	SELinux     string // "", "disable", "z", "Z" or "none"; "" is auto
	ReadOnly    bool
	SSHAgent    bool
	SSHDir      bool
	ExtraMounts []string
}

// eeEngines and eeSELinuxModes are the accepted values of the two
// fixed-choice keys; "" (unset) is always fine.
var (
	eeEngines      = []string{"podman"}
	eeSELinuxModes = []string{"auto", "disable", "z", "Z", "none"}
)

func mergeEE(merged *SettingsConfig, tiers ...SettingsConfig) {
	m := &merged.ExecutionEnvironment
	for _, t := range tiers {
		e := t.ExecutionEnvironment
		m.Image = firstNonEmpty(m.Image, e.Image)
		m.Engine = firstNonEmpty(m.Engine, e.Engine)
		m.Network = firstNonEmpty(m.Network, e.Network)
		m.SELinux = firstNonEmpty(m.SELinux, e.SELinux)
		m.ReadOnly = firstNonNil(m.ReadOnly, e.ReadOnly)
		m.SSHAgent = firstNonNil(m.SSHAgent, e.SSHAgent)
		m.SSHDir = firstNonNil(m.SSHDir, e.SSHDir)
		if len(m.ExtraMounts) == 0 {
			m.ExtraMounts = e.ExtraMounts
		}
	}
}

// EEFromSettings reads cfg's [execution_environment] with defaults
// applied: podman, auto SELinux, project mounted read-write, ssh agent and
// ~/.ssh forwarded. An invalid engine or selinux value falls back to its
// default (settingswarn.go reports it).
func EEFromSettings(cfg SettingsConfig) EESettings {
	e := cfg.ExecutionEnvironment
	s := EESettings{
		Image:       strings.TrimSpace(e.Image),
		Engine:      "podman",
		Network:     e.Network,
		ReadOnly:    e.ReadOnly != nil && *e.ReadOnly,
		SSHAgent:    e.SSHAgent == nil || *e.SSHAgent,
		SSHDir:      e.SSHDir == nil || *e.SSHDir,
		ExtraMounts: e.ExtraMounts,
	}
	if eng := strings.ToLower(e.Engine); containsString(eeEngines, eng) {
		s.Engine = eng
	}
	if containsString(eeSELinuxModes, e.SELinux) && e.SELinux != "auto" {
		s.SELinux = e.SELinux
	}
	return s
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// EEOverride is an explicit --ee <image> / --no-ee choice from the command
// line (design-docs/ExecutionEnvironments.md): Image forces that image,
// Disable forces a host run even when the config names an image. The zero
// value means neither was given.
type EEOverride struct {
	Image   string
	Disable bool
}

// HasEEFlag reports whether args contains --ee or --no-ee, for the verbs
// that don't support them and reject them with a usage error.
func HasEEFlag(args []string) bool {
	for _, a := range args {
		if a == "--no-ee" || a == "--ee" || strings.HasPrefix(a, "--ee=") {
			return true
		}
	}
	return false
}

// ExtractEEFlags pulls --ee <image> / --ee=<image> and --no-ee out of args,
// like ExtractDialogFlag: both are Tangsible's own synthetic flags, never
// part of the passthrough that is recorded in history and replayed at
// ansible-playbook. A repeated --ee keeps the last image. err is non-nil
// for --ee without a value or for --ee together with --no-ee; rest is then
// args unchanged.
func ExtractEEFlags(args []string) (o EEOverride, rest []string, err error) {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--no-ee":
			o.Disable = true
		case a == "--ee":
			if i+1 >= len(args) || args[i+1] == "" {
				return EEOverride{}, args, errors.New("--ee needs an image")
			}
			i++
			o.Image = args[i]
		case strings.HasPrefix(a, "--ee="):
			if a == "--ee=" {
				return EEOverride{}, args, errors.New("--ee needs an image")
			}
			o.Image = strings.TrimPrefix(a, "--ee=")
		default:
			out = append(out, a)
		}
	}
	if o.Disable && o.Image != "" {
		return EEOverride{}, args, errors.New("--ee and --no-ee cannot be combined")
	}
	if !o.Disable && o.Image == "" {
		return o, args, nil
	}
	return o, out, nil
}

// ResolveEE applies an override on top of the configured settings: nil
// means a plain host run.
func ResolveEE(cfg SettingsConfig, o EEOverride) *EESettings {
	if o.Disable {
		return nil
	}
	s := EEFromSettings(cfg)
	if o.Image != "" {
		s.Image = o.Image
	}
	if s.Image == "" {
		return nil
	}
	return &s
}

// QueueWarning queues a message for PrintPendingWarnings - for code that
// runs while the TUI owns the terminal and so can't print.
func QueueWarning(msg string) { addPendingWarnings([]string{msg}) }
