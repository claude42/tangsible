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

package runner

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func pluginFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestEmbeddedPluginIsTheRealPlugin(t *testing.T) {
	if !strings.Contains(string(embeddedPlugin), "CALLBACK_NAME = \""+callbackPluginName+"\"") {
		t.Errorf("embedded plugin doesn't declare CALLBACK_NAME %q", callbackPluginName)
	}
	onDisk, err := os.ReadFile(filepath.Join("..", "..", "callback", callbackPluginFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(embeddedPlugin) {
		t.Error("embedded plugin differs from callback/" + callbackPluginFile)
	}
}

func TestExtractPluginWritesOnlyThePluginUnderACacheDir(t *testing.T) {
	cache := t.TempDir()
	dir, err := extractPlugin([]byte("plugin v1"), cache, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != filepath.Join(cache, "tangsible") || !strings.HasPrefix(filepath.Base(dir), "plugin-") {
		t.Errorf("dir = %q, want <cache>/tangsible/plugin-<hash>", dir)
	}
	if got := pluginFiles(t, dir); len(got) != 1 || got[0] != callbackPluginFile {
		t.Errorf("dir holds %v, want only %s (ansible loads every .py file)", got, callbackPluginFile)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, callbackPluginFile)); string(b) != "plugin v1" {
		t.Errorf("plugin content = %q", b)
	}
}

func TestExtractPluginLeavesACorrectFileAlone(t *testing.T) {
	cache := t.TempDir()
	dir, _ := extractPlugin([]byte("plugin v1"), cache, t.TempDir())
	file := filepath.Join(dir, callbackPluginFile)
	before, _ := os.Stat(file)
	if _, err := extractPlugin([]byte("plugin v1"), cache, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(file)
	if !os.SameFile(before, after) {
		t.Error("an up-to-date plugin file was rewritten")
	}
}

func TestExtractPluginRepairsAModifiedFile(t *testing.T) {
	cache := t.TempDir()
	dir, _ := extractPlugin([]byte("plugin v1"), cache, t.TempDir())
	file := filepath.Join(dir, callbackPluginFile)
	if err := os.WriteFile(file, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := extractPlugin([]byte("plugin v1"), cache, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); string(b) != "plugin v1" {
		t.Errorf("plugin content = %q, want it restored", b)
	}
}

func TestExtractPluginKeepsVersionsApart(t *testing.T) {
	cache := t.TempDir()
	v1, _ := extractPlugin([]byte("plugin v1"), cache, t.TempDir())
	v2, _ := extractPlugin([]byte("plugin v2"), cache, t.TempDir())
	if v1 == v2 {
		t.Fatal("two plugin versions share a directory")
	}
	for dir, want := range map[string]string{v1: "plugin v1", v2: "plugin v2"} {
		if b, _ := os.ReadFile(filepath.Join(dir, callbackPluginFile)); string(b) != want {
			t.Errorf("%s holds %q, want %q", dir, b, want)
		}
	}
}

func TestExtractPluginFallsBackToTempDir(t *testing.T) {
	// A file where the cache directory should be: unusable.
	blocker := filepath.Join(t.TempDir(), "cache")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	for name, cache := range map[string]string{"unwritable": blocker, "no home": ""} {
		dir, err := extractPlugin([]byte("plugin v1"), cache, tmp)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if filepath.Dir(dir) != tmp {
			t.Errorf("%s: dir = %q, want one below %s", name, dir, tmp)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, callbackPluginFile)); string(b) != "plugin v1" {
			t.Errorf("%s: plugin content = %q", name, b)
		}
	}
}

func TestExtractPluginErrorsWhenNothingIsWritable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := extractPlugin([]byte("plugin v1"), blocker, blocker); err == nil {
		t.Error("extractPlugin succeeded with nowhere to write")
	}
}

func TestExtractPluginConcurrentStartsAllSucceed(t *testing.T) {
	cache := t.TempDir()
	var wg sync.WaitGroup
	dirs := make([]string, 16)
	errs := make([]error, len(dirs))
	for i := range dirs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dirs[i], errs[i] = extractPlugin([]byte("plugin v1"), cache, t.TempDir())
		}()
	}
	wg.Wait()
	for i := range dirs {
		if errs[i] != nil || dirs[i] != dirs[0] {
			t.Fatalf("start %d: dir %q err %v", i, dirs[i], errs[i])
		}
	}
	if got := pluginFiles(t, dirs[0]); len(got) != 1 {
		t.Errorf("dir holds %v, want only the plugin", got)
	}
}

func TestResolveCallbackPluginDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TANGSIBLE_CALLBACK_DIR", dir)
	if _, err := ResolveCallbackPluginDir(); err == nil || !strings.Contains(err.Error(), dir) {
		t.Errorf("override without a plugin: err = %v, want one naming %s", err, dir)
	}
	if err := os.WriteFile(filepath.Join(dir, callbackPluginFile), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ResolveCallbackPluginDir(); err != nil || got != dir {
		t.Errorf("override with a plugin: got %q, %v, want %q", got, err, dir)
	}
}

func TestResolveCallbackPluginDirExtractsTheEmbeddedPlugin(t *testing.T) {
	t.Setenv("TANGSIBLE_CALLBACK_DIR", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	// The memo outlives this test's cache dir otherwise.
	resetMemo := func() { extractedDir.once = sync.Once{}; extractedDir.dir, extractedDir.err = "", nil }
	resetMemo()
	t.Cleanup(resetMemo)
	dir, err := ResolveCallbackPluginDir()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, callbackPluginFile)); string(b) != string(embeddedPlugin) {
		t.Error("extracted plugin differs from the embedded one")
	}
}
