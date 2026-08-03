package slate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Fork creates a new slate from an existing slate or checkpoint. Unlike
// rollback and reset it takes effect immediately, because it writes somewhere
// nothing is currently mounted.
func Fork(fs *FsRoot, name, from string) error {
	if err := validateName(name); err != nil {
		return err
	}
	dst := filepath.Join(fs.Path, "@"+name)
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("a slate named %q already exists", name)
	}

	src, err := resolveForkSource(fs, from)
	if err != nil {
		return err
	}
	return btrfsSnapshot(filepath.Join(fs.Path, src), dst)
}

// resolveForkSource accepts a slate name, a checkpoint reference, "baseline",
// or an empty string meaning the running slate.
func resolveForkSource(fs *FsRoot, from string) (string, error) {
	if from == "" || from == "current" {
		b, err := ReadBooted()
		if err != nil {
			return "", fmt.Errorf("cannot tell which slate is running; name a source with --from")
		}
		if b.Mode == ModeRescue {
			return "", fmt.Errorf("a rescue boot has no slate to fork; name one with --from")
		}
		return b.Basis, nil
	}
	if from == "baseline" {
		return BaselineSubvol, nil
	}
	// A checkpoint reference, if it parses as one.
	if c, err := FindCheckpoint(fs, from, ""); err == nil {
		return c.Subvolume, nil
	}
	subvol := "@" + strings.TrimPrefix(from, "@")
	if _, err := os.Stat(filepath.Join(fs.Path, subvol)); err != nil {
		return "", fmt.Errorf("no slate or checkpoint named %q", from)
	}
	return subvol, nil
}

// Rollback stages a return to a checkpoint. It applies at the next boot, and
// the pre-rollback state is checkpointed first, so a rollback is itself
// reversible.
func Rollback(fs *FsRoot, c Checkpoint) error {
	return StagePending(fs, Pending{
		Slate:  "@" + c.Slate,
		Source: c.Subvolume,
		Reason: fmt.Sprintf("rollback to checkpoint %s", c.Ref()),
	})
}

// Reset stages a return to the pristine baseline. This is how a machine is
// handed to someone else: the baseline is never pruned, so it is always
// available however far a slate has drifted.
func Reset(fs *FsRoot, slateName string) error {
	if err := validateName(slateName); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(fs.Path, "@"+slateName)); err != nil {
		return fmt.Errorf("no slate named %q", slateName)
	}
	return StagePending(fs, Pending{
		Slate:  "@" + slateName,
		Source: BaselineSubvol,
		Reason: "reset to the baseline",
	})
}

// Discard removes a slate and every checkpoint belonging to it.
//
// The running slate is refused: the delete would run against the filesystem-root
// view rather than "/", so it would not hit the EBUSY that normally protects a
// mounted subvolume, and would take the running root out from under itself.
func Discard(fs *FsRoot, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	subvol := "@" + name
	path := filepath.Join(fs.Path, subvol)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("no slate named %q", name)
	}

	if b, err := ReadBooted(); err == nil && b.Basis == subvol && b.Mode == ModePersistent {
		return fmt.Errorf("%q is the slate you are running on; boot another one first", name)
	}

	checkpoints, err := ListCheckpoints(fs, name)
	if err != nil {
		return err
	}
	for _, c := range checkpoints {
		if err := DeleteCheckpoint(fs, c); err != nil {
			return fmt.Errorf("removing checkpoint %s: %w", c.Ref(), err)
		}
	}
	return btrfsDeleteRecursive(path)
}

// Slates returns just the slates — the baseline, the named ones, and nothing
// else. Checkpoints, the scratch root, and the host-identity carve-out are
// infrastructure and are reported separately or not at all.
func Slates(fs *FsRoot) ([]Slate, error) {
	all, err := List(fs)
	if err != nil {
		return nil, err
	}
	var out []Slate
	for _, s := range all {
		if s.Subvolume == RuntimeSubvol || checkpointPattern.MatchString(s.Subvolume) {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}
