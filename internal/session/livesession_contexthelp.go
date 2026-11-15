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

package session

import (
	"strings"

	"code.aw.net/claude/tangsible/internal/ansibledoc"
	"code.aw.net/claude/tangsible/internal/uikit"
)

// --- context help (design-docs/Context Help.md) ---

// moduleDocEntry is a module's fetched ansible-doc, once ready.
type moduleDocEntry struct {
	doc     *ansibledoc.ModuleDoc
	pending bool
}

// prefetchContextHelp starts the ansible-doc fetches the pop-up needs - the
// task's module (once per action) and the keyword list (once per session) -
// the moment a drill-down opens, so a click finds them ready. A click before
// then, or after a failed fetch, opens nothing.
func (s *liveSession) prefetchContextHelp(action string) {
	if action != "" {
		if _, have := s.moduleDocs[action]; !have {
			s.moduleDocs[action] = &moduleDocEntry{pending: true}
			go func() {
				doc, err := ansibledoc.FetchModuleDoc(action)
				s.app.QueueUpdate(func() {
					if err != nil {
						doc = nil
					}
					s.moduleDocs[action] = &moduleDocEntry{doc: doc}
				})
			}()
		}
	}
	if !s.keywordDocsRequested {
		s.keywordDocsRequested = true
		go func() {
			kw, err := ansibledoc.FetchKeywordDocs()
			s.app.QueueUpdate(func() {
				if err == nil {
					s.keywordDocs = kw
				}
			})
		}()
	}
}

// keyLinesMatching returns the 1-based numbers of the source lines on which
// a row of cells (trailing blanks ignored) can be the start of the line, with
// word followed by ':' at rune column start. A row is a line's start when it
// is a prefix of it - the whole line, or its first chunk if the view wrapped
// it - which is how a click is mapped to a source line without counting rows:
// a wrapped line earlier in the text throws any row count off.
func keyLinesMatching(lines []string, row string, start int, word string) []int {
	row = strings.TrimRight(row, " ")
	want := word + ":"
	var out []int
	for i, l := range lines {
		l = strings.TrimRight(l, " ")
		if row == "" || !strings.HasPrefix(l, row) {
			continue
		}
		if r := []rune(l); start > len(r) || !strings.HasPrefix(string(r[start:]), want) {
			continue
		}
		out = append(out, i+1)
	}
	return out
}

// openKeyHelp opens context help for the key under a click. The click is
// mapped to a line of the task's own YAML (keyLinesMatching) and classified
// by position (ansibledoc.LocateTaskKey) - name: is a keyword at task level
// but an option under user:. Several candidate lines are fine as long as they
// all classify the same; otherwise no pop-up rather than a wrong one.
func (s *liveSession) openKeyHelp(c taskDefClick, x, y int) bool {
	task, host := s.outputTask, s.outputHost
	if task == nil || c.colOff != 0 {
		return false
	}
	action := uikit.TaskAction(task, host)
	src := s.sourceIndex[task.Path]
	var ref ansibledoc.KeyRef
	for n, line := range keyLinesMatching(strings.Split(src, "\n"), string(c.line), c.start, c.word) {
		r := ansibledoc.LocateTaskKey(src, line, c.start+1, c.word, action)
		if n > 0 && !(r.Kind == ref.Kind && strings.Join(r.Path, "\x00") == strings.Join(ref.Path, "\x00")) {
			return false
		}
		ref = r
	}

	var p infoPopup
	switch ref.Kind {
	case ansibledoc.KeyKeyword:
		kw, ok := s.keywordDocs[c.word]
		if !ok {
			return false
		}
		p = keywordPopup(c.word, kw)
	case ansibledoc.KeyModule, ansibledoc.KeyOption:
		entry := s.moduleDocs[action]
		if entry == nil || entry.doc == nil {
			return false
		}
		if ref.Kind == ansibledoc.KeyModule {
			p = modulePopup(entry.doc)
			break
		}
		opt, ok := entry.doc.Lookup(ref.Path)
		if !ok {
			return false
		}
		p = optionPopup(c.word, opt)
	default:
		return false
	}
	s.showPopup(p, x, y)
	return true
}

// optionPopup is the pop-up for a module option: name, type, required,
// default, choices, then the first paragraph of its description.
func optionPopup(name string, o *ansibledoc.OptionDoc) infoPopup {
	rows := []popupRow{{"Name:", name}, {"Type:", o.Type}}
	if o.Required {
		rows = append(rows, popupRow{"Required:", "yes"})
	}
	if o.HasDefault {
		rows = append(rows, popupRow{"Default:", truncateRunes(o.Default, popupMaxText)})
	}
	if len(o.Choices) > 0 {
		rows = append(rows, popupRow{"Choices:", truncateRunes(strings.Join(o.Choices, ", "), popupMaxText)})
	}
	if o.Description != "" {
		rows = append(rows, popupRow{"About:", wrapText(o.Description, popupMaxText, popupDescLines)})
	}
	return infoPopup{title: " Option ", rows: rows}
}

// keywordPopup is the pop-up for a task keyword (ansible-doc has no default,
// choices or required for these).
func keywordPopup(name string, k ansibledoc.KeywordDoc) infoPopup {
	rows := []popupRow{{"Name:", name}}
	if k.Type != "" {
		rows = append(rows, popupRow{"Type:", k.Type})
	}
	if len(k.AppliesTo) > 0 {
		rows = append(rows, popupRow{"Applies to:", strings.Join(k.AppliesTo, ", ")})
	}
	if k.Description != "" {
		rows = append(rows, popupRow{"About:", wrapText(k.Description, popupMaxText, popupDescLines)})
	}
	return infoPopup{title: " Keyword ", rows: rows}
}

// modulePopup is the pop-up for the module's own key: name and short
// description.
func modulePopup(m *ansibledoc.ModuleDoc) infoPopup {
	rows := []popupRow{{"Module:", m.Name}}
	if m.ShortDescription != "" {
		rows = append(rows, popupRow{"About:", wrapText(m.ShortDescription, popupMaxText, popupDescLines)})
	}
	return infoPopup{title: " Module ", rows: rows}
}
