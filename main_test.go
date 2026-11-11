package main

import (
	"strings"
	"testing"
)

func TestHookSession(t *testing.T) {
	for in, want := range map[string]string{
		`{"session_id":"5f1c2b9e-0a1d-4c3e-9f00-1234567890ab","hook_event_name":"Stop"}`: "5f1c2b9e-0a1d-4c3e-9f00-1234567890ab",
		``:            "",
		`not json`:    "",
		`{"other":1}`: "",
	} {
		if got := hookSession(strings.NewReader(in)); got != want {
			t.Errorf("hookSession(%q) = %q, want %q", in, got, want)
		}
	}
}
