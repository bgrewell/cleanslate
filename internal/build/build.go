// Package build wraps mkosi to produce testbox base disk images.
//
// The package is a thin orchestrator: it locates a mkosi config directory,
// validates that mkosi is installed with a recent-enough version, and shells
// out to mkosi to do the actual build. The post-build subvolume layout
// (@base, @hostid) is performed by mkosi.postoutput, which is referenced from
// the project's mkosi.conf.d/ tree, not by this package.
package build

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Options configures a single mkosi invocation.
type Options struct {
	// ConfigDir is the directory containing mkosi.conf. Defaults to "." when empty.
	ConfigDir string

	// Force passes --force to mkosi, clearing prior outputs before building.
	Force bool

	// Stdout and Stderr receive mkosi's output. Both default to the process streams.
	Stdout io.Writer
	Stderr io.Writer
}

// Run executes a mkosi build using the given options.
func Run(opts Options) error {
	dir := opts.ConfigDir
	if dir == "" {
		dir = "."
	}

	conf := filepath.Join(dir, "mkosi.conf")
	if _, err := os.Stat(conf); err != nil {
		return fmt.Errorf("no mkosi.conf in %q: %w", dir, err)
	}

	if _, err := exec.LookPath("mkosi"); err != nil {
		return errors.New("mkosi not found in PATH (need v26 or newer); install from https://github.com/systemd/mkosi or via the openSUSE Build Service apt repo")
	}

	args := []string{"--directory", dir}
	if opts.Force {
		args = append(args, "--force")
	}
	args = append(args, "build")

	cmd := exec.Command("mkosi", args...)
	cmd.Stdout = opts.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = opts.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mkosi build failed: %w", err)
	}
	return nil
}
