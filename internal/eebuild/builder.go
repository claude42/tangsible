package eebuild

import "strings"

// BuildArgs is the ansible-builder command line that builds file into the
// image tags, with context as its build context. Tags given here replace the
// definition's own options.tags (the command line wins), so `ee verify` can
// build into a name of its own; none leaves the definition's. network is the
// [execution_environment] network ("" for the engine's default) and also the
// one the build uses.
//
// ansible-builder takes several names after one -t (-t TAG [TAG ...]).
// ShowOutput and the status line (progress.go) read what -v 3 makes it print.
//
// By default podman's build cache is used and kept: the stage images that
// outlive the tag (untagged, hundreds of MB each) make a second build of the
// same or a slightly changed definition about twice as fast. noCache passes
// --layers=false, which makes podman keep no intermediate images and so use
// no cache either: a fresh build, and removing the tag removes everything.
// Never --prune-images, which prunes every dangling image the user has.
func BuildArgs(file, context string, tags []string, network string, noCache bool) []string {
	var extra []string
	if noCache {
		extra = append(extra, "--layers=false")
	}
	if network != "" {
		extra = append(extra, "--network="+network)
	}
	args := []string{"build", "-f", file, "-c", context}
	if len(tags) > 0 {
		args = append(args, "-t")
		args = append(args, tags...)
	}
	// -v 3: at its default verbosity ansible-builder prints nothing of the
	// build, not even on success; at 3 it streams podman's output.
	args = append(args, "--container-runtime", "podman", "-v", "3")
	if len(extra) > 0 {
		args = append(args, "--extra-build-cli-args="+strings.Join(extra, " "))
	}
	return args
}
