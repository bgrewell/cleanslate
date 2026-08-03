package slate

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Subvolume is the subset of btrfs subvolume metadata cleanslate cares about.
type Subvolume struct {
	ID         uint64
	Path       string // path within the filesystem, e.g. "@base"
	UUID       string
	ParentUUID string // empty if not a snapshot
	Generation uint64
}

// btrfsListSubvolumes runs `btrfs subvolume list -u -q -g` against fsRoot and
// parses the output. fsRoot must be a path on the target btrfs filesystem.
//
// Output format (btrfs-progs 6.x):
//
//	ID 256 gen 18 top level 5 parent_uuid - uuid abc-... path @base
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
