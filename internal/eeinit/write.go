package eeinit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MaxBackups is how many numbered backups of the draft are kept
// (design-docs/EEInit.md, "Replacing an existing draft").
const MaxBackups = 5

// staleTempAge is how old a leftover temporary file must be to be removed.
const staleTempAge = time.Hour

// WriteResult is what WriteDraft did.
type WriteResult struct {
	// Unchanged: the file already held exactly this content; nothing was written.
	Unchanged bool
	// Created: there was no file before; Replaced: there was one and it is now
	// the new draft.
	Created, Replaced bool
	// Backup is the base name of the backup of the old file, "" if none was made.
	Backup string
	// Pruned are the backups removed to stay within MaxBackups; Warnings are
	// failures that did not stop the write (a backup that could not be removed).
	Pruned, Warnings []string
}

// CheckOutputPath fails when path exists and is not a regular file (a symlink,
// a directory): writing through a symlink would change its target, replacing
// it would break it.
func CheckOutputPath(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if !fi.Mode().IsRegular() {
		kind := "not a regular file"
		if fi.Mode()&fs.ModeSymlink != 0 {
			kind = "a symlink"
		} else if fi.IsDir() {
			kind = "a directory"
		}
		return fmt.Errorf("%s is %s; not replacing it", filepath.Base(path), kind)
	}
	return nil
}

var backupSuffix = regexp.MustCompile(`^[1-9][0-9]*$`)

// backupNumber reads the number from a backup name "<base>.<N>"; only a decimal
// number without a leading zero counts.
func backupNumber(base, name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, base+".")
	if !ok || !backupSuffix.MatchString(rest) {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil
}

// nextBackupNumber is the highest backup number among names, plus one (1 when
// there is none).
func nextBackupNumber(base string, names []string) int {
	next := 1
	for _, name := range names {
		if n, ok := backupNumber(base, name); ok && n >= next {
			next = n + 1
		}
	}
	return next
}

// backupsToPrune are the backups among names to remove to leave the keep
// highest-numbered ones, lowest number first. Names that are not backups are
// never returned.
func backupsToPrune(base string, names []string, keep int) []string {
	type numbered struct {
		name string
		n    int
	}
	var all []numbered
	for _, name := range names {
		if n, ok := backupNumber(base, name); ok {
			all = append(all, numbered{name, n})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].n < all[j].n })
	var out []string
	for i := 0; i < len(all)-keep; i++ {
		out = append(out, all[i].name)
	}
	return out
}

func dirNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

func tempPrefix(base string) string { return "." + base + ".tmp-" }

// removeStaleTemps removes leftover temporary files (a crashed or killed
// earlier run) older than staleTempAge.
func removeStaleTemps(dir, base string) {
	for _, name := range dirNames(dir) {
		if !strings.HasPrefix(name, tempPrefix(base)) {
			continue
		}
		if fi, err := os.Lstat(filepath.Join(dir, name)); err == nil && fi.Mode().IsRegular() && time.Since(fi.ModTime()) > staleTempAge {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// stageTemp writes content to a new temporary file next to path, with mode,
// and returns its path. The file is created exclusively.
func stageTemp(path string, content []byte, mode fs.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), tempPrefix(filepath.Base(path))+"*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	fail := func(err error) (string, error) {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Chmod(mode); err != nil {
		return fail(err)
	}
	if _, err := f.Write(content); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// makeBackup keeps the file at path under the next free backup name and
// returns that name. The backup is a hard link, so the original stays in place
// (and complete) until it is replaced; link fails when the name exists, which
// claims the number atomically, so a name another run took is skipped. Where
// links are not available the file is copied, exclusively.
func makeBackup(path string) (string, error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	for try := 0; try < 20; try++ {
		name := fmt.Sprintf("%s.%d", base, nextBackupNumber(base, dirNames(dir)))
		dst := filepath.Join(dir, name)
		err := os.Link(path, dst)
		if err == nil {
			return name, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		// no hard links here: copy
		err = copyExclusive(path, dst)
		if err == nil {
			return name, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return "", err
	}
	return "", errors.New("could not find a free backup name")
}

func copyExclusive(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// syncDir makes the rename durable; best effort.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}

// WriteDraft puts content at path (design-docs/EEInit.md, "Replacing an
// existing draft"). An existing file with the same content is left alone. A
// different one is kept as a numbered backup (unless backup is false), then
// replaced; at most MaxBackups backups remain. The old file is never renamed
// away: the new content is written to a temporary file first, the backup is a
// hard link, and the temporary file is renamed over path (atomic), so at every
// instant path holds the complete old or the complete new content.
func WriteDraft(path string, content []byte, backup bool) (WriteResult, error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	removeStaleTemps(dir, base)
	// A file that appears while the new one is created without a previous
	// file is the "existing" case: start over.
	for attempt := 0; attempt < 3; attempt++ {
		res, again, err := writeOnce(path, content, backup)
		if !again {
			return res, err
		}
	}
	return WriteResult{}, fmt.Errorf("%s keeps appearing while it is written", base)
}

func writeOnce(path string, content []byte, backup bool) (res WriteResult, again bool, err error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	if err := CheckOutputPath(path); err != nil {
		return res, false, err
	}
	mode := fs.FileMode(0o644)
	existed := false
	if fi, err := os.Lstat(path); err == nil {
		existed = true
		mode = fi.Mode().Perm()
		old, err := os.ReadFile(path)
		if err != nil {
			return res, false, err
		}
		if bytes.Equal(old, content) {
			res.Unchanged = true
			return res, false, nil
		}
	}
	tmp, err := stageTemp(path, content, mode)
	if err != nil {
		return res, false, err
	}
	defer os.Remove(tmp) // gone after the rename; this is for the failures

	if existed {
		if backup {
			if res.Backup, err = makeBackup(path); err != nil {
				return res, false, fmt.Errorf("backing up %s: %w", base, err)
			}
		}
		if err := os.Rename(tmp, path); err != nil {
			return res, false, err
		}
		res.Replaced = true
	} else {
		// link, not rename: it fails if a file appeared meanwhile
		if err := os.Link(tmp, path); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return res, true, nil
			}
			// no hard links: an exclusive create
			f, cerr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if cerr != nil {
				if errors.Is(cerr, fs.ErrExist) {
					return res, true, nil
				}
				return res, false, cerr
			}
			_, werr := f.Write(content)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				os.Remove(path)
				return res, false, werr
			}
		}
		res.Created = true
	}
	syncDir(dir)

	if res.Backup != "" {
		for _, name := range backupsToPrune(base, dirNames(dir), MaxBackups) {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("could not remove the old backup %s: %v", name, err))
			} else {
				res.Pruned = append(res.Pruned, name)
			}
		}
	}
	return res, false, nil
}
