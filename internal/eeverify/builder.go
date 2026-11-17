package eeverify

import (
	"crypto/rand"
	"encoding/hex"
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
// --layers=false makes podman keep no intermediate images: after a successful
// build the only new image is the tag, so removing it removes everything. (The
// default keeps stage images that outlive the tag; they would have to be found
// by comparing podman's image list before and after.) Nor --prune-images,
// which prunes every dangling image the user has.
func BuildArgs(file, context, tag, network string) []string {
	extra := "--layers=false"
	if network != "" {
		extra += " --network=" + network
	}
	return []string{"build", "-f", file, "-c", context, "-t", tag, "--container-runtime", "podman", "--extra-build-cli-args=" + extra}
}
