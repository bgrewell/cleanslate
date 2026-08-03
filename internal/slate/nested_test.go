package slate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNestedErrorNamesThePaths(t *testing.T) {
	err := &NestedError{Slate: "main", Paths: []string{"srv/data", "var/lib/docker/btrfs/subvolumes/a1"}}
	msg := err.Error()

	for _, want := range []string{"2 nested", "srv/data", "var/lib/docker", "empty after a rollback"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
}

// Detection is the whole mitigation, so it is exercised against real btrfs
// rather than a stub: the failure mode being guarded against is a snapshot that
// looks complete, and a stubbed detector would look complete too.
func TestNestedInAgainstRealBtrfs(t *testing.T) {
	requireBtrfsTools(t)
	fsRoot, cleanup := newLoopbackBtrfs(t)
	defer cleanup()

	fs := FsRootAt(fsRoot)
	mustRun(t, "btrfs", "subvolume", "create", filepath.Join(fsRoot, "@main"))

	nested, err := NestedIn(fs, "main")
	if err != nil {
		t.Fatalf("NestedIn on a plain slate: %v", err)
	}
	if len(nested) != 0 {
		t.Fatalf("a plain slate reported nested subvolumes: %v", nested)
	}
	if err := CheckCapturable(fs, "main"); err != nil {
		t.Errorf("a plain slate should be capturable: %v", err)
	}

	deep := filepath.Join(fsRoot, "@main", "var", "lib", "docker")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "btrfs", "subvolume", "create", filepath.Join(deep, "layer1"))
	mustRun(t, "btrfs", "subvolume", "create", filepath.Join(fsRoot, "@main", "srv"))

	nested, err = NestedIn(fs, "main")
	if err != nil {
		t.Fatalf("NestedIn: %v", err)
	}
	if len(nested) != 2 {
		t.Fatalf("expected 2 nested subvolumes, got %v", nested)
	}
	// Paths are reported relative to the slate, not the filesystem root: the
	// question a user is answering is "where in my system is this".
	for _, want := range []string{"srv", "var/lib/docker/layer1"} {
		var found bool
		for _, p := range nested {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %q among %v", want, nested)
		}
	}

	var nestedErr *NestedError
	err = CheckCapturable(fs, "main")
	if err == nil {
		t.Fatal("a slate with nested subvolumes should not be reported as capturable")
	}
	if !asNestedError(err, &nestedErr) {
		t.Fatalf("expected a NestedError, got %T", err)
	}
	if len(nestedErr.Paths) != 2 {
		t.Errorf("NestedError carries %v", nestedErr.Paths)
	}
}

// Demonstrates the loss this issue is about: the nested file is absent from a
// snapshot while an ordinary file survives. If btrfs ever changes this, the
// warning machinery becomes unnecessary and this test says so.
func TestSnapshotDropsNestedContent(t *testing.T) {
	requireBtrfsTools(t)
	fsRoot, cleanup := newLoopbackBtrfs(t)
	defer cleanup()

	slatePath := filepath.Join(fsRoot, "@main")
	mustRun(t, "btrfs", "subvolume", "create", slatePath)
	mustRun(t, "btrfs", "subvolume", "create", filepath.Join(slatePath, "nested"))

	if err := os.WriteFile(filepath.Join(slatePath, "ordinary.txt"), []byte("kept"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slatePath, "nested", "lost.txt"), []byte("lost"), 0644); err != nil {
		t.Fatal(err)
	}

	ckpt := filepath.Join(fsRoot, "@main.ckpt.0001.auto")
	mustRun(t, "btrfs", "subvolume", "snapshot", "-r", slatePath, ckpt)

	if _, err := os.Stat(filepath.Join(ckpt, "ordinary.txt")); err != nil {
		t.Errorf("an ordinary file should survive a snapshot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ckpt, "nested", "lost.txt")); err == nil {
		t.Error("btrfs now captures nested subvolumes; the warning machinery can be removed")
	}
}

func asNestedError(err error, target **NestedError) bool {
	if e, ok := err.(*NestedError); ok {
		*target = e
		return true
	}
	return false
}

func requireBtrfsTools(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("needs root to create a loopback btrfs filesystem")
	}
	for _, bin := range []string{"btrfs", "mkfs.btrfs", "mount", "umount"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is not available", bin)
		}
	}
}

// newLoopbackBtrfs builds a small btrfs filesystem and returns its mounted
// root. Nothing here can be faked usefully — the behaviour under test is the
// filesystem's.
func newLoopbackBtrfs(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	img := filepath.Join(dir, "fs.img")

	mustRun(t, "truncate", "-s", "512M", img)
	mustRun(t, "mkfs.btrfs", "-q", "-f", img)

	mnt := filepath.Join(dir, "mnt")
	if err := os.MkdirAll(mnt, 0755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "mount", "-o", "loop", img, mnt)

	return mnt, func() { _ = exec.Command("umount", mnt).Run() }
}

func mustRun(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v (%s)", name, args, err, strings.TrimSpace(string(out)))
	}
}
