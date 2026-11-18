package eeverify

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// TagPrefix is the repository of the throw-away images; localhost/ keeps
// podman from ever trying to pull one.
const TagPrefix = "localhost/tangsible-ee-verify:"

// NewTag is a fresh throw-away image name.
func NewTag() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return TagPrefix + "verify"
	}
	return TagPrefix + hex.EncodeToString(b[:])
}

// BuildArgs is the ansible-builder command line that builds file into the
// image tag, with context as its build context. The tag given here replaces
// the definition's own options.tags (the command line wins), so an image the
// user named is never created or overwritten. network is the
// [execution_environment] network ("" for the engine's default) and also the
// one the build uses.
//
// By default podman's build cache is used and kept: the stage images that
// outlive the tag (untagged, hundreds of MB each) make a second build of the
// same or a slightly changed definition about twice as fast. noCache passes
// --layers=false, which makes podman keep no intermediate images and so use
// no cache either: a fresh build, and removing the tag removes everything.
// Never --prune-images, which prunes every dangling image the user has.
func BuildArgs(file, context, tag, network string, noCache bool) []string {
	var extra []string
	if noCache {
		extra = append(extra, "--layers=false")
	}
	if network != "" {
		extra = append(extra, "--network="+network)
	}
	args := []string{"build", "-f", file, "-c", context, "-t", tag, "--container-runtime", "podman"}
	if len(extra) > 0 {
		args = append(args, "--extra-build-cli-args="+strings.Join(extra, " "))
	}
	return args
}
