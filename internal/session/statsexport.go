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
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/revisit"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The statistics screen's 'c' export (design-docs/Stats.md): the run's timing
// as one CSV file, a line per host per executed task.

var statsCSVHeader = []string{"host", "play", "task", "start time", "end time", "result"}

// csvTime formats a timestamp for the export as "2026-11-21 18:00:23" in
// the local zone - the form Excel and Google Sheets read as a date and time
// (RFC 3339's "T" and zone suffix they take for text); a missing one is an
// empty field.
func csvTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// writeTimingCSV writes the header and then one record per host and task, in
// execution order: a task's hosts in the order they reported. Pure.
func writeTimingCSV(w io.Writer, state *playbook.PlaybookState) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(statsCSVHeader); err != nil {
		return err
	}
	for i, play := range state.Plays {
		playName := play.Name
		if playName == "" {
			playName = fmt.Sprintf("play %d", i+1)
		}
		for _, task := range play.Tasks {
			for _, host := range task.HostOrder {
				outcome, ok := task.Hosts[host]
				if !ok {
					continue
				}
				err := cw.Write([]string{host, playName, task.Name,
					csvTime(task.Started[host]), csvTime(task.Finished[host]), outcome.String()})
				if err != nil {
					return err
				}
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

// timingCSVFileName is the file name the export dialog suggests: the
// original run's own (statsCSVName, set for a revisited run), else named like
// a revisit export from this run's start time and command line, with .csv.
func (s *liveSession) timingCSVFileName() string {
	if s.statsCSVName != "" {
		return s.statsCSVName
	}
	e := revisit.RevisitEntry{
		Playbook: s.targetPlaybook,
		Role:     s.targetRole,
		Args:     strings.Join(s.passthroughArgs, " "),
		Time:     s.startedAt.UTC().Format(time.RFC3339),
	}
	return revisit.ExportFileNameExt(e, ".csv")
}

// writeTimingCSVFile writes the CSV to path, refusing to overwrite a file; a
// failed write leaves no partial file behind.
func writeTimingCSVFile(path string, state *playbook.PlaybookState) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return errors.New("file exists - choose another name")
		}
		return err
	}
	err = writeTimingCSV(f, state)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

const statsExportTitle = " Export the timing as CSV (enter: save, esc: cancel) "

// initStatsExportDialog builds the 'c' dialog, like revisit's export dialog:
// a file name written to the current directory (a path typed in works too);
// an error goes in the dialog's own title so it can be fixed in place.
func (s *liveSession) initStatsExportDialog() {
	s.statsExportInput = tview.NewInputField().SetLabel("File: ")
	uikit.StyleInputField(s.statsExportInput)
	s.statsExportInput.SetLabelColor(tcell.GetColor(uikit.StandardTextColor))
	note := tview.NewTextView().SetTextColor(tcell.GetColor(uikit.StandardTextColor)).
		SetText("One line per host and task: play, start and end time, result.")
	s.statsExportFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tview.NewBox(), 1, 0, false). // top margin
		AddItem(s.statsExportInput, 1, 0, true).
		AddItem(tview.NewBox(), 1, 0, false).
		AddItem(note, 1, 0, false)
	s.statsExportFlex.SetBorder(true).SetBorderColor(tcell.GetColor(uikit.DialogBorderColor)).
		SetTitleColor(tcell.GetColor(uikit.StandardTextColor)).SetTitle(statsExportTitle)
	s.pages.AddPage("statsexport", uikit.CenteredModal(s.statsExportFlex, 70, 7), true, false)
	s.statsExportInput.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			path := strings.TrimSpace(s.statsExportInput.GetText())
			if path == "" {
				return
			}
			if err := writeTimingCSVFile(path, s.state); err != nil {
				s.statsExportFlex.SetTitle(" " + tview.Escape(err.Error()) + " ")
				return
			}
			s.closeStatsExport()
			s.statsBottomBar.SetTextStyle(s.outputBottomBarNormalStyle())
			s.statsBottomBar.SetText(" Exported the timing to " + tview.Escape(path) + " ")
		case tcell.KeyEscape:
			s.closeStatsExport()
		}
	})
}

func (s *liveSession) openStatsExport() {
	s.statsExportInput.SetText(s.timingCSVFileName())
	s.statsExportFlex.SetTitle(statsExportTitle)
	s.statsExportOpen = true
	s.pages.ShowPage("statsexport")
	s.app.SetFocus(s.statsExportInput)
}

func (s *liveSession) closeStatsExport() {
	s.statsExportOpen = false
	s.pages.HidePage("statsexport")
	s.app.SetFocus(s.statsTabs.Primitive())
}
