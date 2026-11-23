package eeinit

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// LookupUse is a lookup plugin the playbook calls (lookup(), query(), q(),
// or a with_* loop), as written.
type LookupUse struct {
	Name string
	// Collections is the collections: search list in effect.
	Collections []string
	// Loc is "path:line".
	Loc string
	// Cmds are the command lines of a pipe lookup's terms: it runs them
	// through a shell on the controller, so what they run has to be in the
	// image. A term that is not a string literal is "{{ ... }}" (unknown).
	Cmds []string
}

var (
	// lookupCall matches a call with a literal plugin name; lookupVarCall one
	// whose first argument is a variable (the name can't be known). "q" is
	// query's alias.
	lookupCall    = regexp.MustCompile(`\b(?:lookup|query|q)\(\s*['"]([A-Za-z0-9_.]+)['"]`)
	lookupVarCall = regexp.MustCompile(`\b(?:lookup|query|q)\(\s*[A-Za-z_]`)
	// jinjaComment is {# ... #}.
	jinjaComment = regexp.MustCompile(`(?s)\{#.*?#\}`)
)

// ScanLookups finds the lookups a YAML file uses: in every string scalar
// (task arguments, vars, defaults, group_vars, conditions - not only task
// lists) and in with_<lookup> task keys. own is the collection a collection
// role's file belongs to. templated counts the calls whose plugin name is not
// a literal. A file that doesn't parse yields nothing.
func ScanLookups(path string, data []byte, own []string) (uses []LookupUse, templated int) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil, 0
	}
	var walk func(n *yaml.Node, colls []string)
	walk = func(n *yaml.Node, colls []string) {
		switch n.Kind {
		case yaml.ScalarNode:
			if n.ShortTag() == "!!str" {
				u, t := scanText(n.Value, func(offset int) string { return scalarLoc(path, n, offset) }, colls)
				uses = append(uses, u...)
				templated += t
			}
		case yaml.MappingNode:
			colls := append(collectionsOf(n), colls...)
			for i := 0; i+1 < len(n.Content); i += 2 {
				key := n.Content[i]
				if name, ok := strings.CutPrefix(key.Value, "with_"); ok && moduleName.MatchString(name) {
					uses = append(uses, LookupUse{Name: name, Collections: colls, Loc: nodeLoc(path, key)})
				}
				walk(n.Content[i+1], colls)
			}
		default:
			for _, c := range n.Content {
				walk(c, colls)
			}
		}
	}
	walk(doc.Content[0], own)
	return uses, templated
}

// ScanTemplate finds the lookups a Jinja template file uses ({# ... #}
// comments removed, line numbers kept).
func ScanTemplate(path, text string) (uses []LookupUse, templated int) {
	text = jinjaComment.ReplaceAllStringFunc(text, func(c string) string {
		return strings.Repeat("\n", strings.Count(c, "\n"))
	})
	return scanText(text, func(offset int) string {
		return path + ":" + strconv.Itoa(1+strings.Count(text[:offset], "\n"))
	}, nil)
}

// scanText finds the lookup calls in text; loc turns a byte offset in text
// into a location.
func scanText(text string, loc func(offset int) string, colls []string) (uses []LookupUse, templated int) {
	if !strings.Contains(text, "(") {
		return nil, 0
	}
	for _, m := range lookupCall.FindAllStringSubmatchIndex(text, -1) {
		u := LookupUse{Name: text[m[2]:m[3]], Collections: colls, Loc: loc(m[0])}
		if u.Name == "pipe" || u.Name == "ansible.builtin.pipe" {
			u.Cmds = pipeTerms(text[m[1]:])
		}
		uses = append(uses, u)
	}
	templated = len(lookupVarCall.FindAllStringIndex(text, -1))
	return uses, templated
}

// pipeTermLiteral is one more argument of a lookup call that is a quoted
// string; pipeTermAny is any argument at all (a variable, a list, ...).
var (
	pipeTermLiteral = regexp.MustCompile(`^\s*,\s*(?:'([^']*)'|"([^"]*)")`)
	pipeTermAny     = regexp.MustCompile(`^\s*,\s*[^\s,)]`)
)

// pipeTerms reads the arguments after a pipe lookup's name (text starts just
// past the closing quote of 'pipe'): every leading string literal is a
// command; a first argument that isn't one is an unknown command.
func pipeTerms(text string) []string {
	var cmds []string
	for {
		m := pipeTermLiteral.FindStringSubmatch(text)
		if m == nil {
			break
		}
		cmds = append(cmds, m[1]+m[2])
		text = text[len(m[0]):]
	}
	if len(cmds) == 0 && pipeTermAny.MatchString(text) {
		cmds = append(cmds, "{{ unknown }}")
	}
	return cmds
}

// nodeLoc is "path:line" of a node.
func nodeLoc(path string, n *yaml.Node) string { return path + ":" + strconv.Itoa(n.Line) }

// scalarLoc is the location of byte offset in a scalar's value: the scalar's
// own line for a one-line value, further down for a multi-line one (a block
// scalar's text starts on the line after its | or >).
func scalarLoc(path string, n *yaml.Node, offset int) string {
	line := n.Line + strings.Count(n.Value[:min(offset, len(n.Value))], "\n")
	if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		line++
	}
	return path + ":" + strconv.Itoa(line)
}

// templateExts are the file extensions of Jinja templates.
var templateExts = map[string]bool{".j2": true, ".jinja2": true}

// TemplateFiles lists the Jinja template files under dir, skipping .git and
// .tangsible.
func TemplateFiles(dir string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".tangsible" {
				return filepath.SkipDir
			}
			return nil
		}
		if templateExts[strings.ToLower(filepath.Ext(path))] {
			files = append(files, path)
		}
		return nil
	})
	return files
}
