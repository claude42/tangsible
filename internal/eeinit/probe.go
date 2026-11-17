package eeinit

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"code.aw.net/claude/tangsible/internal/execerr"
)

// ProbeBase runs ProbeScript in image with the engine and returns what it
// found. The container gets no network and is removed afterwards.
func ProbeBase(engine, image string) (BaseFacts, error) {
	cmd := exec.Command(engine, "run", "--rm", "--network=none", "--entrypoint", "/bin/sh", image, "-c", ProbeScript)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := execerr.NotFoundMessage(engine, err); msg != "" {
			return BaseFacts{}, errors.New(msg)
		}
		return BaseFacts{}, fmt.Errorf("couldn't inspect %s: %v: %s", image, err, strings.TrimSpace(stderr.String()))
	}
	return ParseProbe(stdout.String()), nil
}
