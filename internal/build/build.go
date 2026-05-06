// Package build wraps mkosi to produce testbox base disk images.
//
// The package is a thin orchestrator: it validates the config directory and
// the mkosi installation, runs mkosi to produce a flat btrfs raw disk image,
// and then runs scripts/relayout.sh to rearrange the rootfs into @base and
// @hostid subvolumes. The relayout step needs root (loop-mounting) and
// cannot run inside mkosi's sandbox, so it lives outside the mkosi build.
package build

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Options configures a single image build.
type Options struct {
	// ConfigDir is the directory containing mkosi.conf. Defaults to "." when empty.
	ConfigDir string

	// Force passes --force to mkosi, clearing prior outputs before building.
	Force bool

	// SkipRelayout, when true, leaves the rootfs flat (no @base / @hostid).
	// Useful for debugging mkosi output or bypassing the root-required step.
	SkipRelayout bool

	// Stdout and Stderr receive subprocess output. Both default to the process streams.
	Stdout io.Writer
	Stderr io.Writer
}

// Run produces a testbox base disk image using the given options.
func Run(opts Options) error {
	dir := opts.ConfigDir
	if dir == "" {
		dir = "."
	}

	if _, err := os.Stat(filepath.Join(dir, "mkosi.conf")); err != nil {
		return fmt.Errorf("no mkosi.conf in %q: %w", dir, err)
	}

	if _, err := exec.LookPath("mkosi"); err != nil {
		return errors.New("mkosi not found in PATH (need v26 or newer); install from https://github.com/systemd/mkosi or via the openSUSE Build Service apt repo")
	}

	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	args := []string{"--directory", dir}
	if opts.Force {
		args = append(args, "--force")
	}
	args = append(args, "build")

	mkosiCmd := exec.Command("mkosi", args...)
	mkosiCmd.Stdout = stdout
	mkosiCmd.Stderr = stderr
	mkosiCmd.Env = os.Environ()
	if err := mkosiCmd.Run(); err != nil {
		return fmt.Errorf("mkosi build failed: %w", err)
	}

	if opts.SkipRelayout {
		return nil
	}

	relayout := filepath.Join(dir, "scripts", "relayout.sh")
	if _, err := os.Stat(relayout); err != nil {
		return fmt.Errorf("relayout script not found at %s: %w", relayout, err)
	}
	imagePath := filepath.Join(dir, "mkosi.output", "testbox.raw")
	if _, err := os.Stat(imagePath); err != nil {
		return fmt.Errorf("expected mkosi output at %s: %w", imagePath, err)
	}

	if os.Geteuid() != 0 {
		return fmt.Errorf("relayout step needs root (loop-mounts %s); re-run testbox build under sudo, or pass --skip-relayout to leave the rootfs flat", imagePath)
	}

	relayoutCmd := exec.Command(relayout, imagePath)
	relayoutCmd.Stdout = stdout
	relayoutCmd.Stderr = stderr
	relayoutCmd.Env = os.Environ()
	if err := relayoutCmd.Run(); err != nil {
		return fmt.Errorf("relayout failed: %w", err)
	}
	return nil
}
