package state

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// FsRoot is a temporarily-mounted view of the btrfs filesystem at subvol=/,
// regardless of which subvolume is currently mounted as /. State management
// operations need this view because they reference subvolumes by path
// relative to the filesystem root (e.g. /<fsroot>/@gnb-xyz).
type FsRoot struct {
	Path string

	tempMount bool // true if we mounted it ourselves and need to clean up
}

// MountFsRoot finds the device backing / and mounts subvol=/ at a temp path.
// Caller must call Close. Requires root.
func MountFsRoot() (*FsRoot, error) {
	dev, err := rootDevice()
	if err != nil {
		return nil, err
	}

	tmp, err := os.MkdirTemp("", "testbox-fsroot-*")
	if err != nil {
		return nil, fmt.Errorf("mkdir temp: %w", err)
	}

	out, err := exec.Command("mount", "-t", "btrfs", "-o", "subvol=/", dev, tmp).CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("mount %s -o subvol=/ at %s: %w (%s)", dev, tmp, err, strings.TrimSpace(string(out)))
	}

	return &FsRoot{Path: tmp, tempMount: true}, nil
}

// FsRootAt returns an FsRoot using an already-mounted path. The path must
// already be a mount of the target filesystem at subvol=/. No cleanup is
// performed by Close in this case.
func FsRootAt(path string) *FsRoot {
	return &FsRoot{Path: path, tempMount: false}
}

// Close unmounts and cleans up the temp directory if MountFsRoot allocated one.
func (f *FsRoot) Close() error {
	if !f.tempMount {
		return nil
	}
	if out, err := exec.Command("umount", f.Path).CombinedOutput(); err != nil {
		return fmt.Errorf("umount %s: %w (%s)", f.Path, err, strings.TrimSpace(string(out)))
	}
	return os.Remove(f.Path)
}

// rootDevice reads /proc/self/mounts and returns the device backing /.
func rootDevice() (string, error) {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		if fields[1] == "/" && fields[2] == "btrfs" {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no btrfs root mount found in /proc/self/mounts")
}
