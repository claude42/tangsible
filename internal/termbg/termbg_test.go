package termbg

import (
	"errors"
	"testing"
	"time"
)

func TestParseOSC11(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		r, g, b float64
		ok      bool
	}{
		{"4 digit, BEL", "\x1b]11;rgb:ffff/ffff/ffff\a", 1, 1, 1, true},
		{"4 digit, ST", "\x1b]11;rgb:0000/0000/0000\x1b\\", 0, 0, 0, true},
		{"2 digit", "\x1b]11;rgb:ff/00/80\a", 1, 0, 128.0 / 255, true},
		{"1 digit", "\x1b]11;rgb:f/0/0\a", 1, 0, 0, true},
		{"rgba ignores alpha", "\x1b]11;rgba:0000/ffff/0000/8000\a", 0, 1, 0, true},
		{"preceded by noise", "xx\x1b]11;rgb:0000/0000/0000\a\x1b[?62c", 0, 0, 0, true},
		{"unterminated", "\x1b]11;rgb:0000/0000/0000", 0, 0, 0, false},
		{"hash form unsupported", "\x1b]11;#000000\a", 0, 0, 0, false},
		{"bad hex", "\x1b]11;rgb:zz/00/00\a", 0, 0, 0, false},
		{"too few components", "\x1b]11;rgb:00/00\a", 0, 0, 0, false},
		{"5 digit component", "\x1b]11;rgb:fffff/00/00\a", 0, 0, 0, false},
		{"no OSC at all", "\x1b[?62;4c", 0, 0, 0, false},
	}
	for _, tc := range tests {
		r, g, b, ok := parseOSC11([]byte(tc.in))
		if ok != tc.ok || (ok && (r != tc.r || g != tc.g || b != tc.b)) {
			t.Errorf("%s: got (%v,%v,%v,%v), want (%v,%v,%v,%v)", tc.name, r, g, b, ok, tc.r, tc.g, tc.b, tc.ok)
		}
	}
}

func TestFromColorFGBG(t *testing.T) {
	tests := map[string]Result{
		"":             {},
		"15;0":         {Light: false, OK: true},
		"0;15":         {Light: true, OK: true},
		"0;7":          {Light: true, OK: true},
		"15;8":         {Light: false, OK: true},
		"7;default;0":  {Light: false, OK: true},
		"0;default;15": {Light: true, OK: true},
		"0;default":    {},
		"abc":          {},
		"0;99":         {},
	}
	for in, want := range tests {
		if got := fromColorFGBG(in); got != want {
			t.Errorf("%q: got %+v, want %+v", in, got, want)
		}
	}
}

// fakeSource delivers its chunks one read at a time, then times out.
type fakeSource struct {
	chunks []string
	err    error
}

func (f *fakeSource) read(time.Duration) ([]byte, error) {
	if len(f.chunks) == 0 {
		if f.err != nil {
			return nil, f.err
		}
		time.Sleep(5 * time.Millisecond)
		return nil, nil
	}
	c := f.chunks[0]
	f.chunks = f.chunks[1:]
	return []byte(c), nil
}

func TestDetect(t *testing.T) {
	dark := "\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\"
	light := "\x1b]11;rgb:ffff/ffff/f8f8\a"
	da1 := "\x1b[?62;4c"

	tests := []struct {
		name   string
		chunks []string
		want   Result
	}{
		{"dark", []string{dark + da1}, Result{Light: false, OK: true}},
		{"light", []string{light + da1}, Result{Light: true, OK: true}},
		{"reply split across reads", []string{dark[:9], dark[9:] + da1[:3], da1[3:]}, Result{Light: false, OK: true}},
		{"no OSC 11 support: DA1 alone ends the wait", []string{da1}, Result{}},
	}
	for _, tc := range tests {
		start := time.Now()
		if got := detect(&fakeSource{chunks: tc.chunks}, time.Second); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
		if time.Since(start) > 200*time.Millisecond {
			t.Errorf("%s: should not have waited for the timeout", tc.name)
		}
	}

	// A terminal that answers nothing: bounded by the timeout.
	start := time.Now()
	if got := detect(&fakeSource{}, 50*time.Millisecond); got != (Result{}) {
		t.Errorf("silent terminal: got %+v", got)
	}
	if d := time.Since(start); d < 40*time.Millisecond || d > 500*time.Millisecond {
		t.Errorf("silent terminal: waited %v, want about 50ms", d)
	}

	// Color answered but DA1 never did: the color is still used at timeout.
	if got := detect(&fakeSource{chunks: []string{light}}, 50*time.Millisecond); got != (Result{Light: true, OK: true}) {
		t.Errorf("color without DA1: got %+v", got)
	}

	// A read error ends the wait.
	if got := detect(&fakeSource{err: errors.New("boom")}, time.Second); got != (Result{}) {
		t.Errorf("read error: got %+v", got)
	}
}

func TestNilQueryWait(t *testing.T) {
	var q *Query
	if got := q.Wait(); got != (Result{}) {
		t.Errorf("got %+v", got)
	}
}
