package core

import (
	"testing"
)

func TestParseTraceTarget(t *testing.T) {
	cases := []struct {
		input string
		want  *Location
	}{
		{"", nil},
		{"  ", nil},
		{"src/auth.go:42", &Location{File: "src/auth.go", Line: 42}},
		{"/abs/path/file.py:10", &Location{File: "/abs/path/file.py", Line: 10}},
		{"src/utils.go", &Location{File: "src/utils.go", Line: 0}},
		{"functionNameOnly", nil},
	}

	for _, c := range cases {
		got := parseTraceTarget(c.input)
		if c.want == nil {
			if got != nil {
				t.Errorf("%q: expected nil, got %+v", c.input, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("%q: expected %+v, got nil", c.input, c.want)
			continue
		}
		if got.File != c.want.File || got.Line != c.want.Line {
			t.Errorf("%q: got %+v, want %+v", c.input, got, c.want)
		}
	}
}
