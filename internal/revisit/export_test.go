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

package revisit

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExportRoundTrip(t *testing.T) {
	e := RevisitEntry{Playbook: "site.yml", Args: "-l zen -e 'a b'", Time: "2026-10-27T18:43:47Z", ExitCode: 2, Name: "foo bar"}
	events := "{\"_event\":\"a\"}\n{\"_event\":\"b\",\"x\":\"==== output ====\"}\n"
	var buf bytes.Buffer
	err := WriteExport(&buf, e, "Host failed",
		[]string{"\x1b[0;32mPLAY [x]\x1b[0m", "", "ok"}, true,
		[]string{"warn"}, strings.NewReader(events))
	if err != nil {
		t.Fatal(err)
	}
	text := buf.String()
	if strings.Contains(text, "\x1b") {
		t.Errorf("export contains ANSI escapes")
	}
	if !strings.HasPrefix(text, exportMagic+"\n# command:  tangsible run site.yml -l zen -e 'a b'\n") {
		t.Errorf("unexpected header:\n%s", text)
	}

	got, err := ReadExport(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	want := e
	want.Imported = got
	if !reflect.DeepEqual(got.Entry, want) {
		t.Errorf("entry = %+v, want %+v", got.Entry, want)
	}
	if !reflect.DeepEqual(got.Output, []string{"PLAY [x]", "", "ok"}) || !got.HasOutput {
		t.Errorf("output = %q (has=%v)", got.Output, got.HasOutput)
	}
	if !reflect.DeepEqual(got.Stderr, []string{"warn"}) {
		t.Errorf("stderr = %q", got.Stderr)
	}
	if string(got.Events) != events {
		t.Errorf("events = %q, want verbatim %q", got.Events, events)
	}
}

func TestExportRoleWithoutOutputOrStderr(t *testing.T) {
	var buf bytes.Buffer
	e := RevisitEntry{Role: "postfix", Time: "2026-10-27T18:43:47Z"}
	if err := WriteExport(&buf, e, "Success", nil, false, nil, strings.NewReader("{}\n")); err != nil {
		t.Fatal(err)
	}
	got, err := ReadExport(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got.Entry.Role != "postfix" || got.HasOutput || len(got.Stderr) != 0 || string(got.Events) != "{}\n" {
		t.Errorf("got %+v", got)
	}
}

func TestReadExportRejects(t *testing.T) {
	for name, in := range map[string]string{
		"not an export": "- hosts: all\n",
		"no events":     exportMagic + "\n# playbook: a.yml\n# started: 2026-10-27T18:43:47Z\n# exit: 0 (Success)\n",
		"no target":     exportMagic + "\n# started: 2026-10-27T18:43:47Z\n# exit: 0\n\n" + exportEventsMarker + "\n",
		"bad time":      exportMagic + "\n# playbook: a.yml\n# started: yesterday\n# exit: 0\n\n" + exportEventsMarker + "\n",
		"bad exit code": exportMagic + "\n# playbook: a.yml\n# started: 2026-10-27T18:43:47Z\n# exit: x\n\n" + exportEventsMarker + "\n",
	} {
		if _, err := ReadExport([]byte(in)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestExportFileName(t *testing.T) {
	cases := []struct {
		e    RevisitEntry
		want string
	}{
		{RevisitEntry{Name: "my run: #1/x"}, "my_run_1_x.txt"},
		{RevisitEntry{Playbook: "site.yml", Args: `-e msg="hello world"`, Time: "bad"}, "bad_run_site.yml_-e_msg_hello_world.txt"},
		{RevisitEntry{Name: "///"}, "tangsible-run.txt"},
	}
	for _, c := range cases {
		if got := ExportFileName(c.e); got != c.want {
			t.Errorf("ExportFileName(%+v) = %q, want %q", c.e, got, c.want)
		}
	}
	long := ExportFileName(RevisitEntry{Name: strings.Repeat("a", 200)})
	if len(long) > 84 {
		t.Errorf("long name not capped: %d chars", len(long))
	}
}

func TestIsExportFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if !IsExportFile(write("a.txt", exportMagic+"\nrest")) {
		t.Error("export file not recognized")
	}
	if IsExportFile(write("b.yml", "- hosts: all\n")) || IsExportFile(write("c.txt", "")) || IsExportFile(filepath.Join(dir, "missing")) || IsExportFile(dir) {
		t.Error("non-export recognized as export")
	}
}
