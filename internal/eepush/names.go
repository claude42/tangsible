// Package eepush is `tangsible ee push` (design-docs/EEInit.md, "Pushing an
// image"): it pushes the names a definition lists with podman push, after a
// check and a confirmation. Credentials are podman's own (podman login);
// nothing here reads or stores one. The name checks, the summary and the hint
// are pure; run.go is the verb.
package eepush

import (
	"fmt"
	"strings"

	"code.aw.net/claude/tangsible/internal/eebuild"
)

// Registry is the registry part of an image name: the first path component
// when there is a path after it and it looks like a host (has a "." or a ":",
// or is "localhost"). "" when the name has none - podman then stores it under
// localhost/.
func Registry(name string) string {
	first, _, ok := strings.Cut(name, "/")
	if !ok {
		return ""
	}
	if strings.ContainsAny(first, ".:") || first == "localhost" {
		return first
	}
	return ""
}

// Local is whether name is a local-only name: one with no registry, which
// podman keeps as localhost/<name>. A definition lists such names on purpose
// (short names to run with, next to the registry's), so push leaves them out
// instead of failing on them.
func Local(name string) bool { return Registry(name) == "" }

// Problem is what is wrong with name for pushing, "" when nothing: a name that
// spells out localhost/ (the local storage itself, which cannot be pushed to; a
// "localhost:5000" registry is a real one). A name with no registry is not a
// problem, it is Local.
func Problem(name string) string {
	if Registry(name) == "localhost" {
		return fmt.Sprintf("%s is in local storage (localhost/), which cannot be pushed; use a registry name or drop it from options.tags", name)
	}
	return ""
}

// Image is a name with the image it points at.
type Image struct {
	Name string
	ID   string // 12 characters
	Size int64
}

// Summary is what the confirmation shows: the names per registry, in order of
// first appearance, with the image behind each; a name whose image already
// appeared above it is marked "(same image)". The question itself is not part
// of it. skipped are the local-only names left out; they are listed after the
// pushes.
func Summary(images []Image, skipped []string) string {
	var registries []string
	byRegistry := map[string][]Image{}
	width := 0
	for _, im := range images {
		r := Registry(im.Name)
		if _, seen := byRegistry[r]; !seen {
			registries = append(registries, r)
		}
		byRegistry[r] = append(byRegistry[r], im)
		width = max(width, len(im.Name))
	}
	var b strings.Builder
	noun := "names"
	if len(images) == 1 {
		noun = "name"
	}
	if len(registries) == 1 {
		fmt.Fprintf(&b, "Push %d %s to %s?\n", len(images), noun, registries[0])
	} else {
		fmt.Fprintf(&b, "Push %d %s to %d registries?\n", len(images), noun, len(registries))
	}
	shown := map[string]bool{}
	for _, r := range registries {
		indent := "  "
		if len(registries) > 1 {
			fmt.Fprintf(&b, "  %s\n", r)
			indent = "    "
		}
		for _, im := range byRegistry[r] {
			detail := fmt.Sprintf("image %s, %s", im.ID, eebuild.HumanSize(im.Size))
			if shown[im.ID] {
				detail = "(same image)"
			}
			shown[im.ID] = true
			fmt.Fprintf(&b, "%s%-*s  %s\n", indent, width, im.Name, detail)
		}
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&b, "Not pushed (no registry, local only): %s\n", strings.Join(skipped, ", "))
	}
	return b.String()
}

// Accepts is whether the answer to the confirmation is a yes: "y" or "yes",
// any case. Anything else, an empty answer included, is a no.
func Accepts(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}
