package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bgrewell/stencil"
)

// The command tree is wiring, and wiring is exactly what a restructure breaks
// silently: a command that used to read a parent's persistent flag still
// compiles after the parent is gone, it just receives a zero value forever.
// These drive the real tree in-process to catch that.

func execute(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	app := stencil.NewApp(
		stencil.WithName("cleanslate"),
		stencil.WithRootCommand(newRootCmd()),
		stencil.WithIO(strings.NewReader(""), &out, &errOut),
	)
	code := app.Execute(args)
	return code, out.String() + errOut.String()
}

func TestCommandTreeIsFlat(t *testing.T) {
	want := map[string]bool{
		"status": true, "list": true, "checkpoint": true, "history": true,
		"rollback": true, "fork": true, "switch": true, "reset": true,
		"discard": true, "build": true, "install": true,
	}

	root := newRootCmd()
	got := map[string]bool{}
	for _, c := range root.Sub {
		got[c.Name] = true
		if len(c.Sub) != 0 {
			t.Errorf("%s has subcommands; the tree is meant to be flat", c.Name)
		}
	}

	for name := range want {
		if !got[name] {
			t.Errorf("missing command %q", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("unexpected command %q", name)
		}
	}
}

func TestStateGroupIsGone(t *testing.T) {
	code, _ := execute(t, "state", "list")
	if code != stencil.ExitUsage {
		t.Fatalf("`state list` should be a usage error now, got exit %d", code)
	}
}

// --no-bls never worked: stencil parses --no-<name> by stripping the prefix and
// looking up the bare name, so a flag declared as "no-bls" was unreachable and
// every use of it was a usage error. It is declared as "bls" now.
func TestNoBLSParses(t *testing.T) {
	code, out := execute(t, "discard", "--no-bls", "--force", "somename")
	if strings.Contains(out, "unknown flag") {
		t.Fatalf("--no-bls is not reachable: %s", out)
	}
	if code == stencil.ExitUsage {
		t.Fatalf("--no-bls produced a usage error: %s", out)
	}
}

// Every flag that used to be persistent on the `state` parent now has to be
// declared on each command that reads it. Reachability is asserted by driving
// the parser: a flag the command does not declare is a usage error, whereas
// one it declares gets as far as the runtime failure of doing the work.
func TestFlagsAreReachableAfterFlattening(t *testing.T) {
	cases := [][]string{
		{"list", "--fs-root", "/tmp"},
		{"status", "--fs-root", "/tmp"},
		{"history", "--fs-root", "/tmp"},
		{"checkpoint", "--fs-root", "/tmp", "--message", "note"},
		{"fork", "--fs-root", "/tmp", "--esp", "/tmp", "--from", "baseline", "newname"},
		{"fork", "--no-bls", "newname"},
		{"discard", "--fs-root", "/tmp", "--esp", "/tmp", "--force", "somename"},
		{"reset", "--fs-root", "/tmp", "--force", "somename"},
		{"rollback", "--fs-root", "/tmp", "1"},
		{"switch", "--esp", "/tmp", "--scratch", "somename"},
		{"build", "--config-dir", "/tmp", "--skip-relayout"},
		{"install", "--image", "/tmp/nonexistent.raw", "--force", "/tmp/target"},
	}

	for _, argv := range cases {
		code, out := execute(t, argv...)
		if code == stencil.ExitUsage || strings.Contains(out, "unknown flag") {
			t.Errorf("%v was rejected by the parser: %s", argv, out)
		}
	}
}

// A zero-value ArgSpec means "unlimited" in stencil, so every command that
// takes no arguments has to reject them explicitly or it silently accepts junk.
func TestNoArgCommandsRejectExtras(t *testing.T) {
	for _, cmd := range []string{"status", "list", "checkpoint", "build"} {
		code, out := execute(t, cmd, "unexpected")
		if code != stencil.ExitUsage && !strings.Contains(out, "takes no arguments") {
			t.Errorf("%s accepted a stray argument (exit %d): %s", cmd, code, out)
		}
	}
}

func TestArgCountsAreBounded(t *testing.T) {
	cases := []struct {
		args  []string
		usage bool
	}{
		{[]string{"fork"}, true},                   // name required
		{[]string{"fork", "a", "b"}, true},         // at most one
		{[]string{"rollback"}, true},               // checkpoint required
		{[]string{"discard"}, true},                // name required
		{[]string{"history", "a", "b"}, true},      // at most one
		{[]string{"reset", "a", "b"}, true},        // at most one
		{[]string{"switch", "a", "b"}, true},       // at most one
		{[]string{"install", "a", "b", "c"}, true}, // at most one
	}
	for _, tc := range cases {
		code, _ := execute(t, tc.args...)
		if tc.usage && code != stencil.ExitUsage {
			t.Errorf("%v should be a usage error, got exit %d", tc.args, code)
		}
	}
}
