package slate

import (
	"bufio"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Subvolume is the subset of btrfs subvolume metadata cleanslate cares about.
type Subvolume struct {
	ID         uint64
	Path       string // path within the filesystem, e.g. "@baseline"
	UUID       string
	ParentUUID string // empty if not a snapshot
	Generation uint64
}

// btrfsListSubvolumes runs `btrfs subvolume list -u -q -g` against fsRoot and
// parses the output. fsRoot must be a path on the target btrfs filesystem.
//
// Output format (btrfs-progs 6.x):
//
//	ID 256 gen 18 top level 5 parent_uuid - uuid abc-... path @baseline
func btrfsListSubvolumes(fsRoot string) ([]Subvolume, error) {
	out, err := exec.Command("btrfs", "subvolume", "list", "-u", "-q", "-g", fsRoot).Output()
	if err != nil {
		return nil, fmt.Errorf("btrfs subvolume list %s: %w", fsRoot, err)
	}

	var subvols []Subvolume
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		s, ok := parseSubvolumeLine(scanner.Text())
		if !ok {
			continue
		}
		subvols = append(subvols, s)
	}
	return subvols, scanner.Err()
}

var subvolLinePattern = regexp.MustCompile(
	`^ID\s+(\d+)\s+gen\s+(\d+)\s+(?:cgen\s+\d+\s+)?top\s+level\s+\d+\s+parent_uuid\s+(\S+)\s+(?:received_uuid\s+\S+\s+)?uuid\s+(\S+)\s+path\s+(.+)$`,
)

func parseSubvolumeLine(line string) (Subvolume, bool) {
	m := subvolLinePattern.FindStringSubmatch(line)
	if m == nil {
		return Subvolume{}, false
	}
	id, _ := strconv.ParseUint(m[1], 10, 64)
	gen, _ := strconv.ParseUint(m[2], 10, 64)
	parent := m[3]
	if parent == "-" {
		parent = ""
	}
	return Subvolume{
		ID:         id,
		Generation: gen,
		ParentUUID: parent,
		UUID:       m[4],
		Path:       m[5],
	}, true
}

// btrfsSnapshot creates a writable snapshot of src at dst.
func btrfsSnapshot(src, dst string) error {
	out, err := exec.Command("btrfs", "subvolume", "snapshot", src, dst).CombinedOutput()
	if err != nil {
		return fmt.Errorf("btrfs subvolume snapshot %s %s: %w (%s)", src, dst, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// btrfsDelete removes the subvolume at path.
func btrfsDelete(path string) error {
	out, err := exec.Command("btrfs", "subvolume", "delete", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("btrfs subvolume delete %s: %w (%s)", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// btrfsSnapshotRO creates a read-only snapshot of src at dst. Checkpoints are
// created read-only rather than sealed afterwards so there is no window in
// which one is writable.
func btrfsSnapshotRO(src, dst string) error {
	out, err := exec.Command("btrfs", "subvolume", "snapshot", "-r", src, dst).CombinedOutput()
	if err != nil {
		return fmt.Errorf("btrfs subvolume snapshot -r %s %s: %w (%s)", src, dst, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// btrfsDeleteRecursive removes a subvolume and any subvolumes nested inside it.
// Plain deletion fails with ENOTEMPTY on nested subvolumes, which is what a
// container runtime using the btrfs storage driver leaves behind.
func btrfsDeleteRecursive(path string) error {
	out, err := exec.Command("btrfs", "subvolume", "delete", "-R", path).CombinedOutput()
	if err == nil {
		return nil
	}
	// btrfs-progs without -R: enumerate nested subvolumes and remove the
	// deepest first, then retry the parent.
	if nested, lErr := nestedSubvolumes(path); lErr == nil {
		for _, n := range nested {
			_ = btrfsDelete(filepath.Join(filepath.Dir(path), n))
		}
		if err2 := btrfsDelete(path); err2 == nil {
			return nil
		}
	}
	return fmt.Errorf("btrfs subvolume delete -R %s: %w (%s)", path, err, strings.TrimSpace(string(out)))
}

// nestedSubvolumes lists subvolumes below path, deepest first.
func nestedSubvolumes(path string) ([]string, error) {
	out, err := exec.Command("btrfs", "subvolume", "list", "-o", path).Output()
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, " path "); i >= 0 {
			paths = append(paths, strings.TrimSpace(line[i+6:]))
		}
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	return paths, nil
}
