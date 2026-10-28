package uikit

import "testing"

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestTerminalProgressSupported(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"empty", nil, false},
		{"term features P", map[string]string{"TERM_FEATURES": "MB2PSx"}, true},
		{"term features without P", map[string]string{"TERM_FEATURES": "MB2Sx"}, false},
		{"term features Pa is another feature", map[string]string{"TERM_FEATURES": "MPa"}, false},
		{"wezterm", map[string]string{"TERM_PROGRAM": "WezTerm"}, true},
		{"ghostty", map[string]string{"TERM_PROGRAM": "ghostty"}, true},
		{"tmux", map[string]string{"TERM_PROGRAM": "tmux"}, false},
		{"windows terminal", map[string]string{"WT_SESSION": "abc"}, true},
		{"conemu", map[string]string{"ConEmuANSI": "ON"}, true},
		{"conemu off", map[string]string{"ConEmuANSI": "OFF"}, false},
		{"iterm over ssh", map[string]string{"LC_TERMINAL": "iTerm2", "LC_TERMINAL_VERSION": "3.7.3"}, true},
		{"iterm 3.6.6beta", map[string]string{"LC_TERMINAL": "iTerm2", "LC_TERMINAL_VERSION": "3.6.6beta2"}, true},
		{"iterm too old", map[string]string{"LC_TERMINAL": "iTerm2", "LC_TERMINAL_VERSION": "3.5.11"}, false},
		{"iterm no version", map[string]string{"LC_TERMINAL": "iTerm2"}, false},
		{"ptyxis 48", map[string]string{"PTYXIS_VERSION": "48.1"}, true},
		{"ptyxis 47", map[string]string{"PTYXIS_VERSION": "47.9"}, false},
		{"ptyxis junk", map[string]string{"PTYXIS_VERSION": "x"}, false},
	}
	for _, tc := range tests {
		if got := TerminalProgressSupported(envOf(tc.env)); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestComputeTerminalProgress(t *testing.T) {
	tests := []struct {
		name                                  string
		started, pending, frozen, failed, pau bool
		pos, total                            int
		want                                  TerminalProgress
	}{
		{"not started", false, false, false, false, false, 0, 5, TerminalProgress{State: TerminalProgressHidden}},
		{"probe pending", true, true, false, false, false, 0, 0, TerminalProgress{State: TerminalProgressHidden}},
		{"running", true, false, false, false, false, 2, 4, TerminalProgress{TerminalProgressNormal, 50}},
		{"no skeleton", true, false, false, false, false, 0, 0, TerminalProgress{State: TerminalProgressIndeterminate}},
		{"prediction exhausted", true, false, false, false, false, 4, 4, TerminalProgress{State: TerminalProgressIndeterminate}},
		{"paused", true, false, false, false, true, 1, 4, TerminalProgress{TerminalProgressPaused, 25}},
		{"finished ok", true, false, true, false, false, 4, 4, TerminalProgress{State: TerminalProgressHidden}},
		{"finished failed", true, false, true, true, false, 2, 4, TerminalProgress{TerminalProgressError, 100}},
	}
	for _, tc := range tests {
		if got := ComputeTerminalProgress(tc.started, tc.pending, tc.frozen, tc.failed, tc.pau, tc.pos, tc.total); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestTerminalProgressSequence(t *testing.T) {
	if got, want := (TerminalProgress{TerminalProgressNormal, 42}).Sequence(), "\x1b]9;4;1;42\x1b\\"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := (TerminalProgress{TerminalProgressNormal, 150}).Sequence(), "\x1b]9;4;1;100\x1b\\"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := (TerminalProgress{State: TerminalProgressHidden}).Sequence(), "\x1b]9;4;0;0\x1b\\"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
