// Package state implements the testbox state-management commands: list,
// save, delete, current. State is modelled directly on btrfs subvolumes:
//
//   - @base    — the immutable baked OS (reserved)
//   - @runtime — ephemeral working root, recreated each fresh boot (reserved)
//   - @hostid  — stable SSH identity carve-out (reserved, hidden from list)
//   - @<name>  — named persistent layers, freely created and deleted
//
// Operations require a mounted view of the btrfs filesystem at subvol=/,
// represented by an FsRoot. Most callers should use MountFsRoot, which
// resolves the device backing / and mounts a temp view; tests and the
// --fs-root flag can use FsRootAt against a pre-existing mount.
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	BaseSubvol    = "@base"
	RuntimeSubvol = "@runtime"
	HostidSubvol  = "@hostid"
)

// reservedSubvols are subvolumes managed by the runtime infrastructure;
// they cannot be created or deleted via `testbox state`.
var reservedSubvols = map[string]bool{
	BaseSubvol:    true,
	RuntimeSubvol: true,
	HostidSubvol:  true,
}

// State is a user-facing view of one btrfs subvolume.
type State struct {
	Name       string // user-facing name ("base", "fresh", "gnb-xyz")
	Subvolume  string // on-disk subvolume name (always begins with @)
	UUID       string
	ParentUUID string // empty if not a snapshot
	Generation uint64
	Reserved   bool // true for @base, @runtime, @hostid
}

// List returns all testbox-managed subvolumes on the given fs-root mount.
// The @hostid subvolume is filtered out — it is infrastructure, not state.
func List(fs *FsRoot) ([]State, error) {
	subvols, err := btrfsListSubvolumes(fs.Path)
	if err != nil {
		return nil, err
	}

	uuidToName := make(map[string]string, len(subvols))
	for _, s := range subvols {
		uuidToName[s.UUID] = s.Path
	}

	var out []State
	for _, s := range subvols {
		if !strings.HasPrefix(s.Path, "@") {
			continue
		}
		if s.Path == HostidSubvol {
			continue
		}
		out = append(out, State{
			Name:       displayName(s.Path),
			Subvolume:  s.Path,
			UUID:       s.UUID,
			ParentUUID: s.ParentUUID,
			Generation: s.Generation,
			Reserved:   reservedSubvols[s.Path],
		})
	}
	return out, nil
}

// displayName converts an on-disk subvolume name to its user-facing form.
// @base → "base", @runtime → "fresh", @gnb-xyz → "gnb-xyz".
func displayName(subvol string) string {
	switch subvol {
	case BaseSubvol:
		return "base"
	case RuntimeSubvol:
		return "fresh"
	}
	return strings.TrimPrefix(subvol, "@")
}

var validNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Save snapshots source into a new named state. Source may be the user-
// facing name of an existing state ("fresh", "base", "gnb-xyz") or "current"
// to use the running active subvolume. Reserved names cannot be used as the
// destination.
func Save(fs *FsRoot, name, source string) error {
	if name == "" {
		return fmt.Errorf("state name is required")
	}
	if !validNamePattern.MatchString(name) {
		return fmt.Errorf("invalid state name %q: only letters, digits, '-', and '_' are allowed", name)
	}
	if reservedSubvols["@"+name] || name == "base" || name == "fresh" {
		return fmt.Errorf("name %q is reserved", name)
	}

	srcSubvol, err := resolveSource(fs, source)
	if err != nil {
		return err
	}
	srcPath := filepath.Join(fs.Path, srcSubvol)
	dstPath := filepath.Join(fs.Path, "@"+name)

	if _, err := os.Stat(dstPath); err == nil {
		return fmt.Errorf("state %q already exists", name)
	}

	return btrfsSnapshot(srcPath, dstPath)
}

// resolveSource maps a user-facing source name to an on-disk subvolume.
func resolveSource(fs *FsRoot, source string) (string, error) {
	if source == "" || source == "current" {
		return resolveCurrentSubvolume(fs)
	}
	switch source {
	case "fresh":
		return RuntimeSubvol, nil
	case "base":
		return BaseSubvol, nil
	}
	return "@" + strings.TrimPrefix(source, "@"), nil
}

func resolveCurrentSubvolume(fs *FsRoot) (string, error) {
	subvol, err := currentSubvolFromCmdline()
	if err != nil {
		// Not running on a testbox; fall back to @runtime so `state save`
		// from the build host operates on the ephemeral working root.
		return RuntimeSubvol, nil
	}
	return subvol, nil
}

// Delete removes a named state. Reserved subvolumes cannot be deleted.
func Delete(fs *FsRoot, name string) error {
	if name == "" {
		return fmt.Errorf("state name is required")
	}
	subvol := "@" + strings.TrimPrefix(name, "@")
	if reservedSubvols[subvol] {
		return fmt.Errorf("refusing to delete reserved subvolume %s", subvol)
	}
	path := filepath.Join(fs.Path, subvol)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("state %q does not exist", name)
	}
	return btrfsDelete(path)
}

// Current returns the user-facing name of the currently-active state by
// parsing /proc/cmdline for rootflags=subvol=. Returns an error if no such
// flag is present (e.g. not running on a testbox).
func Current() (string, error) {
	subvol, err := currentSubvolFromCmdline()
	if err != nil {
		return "", err
	}
	return displayName(subvol), nil
}

func currentSubvolFromCmdline() (string, error) {
	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}
	subvol, ok := parseCmdlineSubvol(string(data))
	if !ok {
		return "", fmt.Errorf("no rootflags=subvol= on /proc/cmdline")
	}
	return subvol, nil
}

func parseCmdlineSubvol(cmdline string) (string, bool) {
	for _, field := range strings.Fields(cmdline) {
		if !strings.HasPrefix(field, "rootflags=") {
			continue
		}
		flags := strings.TrimPrefix(field, "rootflags=")
		for _, f := range strings.Split(flags, ",") {
			if !strings.HasPrefix(f, "subvol=") {
				continue
			}
			subvol := strings.TrimPrefix(f, "subvol=")
			subvol = strings.TrimPrefix(subvol, "/")
			if !strings.HasPrefix(subvol, "@") {
				subvol = "@" + subvol
			}
			return subvol, true
		}
	}
	return "", false
}
