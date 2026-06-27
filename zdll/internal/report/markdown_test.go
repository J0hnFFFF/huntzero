package report

import "testing"

func TestSanitizeMarkdownFences(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"no fences here", "no fences here"},
		{"```typescript\ncode\n```", "~~~typescript\ncode\n~~~"},
		{"```` four ticks ````", "~~~~ four ticks ~~~~"},
		{"inline ``double`` stays", "inline ``double`` stays"},
		{"inline ```triple``` replaced", "inline ~~~triple~~~ replaced"},
	}

	for _, c := range cases {
		got := sanitizeMarkdownFences(c.in)
		if got != c.want {
			t.Fatalf("sanitizeMarkdownFences(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
