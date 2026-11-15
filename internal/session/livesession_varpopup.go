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

// Variable pop-up (design-docs/DebugMode.md, "Mouse support"): a click on a
// word in the Task definition tab shows the variable's name, value and
// origin, with a Watch button that adds it to the variable explorer. Debug
// mode only; the pop-up itself is livesession_popup.go's.

// varPopupValueText renders a resolved value for the pop-up: like a watch
// row's, except the vault placeholder stands for itself, unquoted.
func varPopupValueText(v any) string {
	if v == vaultPlaceholder {
		return vaultPlaceholder
	}
	return formatWatchScalar(v)
}

// openVarPopup opens the variable pop-up for word, clicked at screen cell
// (x, y), if it is a defined scalar variable of the selected host/task, and
// reports whether it opened.
func (s *liveSession) openVarPopup(word string, x, y int) bool {
	// Masked: a click must never put a secret on screen by surprise; the
	// variable explorer is where a vaulted value is shown, on purpose.
	snapshot, _ := s.watchCurrentSnapshot()
	res := evaluateWatchExpr(word, s.watchMaskVaulted(snapshot))
	if res.kind != watchResolved {
		return false // not a defined (scalar) variable: no pop-up
	}
	rows := []popupRow{{"Name:", word}, {"Value:", truncateRunes(varPopupValueText(res.value), popupMaxText)}}
	if origin := s.watchOriginLabel(word); origin != "" {
		rows = append(rows, popupRow{"Defined:", truncateRunesLeft(origin, popupMaxText)})
	}
	s.showPopup(infoPopup{
		title:    " Variable ",
		rows:     rows,
		button:   " Watch ",
		onButton: func() { s.watchAdd(word) },
	}, x, y)
	return true
}

// watchAdd adds expr to the variable explorer's watch list, once.
func (s *liveSession) watchAdd(expr string) {
	for _, e := range s.watch.entries {
		if e.expr == expr {
			return
		}
	}
	s.watch.entries = append(s.watch.entries, watchEntry{expr: expr})
	s.rebuild()
}
