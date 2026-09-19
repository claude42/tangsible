// Copyright 2026 Klaus Wissmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import "strings"

// ParsedPassthroughArgs splits a tokenized ansible-playbook passthrough arg
// list (the same shape splitPlaybookArgs already produces) into the
// values the re-run dialog needs to pre-fill - Tags, SkipTags, and Hosts -
// plus everything else, carried forward verbatim and in original order.
// Not a general-purpose ansible-playbook CLI parser: only --tags/-t,
// --skip-tags, and --limit/-l are recognized, in "--flag value" and
// "--flag=value" form; an attached short form like "-tfoo" (no separator)
// isn't, and falls through to Rest untouched. Good enough for this
// project's own invocation patterns, not chased further - same
// "documented heuristic" style as TaskLabel's truncation or
// PrimaryOutputField's stdout-vs-msg choice.
type ParsedPassthroughArgs struct {
	Tags     string
	SkipTags string
	Hosts    string
	Rest     []string
}

// ParsePassthroughArgs parses args into a parsedPassthroughArgs. Multiple
// --tags/-t (or --skip-tags, or --limit/-l) occurrences are all collected
// and comma-joined into one Tags (or SkipTags, or Hosts) value - this
// doesn't attempt to replicate ansible-playbook's own per-flag
// merge-vs-override semantics, just gives a single value to show and edit
// in one dialog field.
func ParsePassthroughArgs(args []string) ParsedPassthroughArgs {
	var tags, skipTags, hosts []string
	var rest []string

	take := func(flag string, i int) (value string, consumed int, ok bool) {
		a := args[i]
		if a == flag {
			if i+1 < len(args) {
				return args[i+1], 2, true
			}
			return "", 0, false // dangling flag with no value - leave it in Rest untouched
		}
		if v, found := strings.CutPrefix(a, flag+"="); found {
			return v, 1, true
		}
		return "", 0, false
	}

	for i := 0; i < len(args); {
		matched := false
		for _, m := range []struct {
			flag string
			dst  *[]string
		}{
			{"--tags", &tags}, {"-t", &tags},
			{"--skip-tags", &skipTags},
			{"--limit", &hosts}, {"-l", &hosts},
		} {
			if v, n, ok := take(m.flag, i); ok {
				if v != "" {
					*m.dst = append(*m.dst, v)
				}
				i += n
				matched = true
				break
			}
		}
		if !matched {
			rest = append(rest, args[i])
			i++
		}
	}

	return ParsedPassthroughArgs{
		Tags:     strings.Join(tags, ","),
		SkipTags: strings.Join(skipTags, ","),
		Hosts:    strings.Join(hosts, ","),
		Rest:     rest,
	}
}

// ExtractStartAtPlay pulls a "--start-at-play NAME" or "--start-at-play=NAME"
// flag out of args - Tangsible's own synthetic flag
// (design-docs/StartWithPlay.md's "tangsible run --start-at-play" CLI form),
// understood by no real ansible-playbook and so never passed through to it,
// unlike every other passthrough flag. Only the long form is recognized -
// there's no short flag to alias, unlike --tags/-t or --limit/-l. Multiple
// occurrences all get removed; whichever is encountered last wins, the same
// "last one wins" convention most CLI tools apply to a repeated flag
// (deliberately not ParsePassthroughArgs's own "comma-join every value"
// convention - a play name isn't a set of values to combine). A dangling
// "--start-at-play" with no following value is left untouched in rest - the
// same "not chased further" gap ParsePassthroughArgs's own dangling-flag
// case documents, since ansible-playbook (never actually invoked with it,
// but still the closest available "what would normally happen here")
// treats an unrecognized bare flag as its own concern, not this function's.
// No occurrence at all returns ("", args) unmodified, not a copy.
func ExtractStartAtPlay(args []string) (startAtPlay string, rest []string) {
	const flag = "--start-at-play"
	found := false
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == flag && i+1 < len(args):
			startAtPlay, found = args[i+1], true
			i++
		case strings.HasPrefix(a, flag+"="):
			startAtPlay, found = strings.TrimPrefix(a, flag+"="), true
		default:
			out = append(out, a)
		}
	}
	if !found {
		return "", args
	}
	return startAtPlay, out
}

// RerunFlags bundles the three synthetic boolean flags design-docs/
// Rerun.md's "Extend rerun dialog" adds to the "rerun" verb - --only-failed,
// --only-unreachable, --resume-where-failed - mirroring their own dialog
// checkboxes one-for-one. Like --start-at-play (ExtractStartAtPlay), none
// of these is a real ansible-playbook flag, so all three are stripped by
// ExtractRerunFlags before anything reaches ansible-playbook itself.
type RerunFlags struct {
	OnlyFailed        bool
	OnlyUnreachable   bool
	ResumeWhereFailed bool
}

// ExtractRerunFlags pulls RerunFlags's three bare flags out of args,
// returning the remainder with all matching tokens removed - the same
// "synthetic flag, strip before ansible-playbook ever sees it" shape as
// ExtractStartAtPlay, simplified since these take no value: presence
// anywhere in args is enough, so (unlike ExtractStartAtPlay's own "last
// occurrence wins" for a value-bearing flag) a repeated flag is a no-op,
// not tracked specially. No occurrence of a given flag leaves its own
// RerunFlags field at its zero value (false).
func ExtractRerunFlags(args []string) (flags RerunFlags, rest []string) {
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--only-failed":
			flags.OnlyFailed = true
		case "--only-unreachable":
			flags.OnlyUnreachable = true
		case "--resume-where-failed":
			flags.ResumeWhereFailed = true
		default:
			out = append(out, a)
		}
	}
	return flags, out
}

// HasCheckFlag reports whether args contains a bare "--check" or "-C" -
// ansible-playbook's own dry-run flag, left in Rest by ParsePassthroughArgs
// (it isn't one of the flags that function extracts) and so still present
// verbatim wherever Rest itself ends up: the original invocation's own
// passthrough args, and - since a rerun's Rest is always carried forward
// unedited from that original invocation, never exposed in the rerun
// dialog - every later generation's too. Only recognizes the two exact
// tokens, not a value-bearing "--check=..." form (the real flag takes no
// value) and not an attached bundled short form like "-vC" - the same
// "documented heuristic, not chased further" gap ParsePassthroughArgs's
// own "-tfoo" case already accepts for the same reason.
func HasCheckFlag(args []string) bool {
	for _, a := range args {
		if a == "--check" || a == "-C" {
			return true
		}
	}
	return false
}

// HasInteractiveCredentialFlag reports whether args contains a bare
// "--ask-become-pass", "-K", "--ask-vault-pass", or "--ask-vault-password" -
// ansible-playbook's own flags for prompting for a become/vault password at
// bootstrap, before any callback event fires (Purpose.md/CLAUDE.md). That
// timing is fine for the real invocation, whose prompt reaches the terminal
// before the TUI ever takes it over - but replaying the same args to spawn a
// second, throwaway ansible-playbook process *while the TUI already owns the
// terminal* (see fetchRemoteFileContents) would trigger the same prompt
// fighting the raw screen instead, since neither password is cached to disk
// between processes. Callers use this to skip that second invocation
// entirely rather than risk it. Same "documented heuristic, exact tokens
// only, not chased further" gap as HasCheckFlag - no bundled short form
// (e.g. "-vK") is recognized, and none of these four flags takes a value to
// worry about.
func HasInteractiveCredentialFlag(args []string) bool {
	for _, a := range args {
		switch a {
		case "--ask-become-pass", "-K", "--ask-vault-pass", "--ask-vault-password":
			return true
		}
	}
	return false
}

// Reassemble rebuilds a full passthrough arg list from p - the inverse of
// parsePassthroughArgs, used once the re-run dialog's (possibly edited)
// Tags/SkipTags/Hosts need combining back with Rest. Always emits the
// long-form "--tags"/"--skip-tags"/"--limit" flags regardless of which
// form the original invocation used - round-tripping the exact original
// spelling isn't worth tracking separately.
func (p ParsedPassthroughArgs) Reassemble() []string {
	var out []string
	if p.Tags != "" {
		out = append(out, "--tags", p.Tags)
	}
	if p.SkipTags != "" {
		out = append(out, "--skip-tags", p.SkipTags)
	}
	if p.Hosts != "" {
		out = append(out, "--limit", p.Hosts)
	}
	out = append(out, p.Rest...)
	return out
}

// ArgsToHistoryString joins args into the single space-separated string
// stored in .tangsible's history - the same shape the user would type
// after the playbook name on the command line. Any argument containing
// whitespace, a quote character, a backslash, or that's empty is
// double-quoted (with internal '"'/'\' backslash-escaped) so
// historyStringToArgs can split it back out unambiguously; a plain token
// round-trips as-is, unquoted.
func ArgsToHistoryString(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = QuoteHistoryArg(a)
	}
	return strings.Join(parts, " ")
}

func QuoteHistoryArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"\\") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// HistoryStringToArgs splits a string previously produced by
// argsToHistoryString back into its original argument list: a minimal,
// POSIX-ish tokenizer supporting single/double-quoted spans (with
// backslash escaping only inside double quotes and unquoted text) - enough
// to round-trip anything argsToHistoryString itself produces, not a
// general shell parser (no variable expansion, no command substitution -
// neither is meaningful for a stored, literal argument list).
func HistoryStringToArgs(s string) []string {
	var args []string
	var cur strings.Builder
	hasCur := false
	inSingle, inDouble, escaped := false, false, false

	flush := func() {
		if hasCur {
			args = append(args, cur.String())
			cur.Reset()
			hasCur = false
		}
	}

	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
			hasCur = true
		case inSingle:
			if r == '\'' {
				inSingle = false
			} else {
				cur.WriteRune(r)
			}
		case inDouble:
			switch r {
			case '"':
				inDouble = false
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\'':
			inSingle, hasCur = true, true
		case r == '"':
			inDouble, hasCur = true, true
		case r == '\\':
			escaped, hasCur = true, true
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
			hasCur = true
		}
	}
	flush()
	return args
}
