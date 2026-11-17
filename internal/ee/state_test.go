package ee

import (
	"reflect"
	"testing"
)

func TestStatePaths(t *testing.T) {
	cfg := func(origin string) map[string]StateSetting {
		return map[string]StateSetting{
			"CACHE_PLUGIN":            {"jsonfile", origin},
			"CACHE_PLUGIN_CONNECTION": {"/var/cache/facts", origin},
			"DEFAULT_LOG_PATH":        {"/var/log/ansible.log", origin},
		}
	}
	t.Run("configured paths outside the project", func(t *testing.T) {
		got, plugin := StatePaths(cfg("/proj/ansible.cfg"), "/proj", "/home/u", nil)
		want := []StatePath{
			{Path: "/var/cache/facts", Dir: true, Env: "ANSIBLE_CACHE_PLUGIN_CONNECTION"},
			{Path: "/var/log/ansible.log", Env: "ANSIBLE_LOG_PATH"},
		}
		if !reflect.DeepEqual(got, want) || plugin != "jsonfile" {
			t.Errorf("got %+v %q, want %+v", got, plugin, want)
		}
	})
	t.Run("defaults name nothing", func(t *testing.T) {
		if got, _ := StatePaths(cfg("default"), "/proj", "", nil); len(got) != 0 {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("inside the project needs nothing", func(t *testing.T) {
		s := map[string]StateSetting{
			"CACHE_PLUGIN":            {"jsonfile", "x"},
			"CACHE_PLUGIN_CONNECTION": {"facts", "x"},
			"DEFAULT_LOG_PATH":        {"/proj/logs/a.log", "x"},
		}
		if got, _ := StatePaths(s, "/proj", "", nil); len(got) != 0 {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("a sibling directory is outside, a relative path resolves from cwd", func(t *testing.T) {
		s := map[string]StateSetting{"DEFAULT_LOG_PATH": {"../projx/a.log", "x"}}
		got, _ := StatePaths(s, "/proj", "", nil)
		if len(got) != 1 || got[0].Path != "/projx/a.log" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("a cache that is not a directory on disk", func(t *testing.T) {
		s := cfg("x")
		s["CACHE_PLUGIN"] = StateSetting{"redis", "x"}
		got, plugin := StatePaths(s, "/proj", "", nil)
		if len(got) != 1 || got[0].Env != "ANSIBLE_LOG_PATH" || plugin != "" {
			t.Errorf("got %+v %q", got, plugin)
		}
	})
	t.Run("~ and variables expand", func(t *testing.T) {
		s := map[string]StateSetting{"DEFAULT_LOG_PATH": {"~/logs/$NAME.log", "x"}}
		got, _ := StatePaths(s, "/proj", "/home/u", []string{"NAME=a"})
		if len(got) != 1 || got[0].Path != "/home/u/logs/a.log" {
			t.Errorf("got %+v", got)
		}
	})
}
