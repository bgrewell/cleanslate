package slate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Checkpoints are subvolumes named @<slate>.ckpt.<seq>.<class>, read-only from
// birth so there is never a window in which one can be written to.
//
// The retention class is encoded in the name rather than in metadata because
// the initramfs prunes them, and pruning there must not depend on parsing a
// file that could be missing or malformed. Messages and timestamps live in a
// sidecar that only the CLI reads, so losing it degrades presentation and
// nothing else.
const (
	// ClassAuto is taken at boot and rolls off once the retention count is
	// exceeded.
	ClassAuto = "auto"
	// ClassKeep is taken deliberately and is never pruned.
	ClassKeep = "keep"
)

// MetaDir holds checkpoint sidecars and machine configuration. It sits at the
// filesystem root rather than inside a slate, so it cannot be captured into a
// checkpoint of itself.
const MetaDir = ".cleanslate"

const defaultRetainAuto = 10

var checkpointPattern = regexp.MustCompile(`^@(.+)\.ckpt\.(\d+)\.(auto|keep)$`)

// Checkpoint is one rollback point on a slate.
type Checkpoint struct {
	Slate     string
	Seq       int
	Class     string
	Subvolume string
	Message   string
	CreatedAt time.Time
}

// Automatic reports whether this checkpoint is subject to pruning.
func (c Checkpoint) Automatic() bool { return c.Class == ClassAuto }

// Ref is the name a user types to identify a checkpoint.
func (c Checkpoint) Ref() string { return fmt.Sprintf("%s.%d", c.Slate, c.Seq) }

// ListCheckpoints returns the checkpoints for a slate, oldest first. Passing an
// empty slate name returns checkpoints for every slate.
func ListCheckpoints(fs *FsRoot, slateName string) ([]Checkpoint, error) {
	subvols, err := btrfsListSubvolumes(fs.Path)
	if err != nil {
		return nil, err
	}

	var out []Checkpoint
	for _, s := range subvols {
		m := checkpointPattern.FindStringSubmatch(s.Path)
		if m == nil {
			continue
		}
		if slateName != "" && m[1] != slateName {
			continue
		}
		seq, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		c := Checkpoint{
			Slate:     m[1],
			Seq:       seq,
			Class:     m[3],
			Subvolume: s.Path,
		}
		readCheckpointMeta(fs, &c)
		out = append(out, c)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Slate != out[j].Slate {
			return out[i].Slate < out[j].Slate
		}
		return out[i].Seq < out[j].Seq
	})
	return out, nil
}

// FindCheckpoint resolves a user-supplied reference. Accepted forms are
// "<slate>.<seq>", a bare "<seq>" when the slate is implied, and the full
// subvolume name.
func FindCheckpoint(fs *FsRoot, ref, defaultSlate string) (Checkpoint, error) {
	if ref == "" {
		return Checkpoint{}, fmt.Errorf("a checkpoint is required; run `cleanslate history` to see them")
	}

	slateName, seqPart := defaultSlate, ref
	if m := checkpointPattern.FindStringSubmatch(ref); m != nil {
		slateName, seqPart = m[1], m[2]
	} else if i := strings.LastIndex(ref, "."); i >= 0 {
		slateName, seqPart = ref[:i], ref[i+1:]
	}

	seq, err := strconv.Atoi(seqPart)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("%q is not a checkpoint reference; expected <slate>.<number>", ref)
	}

	all, err := ListCheckpoints(fs, slateName)
	if err != nil {
		return Checkpoint{}, err
	}
	for _, c := range all {
		if c.Seq == seq {
			return c, nil
		}
	}
	return Checkpoint{}, fmt.Errorf("no checkpoint %d on slate %q", seq, slateName)
}

// CreateCheckpoint snapshots a slate read-only. An empty message produces an
// automatic checkpoint, which is eligible for pruning; any message makes it a
// kept one, which is not.
//
// allowIncomplete permits a checkpoint of a slate containing nested
// subvolumes, whose contents a snapshot cannot capture. It defaults off so the
// loss has to be accepted explicitly rather than discovered at rollback.
func CreateCheckpoint(fs *FsRoot, slateName, message string, allowIncomplete bool) (Checkpoint, error) {
	if err := validateName(slateName); err != nil {
		return Checkpoint{}, err
	}
	if !allowIncomplete {
		if err := CheckCapturable(fs, slateName); err != nil {
			return Checkpoint{}, err
		}
	}
	subvol := "@" + slateName
	if _, err := os.Stat(filepath.Join(fs.Path, subvol)); err != nil {
		return Checkpoint{}, fmt.Errorf("no slate named %q", slateName)
	}

	class := ClassKeep
	if message == "" {
		class = ClassAuto
	}

	seq, err := nextSeq(fs, slateName)
	if err != nil {
		return Checkpoint{}, err
	}

	c := Checkpoint{
		Slate:     slateName,
		Seq:       seq,
		Class:     class,
		Message:   message,
		CreatedAt: time.Now().UTC(),
		Subvolume: fmt.Sprintf("@%s.ckpt.%04d.%s", slateName, seq, class),
	}

	if err := btrfsSnapshotRO(filepath.Join(fs.Path, subvol), filepath.Join(fs.Path, c.Subvolume)); err != nil {
		return Checkpoint{}, err
	}
	if err := writeCheckpointMeta(fs, c); err != nil {
		// The checkpoint itself is on disk and usable; only its description
		// was lost, which is not worth failing the command over.
		return c, nil
	}
	return c, nil
}

// DeleteCheckpoint removes a checkpoint.
func DeleteCheckpoint(fs *FsRoot, c Checkpoint) error {
	if err := btrfsDeleteRecursive(filepath.Join(fs.Path, c.Subvolume)); err != nil {
		return err
	}
	_ = os.Remove(metaPath(fs, c))
	return nil
}

// PruneAuto deletes the oldest automatic checkpoints beyond the retention
// count. Kept checkpoints are never considered.
func PruneAuto(fs *FsRoot, slateName string) (int, error) {
	all, err := ListCheckpoints(fs, slateName)
	if err != nil {
		return 0, err
	}
	var auto []Checkpoint
	for _, c := range all {
		if c.Automatic() {
			auto = append(auto, c)
		}
	}
	keep := RetainAuto(fs)
	if len(auto) <= keep {
		return 0, nil
	}
	dropped := 0
	for _, c := range auto[:len(auto)-keep] {
		if err := DeleteCheckpoint(fs, c); err != nil {
			return dropped, err
		}
		dropped++
	}
	return dropped, nil
}

// RetainAuto is how many automatic checkpoints per slate are kept.
func RetainAuto(fs *FsRoot) int {
	data, err := os.ReadFile(filepath.Join(fs.Path, MetaDir, "config"))
	if err != nil {
		return defaultRetainAuto
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "retain_auto="); ok {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				return n
			}
		}
	}
	return defaultRetainAuto
}

// AutoCheckpointEnabled reports whether the initramfs takes a checkpoint on
// every boot. Machines running a stateful workload can turn it off, because a
// snapshot forces a copy on the first write to each block even for nodatacow
// files — measured to leave a random-rewrite workload at plain copy-on-write
// speed and permanently fragmented, which makes chattr +C worthless while
// checkpoints are being taken. The cost is the per-boot rollback point.
func AutoCheckpointEnabled(fs *FsRoot) bool {
	data, err := os.ReadFile(filepath.Join(fs.Path, MetaDir, "config"))
	if err != nil {
		return true
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "auto_checkpoint="); ok {
			switch strings.TrimSpace(v) {
			case "off", "no", "false", "0":
				return false
			}
		}
	}
	return true
}

func nextSeq(fs *FsRoot, slateName string) (int, error) {
	all, err := ListCheckpoints(fs, slateName)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, c := range all {
		if c.Seq > max {
			max = c.Seq
		}
	}
	return max + 1, nil
}

func metaPath(fs *FsRoot, c Checkpoint) string {
	return filepath.Join(fs.Path, MetaDir, "checkpoints", c.Slate, fmt.Sprintf("%04d.meta", c.Seq))
}

func writeCheckpointMeta(fs *FsRoot, c Checkpoint) error {
	p := metaPath(fs, c)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	body := fmt.Sprintf("created=%s\nclass=%s\nmessage=%s\n",
		c.CreatedAt.Format(time.RFC3339), c.Class, strings.ReplaceAll(c.Message, "\n", " "))
	return os.WriteFile(p, []byte(body), 0644)
}

func readCheckpointMeta(fs *FsRoot, c *Checkpoint) {
	data, err := os.ReadFile(metaPath(fs, *c))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "message":
			c.Message = v
		case "created":
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				c.CreatedAt = t
			}
		}
	}
}
