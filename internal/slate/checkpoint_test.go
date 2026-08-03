package slate

import (
	"os"
	"path/filepath"
	"testing"
)

// The retention class lives in the subvolume name because the initramfs prunes
// checkpoints and must not depend on parsing a metadata file that could be
// missing. That makes the name grammar load-bearing.
func TestCheckpointNameGrammar(t *testing.T) {
	cases := []struct {
		subvol string
		slate  string
		seq    string
		class  string
		match  bool
	}{
		{"@main.ckpt.0001.auto", "main", "0001", "auto", true},
		{"@main.ckpt.0042.keep", "main", "0042", "keep", true},
		{"@pg-tuned-v2.ckpt.0007.auto", "pg-tuned-v2", "0007", "auto", true},
		{"@main", "", "", "", false},
		{"@baseline", "", "", "", false},
		{"@main.ckpt.0001", "", "", "", false},
		{"@main.ckpt.0001.other", "", "", "", false},
		{"@main.ckpt.abc.auto", "", "", "", false},
	}
	for _, tc := range cases {
		m := checkpointPattern.FindStringSubmatch(tc.subvol)
		if (m != nil) != tc.match {
			t.Errorf("%q: match = %v, want %v", tc.subvol, m != nil, tc.match)
			continue
		}
		if m != nil && (m[1] != tc.slate || m[2] != tc.seq || m[3] != tc.class) {
			t.Errorf("%q parsed as (%q,%q,%q)", tc.subvol, m[1], m[2], m[3])
		}
	}
}

// A slate must not be mistaken for one of its own checkpoints, or listing
// slates would show every rollback point as a slate.
func TestSlatesAreNotCheckpoints(t *testing.T) {
	for _, name := range []string{"@main", "@baseline", "@pg-tuned"} {
		if checkpointPattern.MatchString(name) {
			t.Errorf("%q was taken for a checkpoint", name)
		}
	}
}

func TestCheckpointRef(t *testing.T) {
	c := Checkpoint{Slate: "main", Seq: 7}
	if got := c.Ref(); got != "main.7" {
		t.Errorf("Ref() = %q, want main.7", got)
	}
}

func TestAutomatic(t *testing.T) {
	if !(Checkpoint{Class: ClassAuto}).Automatic() {
		t.Error("an auto checkpoint should be prunable")
	}
	if (Checkpoint{Class: ClassKeep}).Automatic() {
		t.Error("a kept checkpoint should never be prunable")
	}
}

func TestRetainAutoDefaultsWhenUnconfigured(t *testing.T) {
	fs := FsRootAt(t.TempDir())
	if got := RetainAuto(fs); got != defaultRetainAuto {
		t.Errorf("RetainAuto with no config = %d, want %d", got, defaultRetainAuto)
	}
}

func TestRetainAutoReadsConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, MetaDir), 0755); err != nil {
		t.Fatal(err)
	}
	body := "# machine settings\nretain_auto=3\n"
	if err := os.WriteFile(filepath.Join(dir, MetaDir, "config"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if got := RetainAuto(FsRootAt(dir)); got != 3 {
		t.Errorf("RetainAuto = %d, want 3", got)
	}
}

func TestPendingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fs := FsRootAt(dir)

	// StagePending refuses a source that does not exist, so make one.
	if err := os.MkdirAll(filepath.Join(dir, "@main.ckpt.0003.keep"), 0755); err != nil {
		t.Fatal(err)
	}

	want := Pending{Slate: "@main", Source: "@main.ckpt.0003.keep", Reason: "rollback to checkpoint main.3"}
	if err := StagePending(fs, want); err != nil {
		t.Fatal(err)
	}

	got, ok := ReadPending(fs)
	if !ok {
		t.Fatal("staged replacement did not read back")
	}
	if got != want {
		t.Errorf("read back %+v, want %+v", got, want)
	}

	if err := ClearPending(fs); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadPending(fs); ok {
		t.Error("cleared replacement is still present")
	}
}

func TestStagePendingRejectsMissingSource(t *testing.T) {
	fs := FsRootAt(t.TempDir())
	err := StagePending(fs, Pending{Slate: "@main", Source: "@nope"})
	if err == nil {
		t.Error("staging against a source that does not exist should fail")
	}
}

// The staged file is parsed by a shell script in the initramfs, so it has to
// stay one key=value per line with no quoting.
func TestPendingFileIsShellParseable(t *testing.T) {
	dir := t.TempDir()
	fs := FsRootAt(dir)
	if err := os.MkdirAll(filepath.Join(dir, "@baseline"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := StagePending(fs, Pending{Slate: "@main", Source: "@baseline", Reason: "reset\nto the baseline"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, MetaDir, pendingFile))
	if err != nil {
		t.Fatal(err)
	}
	want := "slate=@main\nsource=@baseline\nreason=reset to the baseline\n"
	if string(data) != want {
		t.Errorf("staged file is %q, want %q", data, want)
	}
}
