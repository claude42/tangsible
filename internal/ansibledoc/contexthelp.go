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

package ansibledoc

// Structured module and keyword documentation for the drill-down's context
// help (design-docs/Context Help.md): `ansible-doc -j` parsed into just the
// fields the pop-up shows. Pure parsing lives in parse* so it can be tested
// against fixture JSON without ansible-doc on PATH.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"code.aw.net/claude/tangsible/internal/execerr"
	"code.aw.net/claude/tangsible/internal/runner"
)

// OptionDoc is one module option (or suboption).
type OptionDoc struct {
	Description string // first paragraph, markup stripped
	Type        string // "str", "list of str", ...
	Default     string // display form; only meaningful when HasDefault
	HasDefault  bool
	Choices     []string
	Required    bool
	Suboptions  map[string]*OptionDoc
}

// ModuleDoc is a module's short description and its options.
type ModuleDoc struct {
	Name             string // as ansible-doc names it (the FQCN)
	ShortDescription string
	Options          map[string]*OptionDoc
}

// KeywordDoc is a playbook keyword (name, when, register, ...).
type KeywordDoc struct {
	Description string
	Type        string
	AppliesTo   []string
}

// Lookup follows path (an option name, then suboption names) and returns the
// option it ends at.
func (m *ModuleDoc) Lookup(path []string) (*OptionDoc, bool) {
	opts := m.Options
	var cur *OptionDoc
	for _, name := range path {
		o, ok := opts[name]
		if !ok {
			return nil, false
		}
		cur, opts = o, o.Suboptions
	}
	return cur, cur != nil
}

// FetchModuleDoc runs `ansible-doc -t module -j action`. action is whatever
// the task reported; ansible-doc resolves bare names and FQCNs alike. An
// unknown module is an error (ansible-doc itself only warns and prints {}).
func FetchModuleDoc(action string) (*ModuleDoc, error) {
	out, err := runAnsibleDoc("-t", "module", "-j", action)
	if err != nil {
		return nil, err
	}
	return parseModuleDoc(out)
}

// FetchKeywordDocs returns every playbook keyword's documentation: the names
// come from `ansible-doc -t keyword -l`, the docs from one `-t keyword -j
// <names...>` call - -j alone needs names.
func FetchKeywordDocs() (map[string]KeywordDoc, error) {
	list, err := runAnsibleDoc("-t", "keyword", "-l")
	if err != nil {
		return nil, err
	}
	names := parseKeywordNames(string(list))
	if len(names) == 0 {
		return nil, errors.New("ansible-doc -t keyword -l: no keywords listed")
	}
	out, err := runAnsibleDoc(append([]string{"-t", "keyword", "-j"}, names...)...)
	if err != nil {
		return nil, err
	}
	return parseKeywordDocs(out)
}

// keywordNameRe matches a listing line that starts a keyword: the name at
// column 0, a colon. Descriptions wrap onto indented continuation lines.
var keywordNameRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*):`)

// parseKeywordNames extracts the keyword names from `ansible-doc -t keyword
// -l`. Its output is YAML-shaped but not safe to parse as YAML (ANSI codes,
// unquoted text containing ": "), so names are picked out line by line.
func parseKeywordNames(list string) []string {
	var names []string
	for _, line := range strings.Split(stripANSI(list), "\n") {
		if m := keywordNameRe.FindStringSubmatch(line); m != nil {
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	return names
}

// runAnsibleDoc goes through runner.NewToolCommand like FetchAnsibleDoc: in an
// execution environment the help must describe that environment's modules
// and keywords, not the host's.
func runAnsibleDoc(args ...string) ([]byte, error) {
	cmd, err := runner.NewToolCommand("ansible-doc", args, runner.ToolOptions{})
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = execerr.Fallback("ansible-doc", err)
		}
		return nil, errors.New(msg)
	}
	return out, nil
}

type rawOption struct {
	Description any                  `json:"description"`
	Type        string               `json:"type"`
	Elements    string               `json:"elements"`
	Default     json.RawMessage      `json:"default"`
	Choices     json.RawMessage      `json:"choices"`
	Required    bool                 `json:"required"`
	Aliases     []string             `json:"aliases"`
	Suboptions  map[string]rawOption `json:"suboptions"`
}

func parseModuleDoc(raw []byte) (*ModuleDoc, error) {
	var top map[string]struct {
		Doc struct {
			ShortDescription string               `json:"short_description"`
			Options          map[string]rawOption `json:"options"`
		} `json:"doc"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("ansible-doc -j: %w", err)
	}
	for name, entry := range top { // one module asked for, one entry back
		return &ModuleDoc{
			Name:             name,
			ShortDescription: StripMarkup(entry.Doc.ShortDescription),
			Options:          convertOptions(entry.Doc.Options),
		}, nil
	}
	return nil, errors.New("module not found")
}

func convertOptions(in map[string]rawOption) map[string]*OptionDoc {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*OptionDoc, len(in))
	for name, r := range in {
		o := &OptionDoc{
			Description: firstParagraph(r.Description),
			Type:        r.Type,
			Required:    r.Required,
			Choices:     parseChoices(r.Choices),
			Suboptions:  convertOptions(r.Suboptions),
		}
		if o.Type == "" {
			o.Type = "str" // ansible's own default
		}
		if r.Elements != "" {
			o.Type += " of " + r.Elements
		}
		o.Default, o.HasDefault = formatDefault(r.Default)
		out[name] = o
		for _, alias := range r.Aliases { // a task may use either spelling
			if _, taken := in[alias]; !taken {
				out[alias] = o
			}
		}
	}
	return out
}

// parseChoices accepts both shapes ansible-doc uses: a list of values, or a
// value -> description map (only the values are kept, sorted).
func parseChoices(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []any
	if json.Unmarshal(raw, &list) == nil {
		out := make([]string, len(list))
		for i, v := range list {
			out[i] = scalarString(v)
		}
		return out
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	return nil
}

// formatDefault renders a JSON default for display; absent or null means no
// default.
func formatDefault(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw), true
	}
	switch x := v.(type) {
	case []any, map[string]any:
		b, _ := json.Marshal(x)
		return string(b), true
	}
	return scalarString(v), true
}

func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	default:
		return fmt.Sprint(x)
	}
}

func parseKeywordDocs(raw []byte) (map[string]KeywordDoc, error) {
	var top map[string]struct {
		Description string   `json:"description"`
		Type        string   `json:"type"`
		AppliesTo   []string `json:"applies_to"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("ansible-doc -j: %w", err)
	}
	out := make(map[string]KeywordDoc, len(top))
	for name, k := range top {
		out[name] = KeywordDoc{Description: StripMarkup(k.Description), Type: k.Type, AppliesTo: k.AppliesTo}
	}
	return out, nil
}

// firstParagraph is the first paragraph of an ansible-doc description (a
// list of paragraphs, or a plain string), markup stripped, whitespace
// collapsed.
func firstParagraph(desc any) string {
	var s string
	switch d := desc.(type) {
	case string:
		s = d
	case []any:
		if len(d) > 0 {
			s, _ = d[0].(string)
		}
	}
	return StripMarkup(s)
}

var (
	// ansible-doc's inline markup: I() B() C() U() M() O() V() RV() P() E()
	// wrap one argument, L(text,url) has two. "\," and "\)" escape.
	markupRe     = regexp.MustCompile(`\b(RV|[IBCUMLOVPE])\(((?:[^()\\]|\\.)*)\)`)
	linkTextRe   = regexp.MustCompile(`^((?:[^,\\]|\\.)*),`)
	markupUnesc  = strings.NewReplacer(`\,`, `,`, `\)`, `)`, `\(`, `(`, `\\`, `\`)
	whitespaceRe = regexp.MustCompile(`\s+`)
)

// StripMarkup turns ansible-doc's inline markup into plain text: O(src) ->
// src, L(text,url) -> text, C(chattr) -> chattr, and so on - the markup
// only styles text, so nothing else is lost - and collapses whitespace.
func StripMarkup(s string) string {
	s = markupRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := markupRe.FindStringSubmatch(m)
		kind, arg := sub[1], sub[2]
		if kind == "L" {
			if lm := linkTextRe.FindStringSubmatch(arg); lm != nil {
				arg = lm[1]
			}
		}
		return markupUnesc.Replace(arg)
	})
	s = strings.ReplaceAll(s, "HORIZONTALLINE", "")
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(s, " "))
}
