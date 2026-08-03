package slate

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// A btrfs snapshot does not descend into subvolumes nested inside the tree it
// captures: they appear in the snapshot as empty directories. A checkpoint of a
// slate containing them therefore looks complete, rolls back cleanly, and has
// silently lost whatever was inside them.
//
// Rollback is the safety mechanism the whole model rests on, so a checkpoint
// that quietly omits data is worse than no checkpoint — it is trusted. These
// helpers exist so nothing has to be trusted blindly.

// NestedError reports that a slate contains subvolumes a checkpoint cannot
// capture. It carries the paths so callers can name them rather than describe
// the problem in the abstract.
type NestedError struct {
	Slate string
	Paths []string
}

func (e *NestedError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d nested filesystem(s) under this slate will NOT be captured, "+
		"and would be empty after a rollback:\n", len(e.Paths))
	for _, p := range e.Paths {
		fmt.Fprintf(&b, "\n  %s", p)
	}
	return b.String()
}

// NestedIn returns the subvolumes nested inside a slate, as paths relative to
// its root. An empty result means a checkpoint of it will be complete.
func NestedIn(fs *FsRoot, slateName string) ([]string, error) {
	subvol := "@" + strings.TrimPrefix(slateName, "@")
	found, err := nestedSubvolumes(filepath.Join(fs.Path, subvol))
	if err != nil {
		// Not being able to ask is not the same as there being none, but the
		// caller is usually about to do something more important than this
		// check, so an empty list with the error lets it decide.
		return nil, err
	}

	// btrfs reports paths relative to the filesystem root; callers care about
	// where inside the slate the data sits.
	out := make([]string, 0, len(found))
	for _, p := range found {
		rel := strings.TrimPrefix(strings.TrimPrefix(p, subvol), "/")
		if rel == "" || rel == p && !strings.HasPrefix(p, subvol) {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out, nil
}

// CheckCapturable returns a NestedError when a checkpoint of the slate would be
// incomplete. A failure to enumerate is reported as no obstruction: refusing to
// checkpoint because the check itself broke would deny people the rollback
// point they were trying to create.
func CheckCapturable(fs *FsRoot, slateName string) error {
	nested, err := NestedIn(fs, slateName)
	if err != nil || len(nested) == 0 {
		return nil
	}
	return &NestedError{Slate: slateName, Paths: nested}
}
