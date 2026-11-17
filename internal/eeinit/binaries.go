package eeinit

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// BinaryUse is a binary a controller-side command/shell task runs, with where.
type BinaryUse struct {
	Name string
	Locs []string
}

// binaryName is what a binary can be called: a plain name, or an absolute
// path. Anything else (a relative path, a template) is not a binary to look
// for in the image.
var (
	binaryName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	binaryPath = regexp.MustCompile(`^/[A-Za-z0-9._+/-]+$`)
)

// ScanBinaries finds the binaries cmds run: the first word of every simple
// command (see commandBinaries). Absolute paths under projectDir are the
// project's own scripts and are skipped. templated counts the commands whose
// binary can't be determined (a template, eval, a variable as command).
func ScanBinaries(cmds []Command, projectDir string) (uses []BinaryUse, templated int) {
	locs := map[string][]string{}
	for _, c := range cmds {
		names, tmpl := commandBinaries(c)
		if tmpl {
			templated++
		}
		for _, n := range names {
			if strings.HasPrefix(n, "/") && projectDir != "" && isUnder(n, projectDir) {
				continue
			}
			if !contains(locs[n], c.Loc) {
				locs[n] = append(locs[n], c.Loc)
			}
		}
	}
	for n, l := range locs {
		uses = append(uses, BinaryUse{Name: n, Locs: l})
	}
	sort.Slice(uses, func(i, j int) bool { return uses[i].Name < uses[j].Name })
	return uses, templated
}

func isUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// commandBinaries returns the binaries one command runs, and whether some
// could not be determined.
func commandBinaries(c Command) (names []string, templated bool) {
	add := func(w string) {
		switch kind := classify(w); kind {
		case wordBinary:
			names = append(names, w)
		case wordTemplated:
			templated = true
		}
	}
	if c.Executable != "" {
		add(c.Executable)
	}
	switch {
	case len(c.Argv) > 0:
		add(c.Argv[0])
	case c.Text != "" && !c.Shell:
		// command: no shell, the first word is the binary
		if words := splitWords(c.Text); len(words) > 0 {
			add(words[0])
		}
	case c.Text != "":
		for _, seg := range shellSegments(stripHeredocs(c.Text)) {
			switch name, kind := segmentCommand(splitWords(seg)); kind {
			case wordBinary:
				add(name)
			case wordTemplated:
				templated = true
			}
		}
	}
	return names, templated
}

type wordKind int

const (
	wordNone      wordKind = iota // nothing to look for (a builtin, a project path)
	wordBinary                    // a binary to look for
	wordTemplated                 // can't be determined
)

// classify says what a command word is.
func classify(w string) wordKind {
	switch {
	case w == "":
		return wordNone
	case strings.Contains(w, "{{") || strings.Contains(w, "{%") || strings.HasPrefix(w, "$") || strings.ContainsAny(w, "`"):
		return wordTemplated
	case binaryName.MatchString(w) || binaryPath.MatchString(w):
		return wordBinary
	}
	return wordNone
}

// shellWords are builtins and keywords that are never a binary to install.
var shellWords = map[string]bool{}

// shellSkipSegment are words that make a whole simple command uninteresting.
var shellSkipSegment = map[string]bool{
	"for": true, "case": true, "select": true, "function": true, "done": true,
	"fi": true, "esac": true, "}": true, ")": true, "in": true,
}

// shellPrefix are words after which the real command follows.
var shellPrefix = map[string]bool{
	"!": true, "if": true, "then": true, "elif": true, "else": true, "do": true,
	"while": true, "until": true, "time": true, "nohup": true, "exec": true,
	"{": true, "(": true,
}

func init() {
	for _, w := range strings.Fields(`cd echo printf test [ [[ export set unset read source . : true false exit
		return local declare typeset readonly alias unalias type hash ulimit umask wait trap shift break
		continue let getopts pwd pushd popd dirs kill jobs fg bg builtin enable help history mapfile
		readarray fc bind caller compgen complete shopt`) {
		shellWords[w] = true
	}
}

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// sudoArgOptions are the sudo options that take a separate argument.
var sudoArgOptions = map[string]bool{"-u": true, "-g": true, "-h": true, "-p": true, "-C": true, "-D": true, "-R": true, "-T": true, "-U": true, "-r": true, "-t": true}

// segmentCommand finds the command a simple command (already split into
// words) runs: after assignments, redirections, keywords and the wrappers
// sudo/env/command.
func segmentCommand(words []string) (string, wordKind) {
	for len(words) > 0 {
		w := words[0]
		// a leading ( or { glued to the word: "(cd x"
		for len(w) > 1 && (w[0] == '(' || (w[0] == '{' && w[1] != '{' && w[1] != '%')) {
			w = w[1:]
		}
		switch {
		case assignment.MatchString(w):
			words = words[1:]
		case redirection.MatchString(w):
			words = words[1:]
		case shellPrefix[w]:
			words = words[1:]
		case shellSkipSegment[w] || shellWords[w]:
			return "", wordNone
		case w == "eval":
			return "", wordTemplated
		case w == "sudo":
			rest := words[1:]
			for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
				skip := 1
				if sudoArgOptions[rest[0]] {
					skip = 2
				}
				rest = rest[min(skip, len(rest)):]
			}
			words = rest
		case w == "env":
			rest := words[1:]
			for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
				skip := 1
				if rest[0] == "-u" || rest[0] == "-C" {
					skip = 2
				}
				rest = rest[min(skip, len(rest)):]
			}
			words = rest
		case w == "command":
			if len(words) > 1 && (words[1] == "-v" || words[1] == "-V") {
				return "", wordNone // looks a command up, doesn't run it
			}
			words = words[1:]
		default:
			if !strings.Contains(w, "$(") {
				w = strings.TrimRight(w, ")")
			}
			return w, classify(w)
		}
	}
	return "", wordNone
}

// redirection matches a redirection word ("2>/dev/null", ">", "<file").
var redirection = regexp.MustCompile(`^[0-9]*[<>]`)

var heredoc = regexp.MustCompile(`(?:^|[^<])<<(-?)\s*["']?([A-Za-z_][A-Za-z0-9_]*)["']?`)

// stripHeredocs removes here-document bodies (they are data, not commands).
func stripHeredocs(text string) string {
	var out []string
	delim, dash := "", false
	for _, line := range strings.Split(text, "\n") {
		if delim != "" {
			l := line
			if dash {
				l = strings.TrimLeft(l, "\t")
			}
			if l == delim {
				delim = ""
			}
			continue
		}
		out = append(out, line)
		if m := heredoc.FindStringSubmatch(line); m != nil {
			delim, dash = m[2], m[1] == "-"
		}
	}
	return strings.Join(out, "\n")
}

// jinjaEnd returns the index of the last byte of the template expression
// ({{ ... }} or {% ... %}) that starts at b[i], or -1 if none does. A pipe
// inside one is a filter, not a shell pipe, so the splitters keep it whole.
func jinjaEnd(b []byte, i int) int {
	if i+1 >= len(b) || b[i] != '{' || (b[i+1] != '{' && b[i+1] != '%') {
		return -1
	}
	closer := "}}"
	if b[i+1] == '%' {
		closer = "%}"
	}
	if end := strings.Index(string(b[i+2:]), closer); end >= 0 {
		return i + 2 + end + 1
	}
	return -1
}

// shellSegments splits text into simple commands at the unquoted operators
// | & ; and newlines (so also || and &&), leaving command substitutions
// $( ... ) and `...` whole, and dropping comments.
func shellSegments(text string) []string {
	var segs []string
	var cur strings.Builder
	flush := func() {
		if strings.TrimSpace(cur.String()) != "" {
			segs = append(segs, cur.String())
		}
		cur.Reset()
	}
	var quote byte
	depth := 0
	backtick := false
	runes := []byte(text)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '\\' && quote != '\'':
			cur.WriteByte(c)
			if i+1 < len(runes) {
				i++
				cur.WriteByte(runes[i])
			}
		case quote != 0:
			if c == quote {
				quote = 0
			}
			cur.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
			cur.WriteByte(c)
		case c == '`':
			backtick = !backtick
			cur.WriteByte(c)
		case c == '{' && jinjaEnd(runes, i) >= 0:
			end := jinjaEnd(runes, i)
			cur.Write(runes[i : end+1])
			i = end
		case c == '$' && i+1 < len(runes) && runes[i+1] == '(':
			depth++
			cur.WriteString("$(")
			i++
		case c == ')' && depth > 0:
			depth--
			cur.WriteByte(c)
		case c == '#' && depth == 0 && !backtick && (cur.Len() == 0 || strings.HasSuffix(cur.String(), " ") || strings.HasSuffix(cur.String(), "\t")):
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			flush()
		case depth == 0 && !backtick && (c == '|' || c == '&' || c == ';' || c == '\n'):
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return segs
}

// splitWords splits a simple command into words, honoring quotes and
// backslashes (and removing them), like a shell would. A command
// substitution stays inside its word.
func splitWords(s string) []string {
	var words []string
	var cur strings.Builder
	inWord := false
	var quote byte
	depth := 0
	flush := func() {
		if inWord {
			words = append(words, cur.String())
		}
		cur.Reset()
		inWord = false
	}
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case c == '\\' && quote != '\'':
			if i+1 < len(b) {
				i++
				if b[i] != '\n' {
					cur.WriteByte(b[i])
					inWord = true
				}
			}
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			inWord = true
		case c == '{' && jinjaEnd(b, i) >= 0:
			end := jinjaEnd(b, i)
			cur.Write(b[i : end+1])
			inWord = true
			i = end
		case c == '$' && i+1 < len(b) && b[i+1] == '(':
			depth++
			cur.WriteString("$(")
			inWord = true
			i++
		case c == ')' && depth > 0:
			depth--
			cur.WriteByte(c)
		case (c == ' ' || c == '\t' || c == '\n') && depth == 0:
			flush()
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	flush()
	return words
}
