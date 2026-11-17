package eeverify

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"code.aw.net/claude/tangsible/internal/eeinit"
)

// Missing is something the playbook uses that the built image lacks.
type Missing struct {
	Name string
	Locs []string
}

// Check is the outcome of one kind of check.
type Check struct {
	// Total is how many different things the playbook uses of this kind.
	Total   int
	Missing []Missing
}

// ModuleNames are the names to ask the image's ansible-doc about so that
// every module use can be judged (see ModuleCheck).
func ModuleNames(uses []eeinit.ModuleUse) []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range uses {
		for _, n := range eeinit.CheckNames(u) {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// LookupNames is ModuleNames for lookups.
func LookupNames(uses []eeinit.LookupUse) []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range uses {
		for _, n := range eeinit.LookupCheckNames(u) {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ModuleCheck judges every module the playbook uses: it is found if the
// image's ansible-doc knows any of its names. A module is one name as written,
// whatever the number of tasks using it.
func ModuleCheck(uses []eeinit.ModuleUse, found map[string]eeinit.PluginDoc) Check {
	locs := map[string][]string{}
	ok := map[string]bool{}
	for _, u := range uses {
		locs[u.Module] = append(locs[u.Module], u.Loc)
		for _, n := range eeinit.CheckNames(u) {
			if _, known := found[n]; known {
				ok[u.Module] = true
			}
		}
	}
	return judge(locs, ok)
}

// LookupCheck is ModuleCheck for lookups.
func LookupCheck(uses []eeinit.LookupUse, found map[string]eeinit.PluginDoc) Check {
	locs := map[string][]string{}
	ok := map[string]bool{}
	for _, u := range uses {
		locs[u.Name] = append(locs[u.Name], u.Loc)
		for _, n := range eeinit.LookupCheckNames(u) {
			if _, known := found[n]; known {
				ok[u.Name] = true
			}
		}
	}
	return judge(locs, ok)
}

// BinaryCheck judges the controller-side binaries: missing are the ones the
// image lacks.
func BinaryCheck(uses []eeinit.BinaryUse, missing []string) Check {
	c := Check{Total: len(uses)}
	for _, u := range uses {
		for _, m := range missing {
			if m == u.Name {
				c.Missing = append(c.Missing, Missing{u.Name, u.Locs})
				break
			}
		}
	}
	return c
}

func judge(locs map[string][]string, ok map[string]bool) Check {
	c := Check{Total: len(locs)}
	for name, l := range locs {
		if !ok[name] {
			c.Missing = append(c.Missing, Missing{name, dedupe(l)})
		}
	}
	sort.Slice(c.Missing, func(i, j int) bool { return c.Missing[i].Name < c.Missing[j].Name })
	return c
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Result is everything a verification found.
type Result struct {
	File string
	// BuildErr is "" when the image was built; BuildTail the last lines of
	// the build's output, LogPath where all of it is.
	BuildDuration time.Duration
	BuildErr      string
	BuildTail     []string
	LogPath       string

	// SyntaxRan/SyntaxOK/SyntaxOutput: the ansible-playbook --syntax-check.
	SyntaxRan    bool
	SyntaxOK     bool
	SyntaxOutput string

	// Checked is false when the plugin and binary check couldn't run (the
	// build failed, or CheckErr says why not).
	Checked  bool
	CheckErr string
	Modules  Check
	Lookups  Check
	Binaries Check

	// Tag is the image's name, Kept says it was left in place.
	Tag  string
	Kept bool
	// Interrupted: the user stopped the run.
	Interrupted bool
}

// maxOutputLines bounds how much of a failing command's output is shown.
const maxOutputLines = 15

// Render is the report and the exit code: 0 when every check passed, 1 when
// the build or any check failed, 130 when interrupted.
func Render(r Result) (string, int) {
	var b strings.Builder
	line := func(label, text string) { fmt.Fprintf(&b, "%-16s %s\n", label+":", text) }
	failed := false

	if r.Interrupted {
		b.WriteString("Interrupted.\n")
		return b.String(), 130
	}
	if r.BuildErr != "" {
		failed = true
		line("Build", "FAILED ("+r.BuildErr+")")
		for _, l := range lastLines(r.BuildTail, maxOutputLines) {
			fmt.Fprintf(&b, "  %s\n", l)
		}
		if r.LogPath != "" {
			fmt.Fprintf(&b, "  full log: %s\n", r.LogPath)
		}
		line("Syntax check", "skipped (the build failed)")
		line("Modules", "skipped")
		line("Lookups", "skipped")
		line("Binaries", "skipped")
		return b.String(), 1
	}
	line("Build", "ok ("+r.BuildDuration.Round(time.Second).String()+")")

	switch {
	case !r.SyntaxRan:
		line("Syntax check", "not run")
	case r.SyntaxOK:
		line("Syntax check", "ok")
	default:
		failed = true
		line("Syntax check", "FAILED")
		for _, l := range lastLines(strings.Split(strings.TrimRight(r.SyntaxOutput, "\n"), "\n"), maxOutputLines) {
			fmt.Fprintf(&b, "  %s\n", l)
		}
	}

	if !r.Checked {
		failed = true
		msg := "could not run"
		if r.CheckErr != "" {
			msg += ": " + r.CheckErr
		}
		line("Modules", msg)
		line("Lookups", msg)
		line("Binaries", msg)
	} else {
		if report(&b, "Modules", r.Modules, "found in the image", "not found", "add the collection that provides it to the definition") {
			failed = true
		}
		if report(&b, "Lookups", r.Lookups, "found in the image", "not found", "add the collection that provides it to the definition") {
			failed = true
		}
		if report(&b, "Binaries", r.Binaries, "found in the image", "missing", "add it under dependencies.system (see ee init --resolve-packages)") {
			failed = true
		}
	}
	if r.Kept && r.Tag != "" {
		fmt.Fprintf(&b, "Image kept: %s\n", r.Tag)
	}
	if failed {
		return b.String(), 1
	}
	return b.String(), 0
}

// report writes one kind of check and reports whether it found anything
// missing.
func report(b *strings.Builder, label string, c Check, foundText, missingWord, hint string) bool {
	if c.Total == 0 {
		fmt.Fprintf(b, "%-16s none used by the playbook\n", label+":")
		return false
	}
	fmt.Fprintf(b, "%-16s %d of %d %s\n", label+":", c.Total-len(c.Missing), c.Total, foundText)
	for _, m := range c.Missing {
		text := m.Name
		if len(m.Locs) > 0 {
			text += " (" + locsText(m.Locs) + ")"
		}
		fmt.Fprintf(b, "  %s: %s\n    -> %s\n", missingWord, text, hint)
	}
	return len(c.Missing) > 0
}

func locsText(locs []string) string {
	shown := locs
	if len(shown) > 2 {
		shown = shown[:2]
	}
	out := strings.Join(shown, ", ")
	if extra := len(locs) - len(shown); extra > 0 {
		out += fmt.Sprintf(", +%d more", extra)
	}
	return out
}

func lastLines(lines []string, n int) []string {
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}
