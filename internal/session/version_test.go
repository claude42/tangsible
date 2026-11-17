package session

import (
	"errors"
	"testing"
)

func TestFormatTool(t *testing.T) {
	for _, c := range []struct {
		name, out string
		err       error
		want      string
	}{
		{"ansible-builder", "3.2.0\n", nil, "ansible-builder (/x/t): 3.2.0"},
		{"podman", "podman version 5.4.2\n", nil, "podman (/x/t): podman version 5.4.2"},
		{"podman", "podman version 5.4.2\nextra\n", nil, "podman (/x/t): podman version 5.4.2"},
		{"podman", "", nil, "podman (/x/t): version unknown"},
		{"podman", "", errors.New("boom"), "podman (/x/t): could not run --version: boom"},
	} {
		if got := formatTool(c.name, "/x/t", c.out, c.err); got != c.want {
			t.Errorf("formatTool(%s, %q, %v) = %q, want %q", c.name, c.out, c.err, got, c.want)
		}
	}
}
