package eeinit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"code.aw.net/claude/tangsible/internal/execerr"
	"code.aw.net/claude/tangsible/internal/rolepath"
)

// lookupChunk bounds how many names one ansible-doc call is asked about.
const lookupChunk = 80

// LookupCollections asks the host's ansible-doc which collection owns each
// of names (module names, short or qualified). A name the host doesn't know
// is simply absent from the result.
func LookupCollections(names []string) (map[string]string, error) {
	found := map[string]string{}
	for len(names) > 0 {
		n := min(lookupChunk, len(names))
		chunk := names[:n]
		names = names[n:]
		cmd := exec.Command("ansible-doc", append([]string{"-t", "module", "-j"}, chunk...)...)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		// Unknown names make ansible-doc warn on stderr (and exit non-zero
		// if none was known); the ones it did find are on stdout.
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				if msg := execerr.NotFoundMessage("ansible-doc", err); msg != "" {
					return nil, errors.New(msg)
				}
				return nil, fmt.Errorf("couldn't run ansible-doc: %w", err)
			}
		}
		for name, coll := range parseDocCollections(stdout.Bytes()) {
			found[name] = coll
		}
	}
	return found, nil
}

// parseDocCollections reads `ansible-doc -j` output: name -> doc.collection.
func parseDocCollections(raw []byte) map[string]string {
	var docs map[string]struct {
		Doc struct {
			Collection string `json:"collection"`
		} `json:"doc"`
	}
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil
	}
	out := map[string]string{}
	for name, d := range docs {
		if d.Doc.Collection != "" {
			out[name] = d.Doc.Collection
		}
	}
	return out
}

// InstalledCollections returns the collection versions installed on this
// host (ansible-galaxy collection list); where a collection sits on several
// paths, the one ansible would use first wins.
func InstalledCollections() (map[string]string, error) {
	cmd := exec.Command("ansible-galaxy", "collection", "list", "--format", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := execerr.NotFoundMessage("ansible-galaxy", err); msg != "" {
			return nil, errors.New(msg)
		}
		return nil, fmt.Errorf("ansible-galaxy collection list: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseInstalled(stdout.Bytes(), rolepath.CollectionDirs())
}

// parseInstalled reads `ansible-galaxy collection list --format json`: a
// map of collection path -> collection -> {version}. order is the
// collections path in ansible's own order, deciding which path wins.
func parseInstalled(raw []byte, order []string) (map[string]string, error) {
	var byPath map[string]map[string]struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &byPath); err != nil {
		return nil, fmt.Errorf("unreadable ansible-galaxy output: %w", err)
	}
	rank := func(p string) int {
		for i, d := range order {
			if p == d || p == filepath.Join(d, "ansible_collections") {
				return i
			}
		}
		return len(order)
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		if ri, rj := rank(paths[i]), rank(paths[j]); ri != rj {
			return ri < rj
		}
		return paths[i] < paths[j]
	})
	out := map[string]string{}
	for _, p := range paths {
		for coll, c := range byPath[p] {
			if _, seen := out[coll]; !seen && c.Version != "" && c.Version != "*" {
				out[coll] = c.Version
			}
		}
	}
	return out, nil
}

// FindRequirements reads the project's requirements.yml (or collections/
// requirements.yml), looking next to the playbook and then in the working
// directory. nil, "" if there is none.
func FindRequirements(playbookDir, cwd string) (*Requirements, string, error) {
	seen := map[string]bool{}
	for _, dir := range []string{playbookDir, cwd} {
		for _, rel := range []string{"requirements.yml", filepath.Join("collections", "requirements.yml")} {
			path := filepath.Join(dir, rel)
			if seen[path] {
				continue
			}
			seen[path] = true
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			req, err := ParseRequirements(data)
			if err != nil {
				return nil, "", fmt.Errorf("%s: %w", path, err)
			}
			return req, displayPath(path, cwd), nil
		}
	}
	return nil, "", nil
}

// displayPath is path relative to cwd when it lies under it.
func displayPath(path, cwd string) string {
	if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}
