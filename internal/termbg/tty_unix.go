// Copyright 2026 Klaus Wissmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package termbg

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// ttySource reads the terminal's replies from stdin.
type ttySource struct{ fd int }

func (s ttySource) read(timeout time.Duration) ([]byte, error) {
	for {
		fds := []unix.PollFd{{Fd: int32(s.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(timeout.Milliseconds())+1)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n == 0 {
			return nil, err
		}
		buf := make([]byte, 256)
		m, err := unix.Read(s.fd, buf)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, err
		}
		return buf[:m], nil
	}
}

// openAndAsk puts the terminal into no-echo, non-canonical mode, sends the
// query and starts the goroutine that collects the answer into q (and
// restores the terminal). False if stdin/stdout aren't a terminal that
// should be asked. Output post-processing and signal generation stay on,
// so other start-up output still renders normally and Ctrl-C still works -
// the latter restores the terminal first.
func openAndAsk(q *Query) bool {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) || !term.IsTerminal(out) {
		return false
	}
	if t := os.Getenv("TERM"); t == "" || t == "dumb" {
		return false
	}
	old, err := unix.IoctlGetTermios(in, ioctlGetTermios)
	if err != nil {
		return false
	}
	raw := *old
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(in, ioctlSetTermios, &raw); err != nil {
		return false
	}
	restore := func() { _ = unix.IoctlSetTermios(in, ioctlSetTermios, old) }
	if _, err := os.Stdout.WriteString(queryString); err != nil {
		restore()
		return false
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	result := make(chan Result, 1)
	go func() { result <- detect(ttySource{in}, replyTimeout) }()
	go func() {
		select {
		case r := <-result:
			restore()
			signal.Stop(sigc)
			q.done <- finish(r)
		case sig := <-sigc:
			// Don't leave the terminal without echo: restore, then deliver
			// the signal as if nothing had intercepted it.
			restore()
			signal.Stop(sigc)
			signal.Reset(sig)
			_ = syscall.Kill(os.Getpid(), sig.(syscall.Signal))
		}
	}()
	return true
}
