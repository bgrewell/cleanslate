// Package state implements the cleanslate state-management commands: list,
// save, delete, current. Slate is modelled directly on btrfs subvolumes:
//
//   - @baseline    — the immutable baked OS (reserved)
//   - @runtime — ephemeral working root, recreated each fresh boot (reserved)
//   - @hostid  — stable SSH identity carve-out (reserved, hidden from list)
//   - @<name>  — named persistent layers, freely created and deleted
//
// Operations require a mounted view of the btrfs filesystem at subvol=/,
// represented by an FsRoot. Most callers should use MountFsRoot, which
// resolves the device backing / and mounts a temp view; tests and the
// --fs-root flag can use FsRootAt against a pre-existing mount.
package slate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	BaselineSubvol = "@baseline"
	RuntimeSubvol  = "@runtime"
	HostidSubvol   = "@hostid"
)

// reservedSubvols are subvolumes managed by the runtime infrastructure;
// they cannot be created or deleted via `cleanslate state`.
var reservedSubvols = map[string]bool{
	BaselineSubvol: true,
	RuntimeSubvol:  true,
	HostidSubvol:   true,
}

// Slate is a user-facing view of one btrfs subvolume.
type Slate struct {
	Name       string // user-facing name ("baseline", "scratch", "pg-tuned")
	Subvolume  string // on-disk subvolume name (always begins with @)
	UUID       string
	ParentUUID string // empty if not a snapshot
	Generation uint64
	Reserved   bool // true for @baseline, @runtime, @hostid
}

// List returns all cleanslate-managed subvolumes on the given fs-root mount.
// The @hostid subvolume is filtered out — it is infrastructure, not state.
func List(fs *FsRoot) ([]Slate, error) {
	subvols, err := btrfsListSubvolumes(fs.Path)
	if err != nil {
		return nil, err
	}

	uuidToName := make(map[string]string, len(subvols))
	for _, s := range subvols {
		uuidToName[s.UUID] = s.Path
	}

	var out []Slate
	for _, s := range subvols {
		if !strings.HasPrefix(s.Path, "@") {
			continue
		}
		if s.Path == HostidSubvol {
			continue
		}
		out = append(out, Slate{
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
// @baseline → "baseline", @runtime → "scratch", @pg-tuned → "pg-tuned".
func displayName(subvol string) string {
	switch subvol {
	case BaselineSubvol:
		return "baseline"
	case RuntimeSubvol:
		return "scratch"
	}
	return strings.TrimPrefix(subvol, "@")
}

var validNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// reservedNames cannot be used for a slate. "scratch", "rescue", and
// "baseline" name boot modes rather than slates; a slate answering to one of
// them would make the boot menu ambiguous.
var reservedNames = map[string]bool{
	"baseline": true,
	"scratch":  true,
	"rescue":   true,
	"runtime":  true,
	"hostid":   true,
}

// validateName rejects names that are unusable as a subvolume, as a boot-entry
// filename, or as a word in the boot menu. Names reach both paths and
// filenames, so this is a safety boundary and not only a courtesy: a name
// containing a slash or a leading dot could escape the entries directory.
func validateName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("a slate name is required")
	case !validNamePattern.MatchString(name):
		return fmt.Errorf("invalid slate name %q: use letters, digits, '-' and '_'", name)
	case reservedNames[name] || reservedSubvols["@"+name]:
		return fmt.Errorf("%q is reserved", name)
	}
	return nil
}

// Save snapshots source into a new named state. Source may be the user-
// facing name of an existing slate ("scratch", "baseline", "pg-tuned") or "current"
// to use the running active subvolume. Reserved names cannot be used as the
// destination.
func Save(fs *FsRoot, name, source string) error {
	if err := validateName(name); err != nil {
		return err
	}

	srcSubvol, err := resolveSource(fs, source)
	if err != nil {
		return err
	}
	srcPath := filepath.Join(fs.Path, srcSubvol)
	dstPath := filepath.Join(fs.Path, "@"+name)

	if _, err := os.Stat(dstPath); err == nil {
		return fmt.Errorf("a slate named %q already exists", name)
	}

	return btrfsSnapshot(srcPath, dstPath)
}

// resolveSource maps a user-facing source name to an on-disk subvolume.
func resolveSource(fs *FsRoot, source string) (string, error) {
	if source == "" || source == "current" {
		return resolveCurrentSubvolume(fs)
	}
	switch source {
	case "scratch":
		return RuntimeSubvol, nil
	case "baseline":
		return BaselineSubvol, nil
	}
	return "@" + strings.TrimPrefix(source, "@"), nil
}

func resolveCurrentSubvolume(fs *FsRoot) (string, error) {
	subvol, err := currentSubvolFromCmdline()
	if err != nil {
		// Not running on a cleanslate; fall back to @runtime so `state save`
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
// flag is present (e.g. not running on a cleanslate).
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
