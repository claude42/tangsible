package eebuild

import (
	"fmt"
	"strings"
	"time"
)

// Image is one built tag.
type Image struct {
	Tag  string
	ID   string
	Size int64
	// Replaced is the id of the image the tag named before this build, when
	// that was a different one: it is untagged now.
	Replaced string
}

// Result is what the build did.
type Result struct {
	LogPath     string
	Duration    time.Duration
	Interrupted bool
	// BuildErr is "" when the image was built; BuildTail the last lines of
	// the log.
	BuildErr  string
	BuildTail []string
	Images    []Image
}

// Render is the report and the exit code: 0 built, 1 failed, 130 interrupted.
func Render(r Result) (string, int) {
	var b strings.Builder
	if r.Interrupted {
		b.WriteString("Interrupted.\n")
		return b.String(), 130
	}
	if r.BuildErr != "" {
		fmt.Fprintf(&b, "Build: FAILED (%s)\n", r.BuildErr)
		for _, l := range LastLines(r.BuildTail, TailLines) {
			fmt.Fprintf(&b, "  %s\n", l)
		}
		if r.LogPath != "" {
			fmt.Fprintf(&b, "  full log: %s\n", r.LogPath)
		}
		return b.String(), 1
	}
	fmt.Fprintf(&b, "Built in %s:\n", r.Duration.Round(time.Second))
	replaced := false
	for _, im := range r.Images {
		fmt.Fprintf(&b, "  %s  %s  %s", im.Tag, im.ID, humanSize(im.Size))
		if im.Replaced != "" {
			replaced = true
			fmt.Fprintf(&b, "  (replaced %s)", im.Replaced)
		}
		b.WriteString("\n")
	}
	if replaced {
		b.WriteString("A replaced image is untagged now; `podman image prune` removes it.\n")
	}
	if len(r.Images) > 0 {
		fmt.Fprintf(&b, "To run in it: image = %q under [execution_environment].\n", r.Images[0].Tag)
	}
	return b.String(), 0
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", n>>10)
	}
}
