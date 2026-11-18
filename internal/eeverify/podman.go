package eeverify

import (
	"os/exec"
	"sort"
	"strings"
)

// Snapshot is what podman held before a build: its containers (including the
// "external" ones buildah makes while building) and its images with their
// repository names. A successful build needs no more cleanup than removing
// its tag; the untagged stage images stay as podman's build cache on purpose
// (BuildArgs). One that fails or is killed leaves working containers and a
// buildah "-tmp" image behind (podman does not clean up after SIGINT or SIGTERM
// either), and the containers pin the images; their names are not ours to
// know, so Cleanup removes exactly what appeared since the snapshot - never
// anything that was already there.
type Snapshot struct {
	Containers map[string]bool
	// Images maps an image id to its repository ("<none>" if untagged).
	Images map[string]string
}

// TakeSnapshot reads podman's current containers and images; what it can't
// read is simply absent (a later Cleanup then removes less, never more).
func TakeSnapshot() Snapshot {
	s := Snapshot{Containers: map[string]bool{}, Images: map[string]string{}}
	for _, id := range podmanLines("ps", "-a", "--external", "-q") {
		s.Containers[id] = true
	}
	f := podmanLines("images", "-a", "--format", "{{.ID}} {{.Repository}}")
	for i := 0; i+1 < len(f); i += 2 {
		s.Images[f[i]] = f[i+1]
	}
	return s
}

func podmanLines(args ...string) []string {
	out, err := exec.Command("podman", args...).Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

// NewSince returns the ids in after that are not in before, sorted.
func NewSince[V any](before, after map[string]V) []string {
	var out []string
	for id := range after {
		if _, seen := before[id]; !seen {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// leftoverImages are the images a failed build left: those new since before
// that are buildah's "-tmp" images. Untagged ones are finished stages, the
// build cache, and an image with a real repository name is one the build had
// to pull (its base); the user may want to keep both.
func leftoverImages(before, after map[string]string) []string {
	var out []string
	for _, id := range NewSince(before, after) {
		if repo := after[id]; strings.HasSuffix(repo, "-tmp") {
			out = append(out, id)
		}
	}
	return out
}

// Cleanup removes what the build created. The image tag always goes, unless
// keepTag; if the build did not succeed, also the containers that are new
// since the snapshot and then the images it left (leftoverImages). All best
// effort.
func (before Snapshot) Cleanup(tag string, buildOK, keepTag bool) {
	if !buildOK {
		after := TakeSnapshot()
		if ids := NewSince(before.Containers, after.Containers); len(ids) > 0 {
			_ = exec.Command("podman", append([]string{"rm", "-f"}, ids...)...).Run()
		}
	}
	if !keepTag {
		_ = exec.Command("podman", "rmi", "-f", tag).Run()
	}
	if !buildOK {
		if ids := leftoverImages(before.Images, TakeSnapshot().Images); len(ids) > 0 {
			_ = exec.Command("podman", append([]string{"rmi", "-f"}, ids...)...).Run()
		}
	}
}
