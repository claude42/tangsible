package eeverify

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"code.aw.net/claude/tangsible/internal/eeinit"
)

// Names are what to look for in the built image.
type Names struct {
	// Modules and Lookups are plugin names for the image's ansible-doc (the
	// candidates, see eeinit.CheckNames); Binaries are names or absolute paths.
	Modules, Lookups, Binaries []string
}

// safeName is what may be written into the check script: it is embedded in
// single quotes, so nothing else (a quote above all) may get in.
var safeName = regexp.MustCompile(`^(/|[A-Za-z0-9])[A-Za-z0-9_.+/-]*$`)

// docChunk bounds how many names one ansible-doc call is asked about.
const docChunk = 60

// CheckScript is the sh script to run in the built image. It prints one
// section per kind of check - @@modules and @@lookups with ansible-doc's JSON
// (one object per call), @@binaries with a missing=<name> line for each binary
// the image lacks - or @@noansibledoc when the image has no ansible-doc to
// ask. The names are written into the script (validated, quoted), not passed
// as arguments, so one container run does everything.
func CheckScript(n Names) string {
	var b strings.Builder
	b.WriteString("command -v ansible-doc >/dev/null 2>&1 || { echo @@noansibledoc; exit 0; }\n")
	for _, k := range []struct {
		marker, kind string
		names        []string
	}{
		{"@@modules", "module", n.Modules},
		{"@@lookups", "lookup", n.Lookups},
	} {
		b.WriteString("echo " + k.marker + "\n")
		names := safe(k.names)
		for len(names) > 0 {
			c := min(docChunk, len(names))
			b.WriteString("ansible-doc -t " + k.kind + " -j " + quote(names[:c]) + " 2>/dev/null\n")
			names = names[c:]
		}
	}
	b.WriteString("echo @@binaries\n")
	if bins := safe(n.Binaries); len(bins) > 0 {
		b.WriteString("for n in " + quote(bins) + `; do
  case "$n" in
    /*) [ -x "$n" ] || echo "missing=$n" ;;
    *) command -v "$n" >/dev/null 2>&1 || echo "missing=$n" ;;
  esac
done
`)
	}
	return b.String()
}

func safe(names []string) []string {
	var out []string
	for _, n := range names {
		if safeName.MatchString(n) {
			out = append(out, n)
		}
	}
	return out
}

func quote(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = "'" + n + "'"
	}
	return strings.Join(q, " ")
}

// Checked is what CheckScript's output says is in the image.
type Checked struct {
	// Modules and Lookups map a name the image's ansible-doc knows to its
	// documentation.
	Modules, Lookups map[string]eeinit.PluginDoc
	// MissingBinaries are the binaries the image lacks.
	MissingBinaries []string
}

// ParseCheck reads CheckScript's output.
func ParseCheck(out string) (Checked, error) {
	c := Checked{Modules: map[string]eeinit.PluginDoc{}, Lookups: map[string]eeinit.PluginDoc{}}
	if strings.Contains(out, "@@noansibledoc") {
		return c, errors.New("the image has no ansible-doc to ask about modules and lookups")
	}
	mi, li, bi := strings.Index(out, "@@modules\n"), strings.Index(out, "@@lookups\n"), strings.Index(out, "@@binaries\n")
	if mi < 0 || li < mi || bi < li {
		return c, errors.New("unexpected output of the check in the image")
	}
	section := func(start int, marker string, end int) string { return out[start+len(marker) : end] }
	for _, s := range []struct {
		text string
		into map[string]eeinit.PluginDoc
	}{
		{section(mi, "@@modules\n", li), c.Modules},
		{section(li, "@@lookups\n", bi), c.Lookups},
	} {
		dec := json.NewDecoder(strings.NewReader(s.text))
		for dec.More() {
			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				break // a truncated or odd tail: what was read so far counts
			}
			for name, d := range eeinit.ParseDocs(raw) {
				s.into[name] = d
			}
		}
	}
	for _, line := range strings.Split(out[bi+len("@@binaries\n"):], "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "missing="); ok && name != "" {
			c.MissingBinaries = append(c.MissingBinaries, name)
		}
	}
	return c, nil
}
