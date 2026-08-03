package slate

import (
	"os"
	"path/filepath"
	"testing"
)

// displayName and resolveSource are hand-maintained inverses of each other.
// The vocabulary change edits both, and nothing else would catch an edit that
// lands in only one — the result would be a name the tool prints but will not
// accept back.
func TestDisplayNameResolveSourceRoundTrip(t *testing.T) {
	fs := FsRootAt(t.TempDir())
	for _, subvol := range []string{BaselineSubvol, RuntimeSubvol, "@pg-tuned"} {
		name := displayName(subvol)
		got, err := resolveSource(fs, name)
		if err != nil {
			t.Fatalf("resolveSource(%q): %v", name, err)
		}
		if got != subvol {
			t.Errorf("round trip of %s produced %s via %q", subvol, got, name)
		}
	}
}

func TestDisplayName(t *testing.T) {
	cases := map[string]string{
		BaselineSubvol: "baseline",
		RuntimeSubvol:  "scratch",
		"@pg-tuned":    "pg-tuned",
	}
	for subvol, want := range cases {
		if got := displayName(subvol); got != want {
			t.Errorf("displayName(%q) = %q, want %q", subvol, got, want)
		}
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"main", "pg-tuned", "pg_tuned_v2", "a", "exp1"}
	for _, name := range valid {
		if err := validateName(name); err != nil {
			t.Errorf("validateName(%q) rejected a good name: %v", name, err)
		}
	}

	// Reserved words name boot modes rather than slates; the rest would escape
	// the entries directory or the filesystem root if they reached a path.
	invalid := []string{
		"", "baseline", "scratch", "rescue", "runtime", "hostid",
		"../evil", "with/slash", ".hidden", "with space", "@at",
		"name.with.dots", "unicodé",
	}
	for _, name := range invalid {
		if err := validateName(name); err == nil {
			t.Errorf("validateName(%q) accepted a name it should not", name)
		}
	}
}

func TestParseCmdlineSubvol(t *testing.T) {
	cases := []struct {
		cmdline string
		want    string
		ok      bool
	}{
		{"root=PARTUUID=x rootflags=subvol=@main", "@main", true},
		{"rootflags=compress=zstd,subvol=/@pg-tuned", "@pg-tuned", true},
		{"rootflags=subvol=main", "@main", true},
		{"root=PARTUUID=x quiet", "", false},
		{"rootflags=compress=zstd", "", false},
	}
	for _, tc := range cases {
		got, ok := parseCmdlineSubvol(tc.cmdline)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("parseCmdlineSubvol(%q) = (%q, %v), want (%q, %v)", tc.cmdline, got, ok, tc.want, tc.ok)
		}
	}
}

func TestReadBootedFromRunFacts(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("slate", "pg-tuned")
	write("mode", "persistent")
	write("basis", "@pg-tuned")
	write("checkpoint", "@pg-tuned.ckpt.0007.auto")
	write("requested", "@pg-tuned")
	write("booted-at", "2026-08-03T09:12:04Z")

	b, err := readRunFacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "pg-tuned" || b.Mode != ModePersistent || b.Basis != "@pg-tuned" {
		t.Errorf("unexpected boot record: %+v", b)
	}
	if b.BootedAt.IsZero() {
		t.Error("boot time was not parsed")
	}
	if b.Diverged() {
		t.Error("a boot that got what it asked for should not report a fallback")
	}
}

// Every fallback the initramfs takes makes /proc/cmdline describe a boot that
// did not happen, so the record has to be able to say the two differed.
func TestBootedReportsFallback(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"slate": "baseline", "mode": "persistent",
		"basis": "@baseline", "requested": "@deleted-slate",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := readRunFacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Diverged() {
		t.Errorf("expected a reported fallback, got %+v", b)
	}
}

func TestReadBootedMissingRecord(t *testing.T) {
	if _, err := readRunFacts(t.TempDir()); err == nil {
		t.Error("an empty directory should not produce a boot record")
	}
}
