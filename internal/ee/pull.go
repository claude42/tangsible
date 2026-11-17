package ee

import (
	"fmt"
	"strings"
)

// Pull policies for the execution environment image (pull_policy), after
// ansible-navigator's: when to ask the registry for the image before a run.
const (
	PullMissing = "missing" // only when it isn't available locally (the default)
	PullAlways  = "always"  // refresh it at the start of every session
	PullNever   = "never"   // never; an absent image is an error
	PullTag     = "tag"     // refresh a ":latest" (or untagged) image, otherwise like missing
)

// PullPolicies lists the accepted values.
var PullPolicies = []string{PullAlways, PullMissing, PullNever, PullTag}

// ImageTag returns the tag of an image reference: "latest" when it has none,
// "" when it is pinned by digest (a tag-less, immutable reference). A port in
// the registry part ("host:5000/img") is not a tag.
func ImageTag(image string) string {
	if strings.Contains(image, "@") {
		return ""
	}
	name := image[strings.LastIndex(image, "/")+1:]
	if i := strings.LastIndex(name, ":"); i >= 0 {
		return name[i+1:]
	}
	return "latest"
}

// LocalOnly reports whether an image reference lives in the engine's local
// "localhost" namespace (localhost/name:tag, what a local build or a loaded
// archive is called): it has no registry, so there is nothing to pull.
// "localhost:5000/..." is a registry on a port and can be pulled from.
func LocalOnly(image string) bool {
	return strings.HasPrefix(image, "localhost/")
}

// PullDecision is what DecidePull concludes.
type PullDecision struct {
	// Pull is whether to run a pull now.
	Pull bool
	// Optional is true when the image is already present, so a failing
	// pull (offline, registry down) is no reason to stop.
	Optional bool
	// Reason is shown with the "pulling ..." line.
	Reason string
	// Err is a reason to stop: nothing can be pulled and the image is not here.
	Err error
}

// DecidePull applies a pull policy. present is whether the image is in local
// storage; refreshedBefore whether this process already refreshed it (a
// refreshing policy acts once per session, not before every re-run). An
// unknown or empty policy means PullMissing.
func DecidePull(policy, image string, present, refreshedBefore bool) PullDecision {
	if LocalOnly(image) {
		if present {
			return PullDecision{}
		}
		return PullDecision{Err: fmt.Errorf("the image %s is not available locally, and images under localhost/ exist only locally (there is no registry to pull from)\nhint: build it (podman build -t %s ...) or load it (podman load -i FILE)", image, strings.TrimPrefix(image, "localhost/"))}
	}
	if policy == PullNever {
		if present {
			return PullDecision{}
		}
		return PullDecision{Err: fmt.Errorf("pull_policy is %q and the image %s is not available locally\nhint: pull it yourself (podman pull %s) or set pull_policy = %q", PullNever, image, image, PullMissing)}
	}
	if !present {
		return PullDecision{Pull: true, Reason: "not available locally"}
	}
	switch policy {
	case PullAlways:
		if !refreshedBefore {
			return PullDecision{Pull: true, Optional: true, Reason: "pull_policy = always"}
		}
	case PullTag:
		if !refreshedBefore && ImageTag(image) == "latest" {
			return PullDecision{Pull: true, Optional: true, Reason: `pull_policy = tag, tag "latest"`}
		}
	}
	return PullDecision{}
}

// ExpandPath expands $NAME/${NAME} from env and a leading "~" to home, as
// Ansible does for a path option on the host (see expandPath); for a
// caller outside the builder, e.g. an argument of the engine's pull.
func ExpandPath(p, home string, env []string) string {
	return expandPath(p, home, envMap(env))
}
