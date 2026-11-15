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

// taskDefClick is what a click on the Task definition tab landed on, read
// back from the terminal's own cells - so wrapping, scrolling and colour tags
// need no handling.
type taskDefClick struct {
	word   string
	start  int    // column of the word's first cell within the view
	isKey  bool   // the word is directly followed by ':' - a YAML key
	line   []rune // the clicked row's cells
	colOff int    // horizontal scroll offset
}

// readTaskDefClick reports what is under the click at screen cell (x, y), if
// that is a word in the Task definition tab (drill-down open, tab active).
func (s *liveSession) readTaskDefClick(x, y int) (taskDefClick, bool) {
	if !s.viewingOutput || s.screen == nil || s.outputTabs.ActiveName() != "Task definition" {
		return taskDefClick{}, false
	}
	tv, ok := s.outputTabs.ActiveTextView()
	if !ok {
		return taskDefClick{}, false
	}
	ix, iy, iw, ih := tv.GetInnerRect()
	if x < ix || x >= ix+iw || y < iy || y >= iy+ih {
		return taskDefClick{}, false
	}
	line := make([]rune, iw)
	for i := range line {
		line[i], _, _, _ = s.screen.GetContent(ix+i, y)
	}
	start, end, ok := wordSpan(line, x-ix)
	if !ok {
		return taskDefClick{}, false
	}
	_, colOff := tv.GetScrollOffset()
	return taskDefClick{
		word:   string(line[start : end+1]),
		start:  start,
		isKey:  end+1 < len(line) && line[end+1] == ':',
		line:   line,
		colOff: colOff,
	}, true
}

// openContextPopup opens the pop-up for a click at screen cell (x, y) in the
// Task definition tab and reports whether it opened one. A word followed by
// ':' is a key and gets context help (every verb); any other word is a
// variable candidate (debug only).
func (s *liveSession) openContextPopup(x, y int) bool {
	c, ok := s.readTaskDefClick(x, y)
	if !ok {
		return false
	}
	if c.isKey {
		return s.openKeyHelp(c, x, y)
	}
	if s.debugMode {
		return s.openVarPopup(c.word, x, y)
	}
	return false
}
