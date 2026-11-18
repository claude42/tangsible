package ee

import "strings"

// BecomeFailure reports whether output, a run's collected stdout and stderr,
// holds ansible's become failure for a password sudo could not get (ansible-core
// 2.21, reproduced in a container): "Duplicate become password prompt" when a
// password was sent and refused, "Premature end of stream waiting for become
// success" together with sudo's "a password is required" when none was.
func BecomeFailure(output string) bool {
	if strings.Contains(output, "Duplicate become password prompt") {
		return true
	}
	return strings.Contains(output, "Premature end of stream waiting for become success") &&
		strings.Contains(output, "a password is required")
}

// BecomeHint is the message for a become failure in a container that runs as
// the host user: that user has no sudo password there
// (design-docs/ExecutionEnvironments.md, "The container user"). It cannot
// tell a task on the controller from one on a managed host whose password
// was really wrong, so it says "if".
const BecomeHint = "a task failed at sudo. If it runs on the controller (localhost), note that in the container " +
	"your user has no sudo password (the community images grant sudo only to root and %wheel), so become: true cannot work there. " +
	"Options: drop become for tasks that run on localhost; set run_as_root = true under [execution_environment] " +
	"(become then works without a password, but the default ssh user becomes root); or add a NOPASSWD sudoers rule to the image"
