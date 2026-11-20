package eeinit

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const wbase = "execution-environment.yml"

func TestNextBackupNumber(t *testing.T) {
	for _, c := range []struct {
		names []string
		want  int
	}{
		{nil, 1},
		{[]string{wbase}, 1},
		{[]string{wbase, wbase + ".1"}, 2},
		{[]string{wbase + ".2", wbase + ".5"}, 6}, // gaps: max+1, not the first free
		{[]string{wbase + ".bak", wbase + ".0", wbase + ".01", wbase + ".1x", wbase + ".-3", "other.yml.9"}, 1},
		{[]string{wbase + ".10", wbase + ".9"}, 11}, // numeric, not alphabetical
	} {
		if got := nextBackupNumber(wbase, c.names); got != c.want {
			t.Errorf("nextBackupNumber(%v) = %d, want %d", c.names, got, c.want)
		}
	}
}

func TestBackupsToPrune(t *testing.T) {
	names := []string{wbase, wbase + ".3", wbase + ".10", wbase + ".1", wbase + ".bak", wbase + ".2", wbase + ".7", wbase + ".4", wbase + ".5", wbase + ".01", "x"}
	// 7 backups, keep 5: the two lowest go; nothing that is not a backup
	got := backupsToPrune(wbase, names, 5)
	if !reflect.DeepEqual(got, []string{wbase + ".1", wbase + ".2"}) {
		t.Errorf("got %v", got)
	}
	if got := backupsToPrune(wbase, names, 7); len(got) != 0 {
		t.Errorf("within the limit: %v", got)
	}
	if got := backupsToPrune(wbase, nil, 5); len(got) != 0 {
		t.Errorf("none: %v", got)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	return dirNames(dir)
}

func TestWriteNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, wbase)
	res, err := WriteDraft(path, []byte("v1\n"), true)
	if err != nil || !res.Created || res.Replaced || res.Unchanged || res.Backup != "" {
		t.Fatalf("%+v, %v", res, err)
	}
	if read(t, path) != "v1\n" {
		t.Error("content")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", fi.Mode())
	}
	// no temporary file is left
	if got := names(t, dir); !reflect.DeepEqual(got, []string{wbase}) {
		t.Errorf("directory: %v", got)
	}
}

func TestWriteChangedKeepsBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, wbase)
	os.WriteFile(path, []byte("old\n"), 0o640)
	res, err := WriteDraft(path, []byte("new\n"), true)
	if err != nil || !res.Replaced || res.Backup != wbase+".1" {
		t.Fatalf("%+v, %v", res, err)
	}
	if read(t, path) != "new\n" || read(t, path+".1") != "old\n" {
		t.Errorf("content: %q / %q", read(t, path), read(t, path+".1"))
	}
	// the permissions of the file survive (new file and backup)
	for _, p := range []string{path, path + ".1"} {
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
			t.Errorf("%s mode %v", p, fi.Mode())
		}
	}
	if got := names(t, dir); len(got) != 2 {
		t.Errorf("directory: %v", got)
	}
	// the next change is .2
	res, err = WriteDraft(path, []byte("newer\n"), true)
	if err != nil || res.Backup != wbase+".2" || read(t, path+".2") != "new\n" || read(t, path+".1") != "old\n" {
		t.Errorf("%+v, %v", res, err)
	}
}

func TestWriteUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, wbase)
	os.WriteFile(path, []byte("same\n"), 0o644)
	past := time.Now().Add(-48 * time.Hour)
	os.Chtimes(path, past, past)
	res, err := WriteDraft(path, []byte("same\n"), true)
	if err != nil || !res.Unchanged || res.Replaced || res.Backup != "" {
		t.Fatalf("%+v, %v", res, err)
	}
	if fi, _ := os.Stat(path); !fi.ModTime().Equal(past.Truncate(time.Second)) && time.Since(fi.ModTime()) < time.Hour {
		t.Errorf("an unchanged file was rewritten")
	}
	if got := names(t, dir); !reflect.DeepEqual(got, []string{wbase}) {
		t.Errorf("directory: %v", got)
	}
}

func TestWriteKeepsFiveBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, wbase)
	other := filepath.Join(dir, wbase+".bak")
	os.WriteFile(other, []byte("mine\n"), 0o644)
	os.WriteFile(path, []byte("v0\n"), 0o644)
	var last WriteResult
	for i := 1; i <= 8; i++ {
		var err error
		if last, err = WriteDraft(path, []byte("v"+string(rune('0'+i))+"\n"), true); err != nil {
			t.Fatal(err)
		}
	}
	var backups []string
	for _, n := range names(t, dir) {
		if _, ok := backupNumber(wbase, n); ok {
			backups = append(backups, n)
		}
	}
	if len(backups) != 5 {
		t.Fatalf("backups: %v", backups)
	}
	// the newest five survive: .4 to .8 (v3 to v7), the oldest were pruned
	for n := 4; n <= 8; n++ {
		if _, err := os.Stat(path + "." + string(rune('0'+n))); err != nil {
			t.Errorf("backup .%d is gone", n)
		}
	}
	if read(t, path+".8") != "v7\n" || read(t, path) != "v8\n" {
		t.Errorf("content: %q %q", read(t, path+".8"), read(t, path))
	}
	if !reflect.DeepEqual(last.Pruned, []string{wbase + ".3"}) {
		t.Errorf("the last write pruned %v", last.Pruned)
	}
	if read(t, other) != "mine\n" {
		t.Error("a file that is not a backup was touched")
	}
}

func TestWriteNoBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, wbase)
	os.WriteFile(path, []byte("old\n"), 0o644)
	os.WriteFile(path+".1", []byte("older\n"), 0o644)
	res, err := WriteDraft(path, []byte("new\n"), false)
	if err != nil || !res.Replaced || res.Backup != "" || len(res.Pruned) != 0 {
		t.Fatalf("%+v, %v", res, err)
	}
	if read(t, path) != "new\n" || read(t, path+".1") != "older\n" {
		t.Error("content")
	}
	if got := names(t, dir); len(got) != 2 {
		t.Errorf("a backup was made or removed: %v", got)
	}
}

func TestWriteRefusesSymlinkAndDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yml")
	os.WriteFile(target, []byte("keep\n"), 0o644)
	link := filepath.Join(dir, wbase)
	if err := os.Symlink(target, link); err != nil {
		t.Skip("no symlinks")
	}
	if err := CheckOutputPath(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("symlink: %v", err)
	}
	if _, err := WriteDraft(link, []byte("new\n"), true); err == nil {
		t.Error("a symlink must be refused")
	}
	if read(t, target) != "keep\n" {
		t.Error("the symlink's target was written")
	}
	d := filepath.Join(dir, "sub", wbase)
	os.MkdirAll(d, 0o755)
	if err := CheckOutputPath(d); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("directory: %v", err)
	}
	if err := CheckOutputPath(filepath.Join(dir, "missing.yml")); err != nil {
		t.Errorf("a missing file is fine: %v", err)
	}
}

func TestWriteRemovesStaleTemps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, wbase)
	stale := filepath.Join(dir, tempPrefix(wbase)+"old")
	fresh := filepath.Join(dir, tempPrefix(wbase)+"fresh")
	os.WriteFile(stale, []byte("x"), 0o600)
	os.WriteFile(fresh, []byte("x"), 0o600)
	past := time.Now().Add(-3 * time.Hour)
	os.Chtimes(stale, past, past)
	if _, err := WriteDraft(path, []byte("v\n"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a stale temporary file was left")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a recent temporary file (maybe another run's) was removed")
	}
}

// A crash after the backup and before the rename (steps 2 and 3 only): the
// output file is whole, the backup exists, and a later run recovers.
func TestCrashBetweenBackupAndRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, wbase)
	os.WriteFile(path, []byte("old\n"), 0o644)
	tmp, err := stageTemp(path, []byte("new\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := makeBackup(path)
	if err != nil || backup != wbase+".1" {
		t.Fatalf("%q, %v", backup, err)
	}
	// ... killed here: the output path still holds the complete old file
	if read(t, path) != "old\n" || read(t, path+".1") != "old\n" {
		t.Fatalf("state after the crash: %q %q", read(t, path), read(t, path+".1"))
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Fatal("the temporary file is expected to be left behind")
	}
	// a full run recovers: the new content, a second backup, the stray temp is
	// young so it stays (it is not ours to delete)
	res, err := WriteDraft(path, []byte("new\n"), true)
	if err != nil || !res.Replaced || read(t, path) != "new\n" {
		t.Fatalf("%+v, %v", res, err)
	}
	if read(t, path+".2") != "old\n" {
		t.Error("second backup")
	}
}

// A crash while staging: the temporary file is incomplete, the output file untouched.
func TestStageFailureLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing-dir", wbase)
	if _, err := WriteDraft(path, []byte("x"), true); err == nil {
		t.Error("expected an error for a missing directory")
	}
	if got := names(t, dir); len(got) != 0 && !(len(got) == 1 && got[0] == "missing-dir") {
		t.Errorf("directory: %v", got)
	}
}

// A file that appears between the check and the link is "existing", not clobbered.
func TestAppearingFileIsNotClobbered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, wbase)
	tmp, err := stageTemp(path, []byte("ours\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("theirs\n"), 0o644) // appears now
	if err := os.Link(tmp, path); err == nil {
		t.Fatal("link must not overwrite")
	}
	if read(t, path) != "theirs\n" {
		t.Error("clobbered")
	}
	// the full procedure on a directory where the file is there treats it as existing
	res, err := WriteDraft(path, []byte("ours\n"), true)
	if err != nil || !res.Replaced || res.Backup != wbase+".1" || read(t, path+".1") != "theirs\n" {
		t.Errorf("%+v, %v", res, err)
	}
}

func TestSummary(t *testing.T) {
	rep := Report{Collections: 3, Pinned: 3, Todos: 3, Implied: 1, Binaries: 3}
	got := Summary(WriteResult{Replaced: true, Backup: wbase + ".1"}, rep, false)
	want := "Created execution-environment.yml. The previous file is saved as execution-environment.yml.1.\n" +
		"\n" +
		"3 collection(s) (3 pinned from this host).\n" +
		"1 package(s) added for lookups or modules; review them in the draft.\n" +
		"3 binaries used on the controller are missing from the base image; see the draft.\n" +
		"\n" +
		"3 TODO(s) to resolve in this file.\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	// nothing to report beyond the file: no empty blocks
	got = Summary(WriteResult{Created: true}, Report{Collections: 1, Pinned: 0}, false)
	if got != "Created execution-environment.yml.\n\n1 collection(s).\n" {
		t.Errorf("plain: %q", got)
	}
	// from Galaxy, and deprecated ones
	got = Summary(WriteResult{Created: true}, Report{Collections: 3, Pinned: 2, FromGalaxy: 1, Deprecated: 1}, false)
	if got != "Created execution-environment.yml.\n\n3 collection(s) (2 pinned from this host, 1 from Galaxy).\n1 collection(s) deprecated on Galaxy; see the comment in the draft.\n" {
		t.Errorf("galaxy: %q", got)
	}
	got = Summary(WriteResult{Created: true}, Report{Collections: 2, FromGalaxy: 2}, false)
	if got != "Created execution-environment.yml.\n\n2 collection(s) (2 from Galaxy).\n" {
		t.Errorf("galaxy only: %q", got)
	}
	// TODOs only: one empty line before them
	got = Summary(WriteResult{Created: true}, Report{Todos: 2}, false)
	if got != "Created execution-environment.yml.\n\n2 TODO(s) to resolve in this file.\n" {
		t.Errorf("todos only: %q", got)
	}
	// one binary: singular
	got = Summary(WriteResult{Created: true}, Report{Collections: 2, Pinned: 2, Binaries: 1}, false)
	if !strings.Contains(got, "\n\n2 collection(s) (2 pinned from this host).\n1 binary used on the controller is missing") {
		t.Errorf("singular: %q", got)
	}
	// no backup, pruned backups, unchanged
	got = Summary(WriteResult{Replaced: true}, Report{}, true)
	if !strings.Contains(got, "replaced without a backup (--no-backup)") {
		t.Errorf("no-backup: %q", got)
	}
	got = Summary(WriteResult{Replaced: true, Backup: wbase + ".8", Pruned: []string{wbase + ".3"}}, Report{}, false)
	if !strings.Contains(got, "saved as execution-environment.yml.8.\nRemoved the oldest backup(s): execution-environment.yml.3.") {
		t.Errorf("pruned: %q", got)
	}
	got = Summary(WriteResult{Unchanged: true}, Report{Todos: 1, Binaries: 2}, false)
	if !strings.HasPrefix(got, "execution-environment.yml is unchanged.\n\n2 binaries") || !strings.HasSuffix(got, "\n\n1 TODO(s) to resolve in this file.\n") {
		t.Errorf("unchanged: %q", got)
	}
}
