package chat

import (
	"reflect"
	"testing"
)

// filterPrefixFold matches case-insensitively, rune-safely, preserving the
// candidate's own spelling.
func TestFilterPrefixFold(t *testing.T) {
	list := []string{"Alice", "alexandra", "bob", "Wörld"}
	cases := []struct {
		prefix string
		want   []string
	}{
		{"al", []string{"Alice", "alexandra"}},
		{"AL", []string{"Alice", "alexandra"}},
		{"b", []string{"bob"}},
		{"wö", []string{"Wörld"}}, // multibyte prefix, case-folded
		{"z", nil},
		{"alexandra", []string{"alexandra"}}, // full-length match
		{"alexandraa", nil},                  // longer than any candidate
	}
	for _, tc := range cases {
		if got := filterPrefixFold(list, tc.prefix); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("filterPrefixFold(%q) = %v, want %v", tc.prefix, got, tc.want)
		}
	}
}

// Command completion offers slash commands only at the start of the line.
func TestCompletionsCommands(t *testing.T) {
	c := &Client{}
	if got := c.completions("/q", true); !reflect.DeepEqual(got, []string{"/quit"}) {
		t.Fatalf("completions(/q, lineStart) = %v, want [/quit]", got)
	}
	if got := c.completions("/q", false); got != nil {
		t.Fatalf("mid-line slash word must not complete, got %v", got)
	}
}
