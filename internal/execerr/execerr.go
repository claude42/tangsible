// Package execerr turns Go's own os/exec error text - unreadable to
// anyone who doesn't already know exec.Cmd's internals, e.g. `exec:
// "ansible-playbook": executable file not found in $PATH` - into a
// message that actually says what's wrong, for every place tangsible
// shells out to an ansible tool. Deliberately narrow: only the "binary
// isn't on PATH at all" case (exec.ErrNotFound) is special-cased: it's
// the one failure mode a user setting tangsible up for the first time is
// actually likely to hit, and every other failure (wrong args, a real
// ansible-playbook error, permission denied, ...) already carries its
// own useful text via stderr or err.Error(), which callers keep using
// unchanged.
package execerr

import (
	"errors"
	"fmt"
	"os/exec"
)

// NotFoundMessage returns a friendly one-line explanation of err if it's
// exec.ErrNotFound, or "" otherwise (including err == nil) - callers fall
// back to their own existing stderr/err.Error() handling when it's empty.
func NotFoundMessage(binary string, err error) string {
	if err == nil || !errors.Is(err, exec.ErrNotFound) {
		return ""
	}
	return fmt.Sprintf("%s not found: tangsible requires %s to be installed and on your PATH", binary, binary)
}

// Fallback picks the message a caller should show when a subprocess it
// just ran produced no stderr output of its own - the case
// exec.ErrNotFound always falls into, since the child process never
// actually started to write any. Prefers NotFoundMessage over err's own
// cryptic exec.Error text; returns "" if err is nil.
func Fallback(binary string, err error) string {
	if msg := NotFoundMessage(binary, err); msg != "" {
		return msg
	}
	if err == nil {
		return ""
	}
	return err.Error()
}
