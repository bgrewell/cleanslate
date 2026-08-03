package slate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A slate is the mounted root while it is running, so it cannot be replaced in
// place: rollback and reset stage the replacement and the initramfs hook
// applies it on the next boot, where nothing is holding the subvolume open.
//
// The staged file is deliberately shell-parseable — the component that applies
// it runs in the initramfs, where a Go parser is not available and a
// hand-rolled one would be the wrong place to be clever.
const pendingFile = "pending"

// Pending is a slate replacement waiting for the next boot.
type Pending struct {
	// Slate is the on-disk subvolume to replace, e.g. "@main".
	Slate string
	// Source is the subvolume to replace it with: a checkpoint, or the
	// baseline in the case of a reset.
	Source string
	// Reason is shown to whoever asks what is pending.
	Reason string
}

func pendingPath(fs *FsRoot) string {
	return filepath.Join(fs.Path, MetaDir, pendingFile)
}

// StagePending records a replacement to apply at the next boot, replacing any
// previously staged one. Staging does not reboot; the caller decides that.
func StagePending(fs *FsRoot, p Pending) error {
	if p.Slate == "" || p.Source == "" {
		return fmt.Errorf("a staged replacement needs both a slate and a source")
	}
	if _, err := os.Stat(filepath.Join(fs.Path, p.Source)); err != nil {
		return fmt.Errorf("%s does not exist", p.Source)
	}
	path := pendingPath(fs)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	body := fmt.Sprintf("slate=%s\nsource=%s\nreason=%s\n",
		p.Slate, p.Source, strings.ReplaceAll(p.Reason, "\n", " "))
	return os.WriteFile(path, []byte(body), 0644)
}

// ReadPending returns the staged replacement, if any.
func ReadPending(fs *FsRoot) (Pending, bool) {
	data, err := os.ReadFile(pendingPath(fs))
	if err != nil {
		return Pending{}, false
	}
	var p Pending
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "slate":
			p.Slate = v
		case "source":
			p.Source = v
		case "reason":
			p.Reason = v
		}
	}
	if p.Slate == "" || p.Source == "" {
		return Pending{}, false
	}
	return p, true
}

// ClearPending discards a staged replacement.
func ClearPending(fs *FsRoot) error {
	err := os.Remove(pendingPath(fs))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
