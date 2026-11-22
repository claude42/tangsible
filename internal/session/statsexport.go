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
)

// The statistics screen's 'c' export (design-docs/Stats.md): the run's timing
// as one CSV file, a line per host per executed task.

var statsCSVHeader = []string{"host", "play", "task", "start time", "end time", "result"}

// csvTime formats a timestamp for the export (RFC 3339 with milliseconds, in
// the local zone); a missing one is an empty field.
func csvTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02T15:04:05.000Z07:00")
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

// timingCSVFileName names the export like revisit's 'E' names a run file (the
// run's start time and command line), with a .csv suffix.
func (s *liveSession) timingCSVFileName() string {
	e := revisit.RevisitEntry{
		Playbook: s.targetPlaybook,
		Role:     s.targetRole,
		Args:     strings.Join(s.passthroughArgs, " "),
		Time:     s.startedAt.UTC().Format(time.RFC3339),
	}
	return revisit.ExportFileNameExt(e, ".csv")
}

// exportTimingCSV writes the CSV into the current directory, refusing to
// overwrite a file; a failed write leaves no partial file. Returns the name.
func (s *liveSession) exportTimingCSV() (string, error) {
	name := s.timingCSVFileName()
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return name, errors.New("file exists")
		}
		return name, err
	}
	err = writeTimingCSV(f, s.state)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(name)
	}
	return name, err
}
