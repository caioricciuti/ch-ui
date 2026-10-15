package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestNoArgsCommand(t *testing.T) {
	cases := map[string]string{
		"windows": "desktop",
		"linux":   "server",
		"darwin":  "server",
		"freebsd": "server",
	}
	for goos, want := range cases {
		if got := noArgsCommand(goos); got != want {
			t.Errorf("noArgsCommand(%q) = %q, want %q", goos, got, want)
		}
	}
}

func TestNoArgsCommandIsRegistered(t *testing.T) {
	for _, goos := range []string{"windows", "linux"} {
		name := noArgsCommand(goos)
		found, _, err := rootCmd.Find([]string{name})
		if err != nil || found.Name() != name {
			t.Errorf("no-args command %q for %s is not a registered command", name, goos)
		}
	}
}

func TestWaitForEnter(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("\nleft over")
	waitForEnter(in, &out)
	if !strings.Contains(out.String(), "Press Enter") {
		t.Fatalf("prompt = %q", out.String())
	}
	// A closed stdin (no console) must not block.
	waitForEnter(strings.NewReader(""), &out)
}
