package slate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Mode is how the running root was prepared.
type Mode string

const (
	// ModePersistent is a slate mounted directly. Writes are kept.
	ModePersistent Mode = "persistent"
	// ModeScratch is a disposable copy of a basis, discarded at reboot.
	ModeScratch Mode = "scratch"
	// ModeRescue is the read-only baseline.
	ModeRescue Mode = "rescue"
	// ModeUnknown covers a system this tool did not boot.
	ModeUnknown Mode = "unknown"
)

// Booted describes the running root as recorded at boot.
type Booted struct {
	Name       string // display name of the slate or basis
	Mode       Mode
	Basis      string // on-disk subvolume the root came from
	Checkpoint string // checkpoint taken on the way in, empty if none
	Requested  string // what the boot entry asked for, if it differed
	BootedAt   time.Time

	// FromCmdline records that /run/cleanslate was unavailable and these
	// facts were inferred. Inference cannot see fallbacks, so callers should
	// present the result as less certain.
	FromCmdline bool
}

// AutoCheckpointOff reports that this boot deliberately left no rollback point.
func (b Booted) AutoCheckpointOff() bool { return b.Checkpoint == "disabled" }

// Diverged reports whether the boot fell back to something other than what was
// asked for — a slate that had been deleted, or a malformed basis.
func (b Booted) Diverged() bool {
	return b.Requested != "" && b.Basis != "" && b.Requested != b.Basis
}

// ReadBooted reports how the running system was booted.
//
// The initramfs hook is the only component that knows what actually happened,
// because every fallback it takes makes /proc/cmdline describe a boot that did
// not occur. Its record is therefore preferred, and cmdline parsing is only a
// fallback for images built before the hook existed and for build hosts.
func ReadBooted() (Booted, error) {
	if b, err := readRunFacts(RunDir); err == nil {
		return b, nil
	}
	return bootedFromCmdline()
}

func readRunFacts(dir string) (Booted, error) {
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}

	name := read("slate")
	if name == "" {
		return Booted{}, fmt.Errorf("no boot record in %s", dir)
	}

	b := Booted{
		Name:       name,
		Mode:       Mode(read("mode")),
		Basis:      read("basis"),
		Checkpoint: read("checkpoint"),
		Requested:  read("requested"),
	}
	if b.Mode == "" {
		b.Mode = ModeUnknown
	}
	if ts := read("booted-at"); ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			b.BootedAt = t
		}
	}
	return b, nil
}

func bootedFromCmdline() (Booted, error) {
	subvol, err := currentSubvolFromCmdline()
	if err != nil {
		return Booted{}, err
	}
	b := Booted{
		Name:        displayName(subvol),
		Basis:       subvol,
		FromCmdline: true,
	}
	switch subvol {
	case RuntimeSubvol:
		b.Mode = ModeScratch
	case BaselineSubvol:
		b.Mode = ModeRescue
	default:
		b.Mode = ModePersistent
	}
	return b, nil
}
