package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedActionPluginIsTheRealPlugin(t *testing.T) {
	onDisk, err := os.ReadFile(filepath.Join("..", "..", "callback", "action", actionPluginFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(embeddedActionPlugin) {
		t.Error("embedded action plugin differs from callback/action/" + actionPluginFile)
	}
}

func TestResolveActionPluginDirOverride(t *testing.T) {
	base := t.TempDir()
	t.Setenv("TANGSIBLE_CALLBACK_DIR", base)
	if _, err := ResolveActionPluginDir(); err == nil {
		t.Fatal("an override without action/tangsible_vars.py must be an error")
	}
	dir := filepath.Join(base, "action")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, actionPluginFile), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveActionPluginDir()
	if err != nil || got != dir {
		t.Fatalf("got %q, %v; want %q", got, err, dir)
	}
}

func TestActionPluginEnvUnionsUserValue(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "action")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, actionPluginFile), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANGSIBLE_CALLBACK_DIR", base)
	t.Setenv("ANSIBLE_ACTION_PLUGINS", "/their/actions")
	env, err := ActionPluginEnv()
	if err != nil || len(env) != 1 {
		t.Fatalf("got %v, %v", env, err)
	}
	if want := "ANSIBLE_ACTION_PLUGINS=" + dir + ":/their/actions"; env[0] != want {
		t.Errorf("got %q, want %q", env[0], want)
	}
	if strings.Contains(env[0], "\n") {
		t.Error("newline in env value")
	}
}
