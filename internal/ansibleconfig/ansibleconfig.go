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

// Package ansibleconfig runs `ansible-config dump --format json` (~250 ms
// of Python start-up) once and shares the result: the roles path
// (internal/rolepath) and the callbacks_enabled list (internal/runner)
// both come out of the same dump, and Prefetch lets session.Main start it
// in the background before either asks, so it overlaps other start-up work
// instead of blocking the first frame.
package ansibleconfig

import (
	"encoding/json"
	"os/exec"
	"sync"
	"time"
)

// Entry is one setting of the dump. Value stays raw: its shape depends on
// the setting.
type Entry struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

type fetch struct {
	done    chan struct{}
	entries []Entry
	at      time.Time // when it finished; zero while in flight
}

var (
	mu  sync.Mutex
	cur *fetch
)

// start launches a dump. Caller holds mu.
func start() *fetch {
	f := &fetch{done: make(chan struct{})}
	go func() {
		defer close(f.done)
		out, err := exec.Command("ansible-config", "dump", "--format", "json").Output()
		if err == nil {
			// One observed anomaly (GALAXY_SERVERS) is shaped as a bare
			// {"<key>": {...}} - decodes to a zero Entry, which never
			// matches a name a caller looks for.
			_ = json.Unmarshal(out, &f.entries)
		}
		f.at = time.Now()
	}()
	return f
}

// Prefetch starts a dump in the background unless one is already running
// or finished. Idempotent.
func Prefetch() {
	mu.Lock()
	defer mu.Unlock()
	if cur == nil {
		cur = start()
	}
}

// Get returns the dump, joining one still in flight, reusing a finished
// one no older than maxAge, and otherwise running a fresh one - so a later
// generation still notices an ansible.cfg edited mid-session. nil if
// ansible-config can't be run or its output can't be parsed: callers fall
// back to their own defaults, as they always did.
func Get(maxAge time.Duration) []Entry {
	mu.Lock()
	f := cur
	if f != nil {
		select {
		case <-f.done:
			if time.Since(f.at) > maxAge {
				f = nil
			}
		default: // in flight: join it
		}
	}
	if f == nil {
		f = start()
		cur = f
	}
	mu.Unlock()
	<-f.done
	return f.entries
}

// Find returns the value of the named setting.
func Find(entries []Entry, name string) (json.RawMessage, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e.Value, true
		}
	}
	return nil, false
}
