// Package install writes the cleanslate raw disk image to a target. The target
// can be a block device (the normal case for deploying to real hardware) or
// a regular file (for chained image production). The implementation shells
// out to dd because it gives us free progress reporting and direct-IO
// flags.
package install

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Options configures a single install run.
type Options struct {
	// ImagePath is the source raw image. Defaults to ./mkosi.output/cleanslate.raw.
	ImagePath string
	// Target is the destination — a block device path or a regular file path.
	Target string
	// Force suppresses the confirmation prompt for block-device targets.
	Force bool
	// Stdin is used to read confirmation. Defaults to os.Stdin.
	Stdin io.Reader
	// Stdout and Stderr receive progress and informational output.
	Stdout io.Writer
	Stderr io.Writer
}

// Run writes ImagePath to Target.
func Run(opts Options) error {
	if opts.Target == "" {
		return errors.New("target is required")
	}
	if opts.ImagePath == "" {
		opts.ImagePath = filepath.Join("mkosi.output", "cleanslate.raw")
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	srcInfo, err := os.Stat(opts.ImagePath)
	if err != nil {
		return fmt.Errorf("source image: %w", err)
	}
	if srcInfo.IsDir() {
		return fmt.Errorf("source %s is a directory", opts.ImagePath)
	}

	targetInfo, targetErr := os.Stat(opts.Target)
	isBlockDevice := targetErr == nil && (targetInfo.Mode()&os.ModeDevice) != 0

	if isBlockDevice {
		if mounted, where := isMounted(opts.Target); mounted {
			return fmt.Errorf("refusing to write to %s: mounted at %s", opts.Target, where)
		}
		if root, ok := isRootDevice(opts.Target); ok {
			return fmt.Errorf("refusing to write to %s: it backs the running system's root (%s)", opts.Target, root)
		}
	}

	if isBlockDevice && !opts.Force {
		fmt.Fprintf(opts.Stdout, "About to write %s (%.1f GiB) to block device %s.\nThis will destroy any existing data on the device.\nType 'yes' to proceed: ",
			opts.ImagePath, float64(srcInfo.Size())/(1<<30), opts.Target)
		ok, err := readYes(opts.Stdin)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("aborted")
		}
	}

	cmd := exec.Command("dd",
		"if="+opts.ImagePath,
		"of="+opts.Target,
		"bs=4M",
		"status=progress",
		"conv=fsync",
		"oflag=direct",
	)
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	if err := cmd.Run(); err != nil {
		// Retry without oflag=direct — some block devices don't support it.
		cmd = exec.Command("dd",
			"if="+opts.ImagePath,
			"of="+opts.Target,
			"bs=4M",
			"status=progress",
			"conv=fsync",
		)
		cmd.Stdout = opts.Stdout
		cmd.Stderr = opts.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("dd: %w", err)
		}
	}

	if isBlockDevice {
		// Have the kernel re-read the partition table so the new GPT shows up.
		_ = exec.Command("partprobe", opts.Target).Run()
	}

	fmt.Fprintf(opts.Stdout, "wrote %s → %s\n", opts.ImagePath, opts.Target)
	return nil
}

// readYes returns true iff the next non-empty line on r is exactly "yes".
func readYes(r io.Reader) (bool, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.TrimSpace(line) == "yes", nil
}

// isMounted returns whether path appears as a mount source in /proc/self/mounts.
func isMounted(path string) (bool, string) {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return false, ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && (fields[0] == path || strings.HasPrefix(fields[0], path)) {
			return true, fields[1]
		}
	}
	return false, ""
}

// isRootDevice reports whether path backs (or is the parent of) the device
// currently mounted at /. If yes, returns (root-device, true).
func isRootDevice(path string) (string, bool) {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return "", false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		if fields[1] != "/" {
			continue
		}
		root := fields[0]
		if root == path {
			return root, true
		}
		// Strip trailing partition number to compare device-level (e.g.
		// /dev/sda2 → /dev/sda).
		parent := stripPartitionSuffix(root)
		if parent != "" && parent == path {
			return root, true
		}
	}
	return "", false
}

// stripPartitionSuffix returns the parent device of a partition path. It
// understands /dev/sdaN, /dev/nvme0n1pN, /dev/loopNpN, and similar.
func stripPartitionSuffix(path string) string {
	// Strip trailing digits.
	i := len(path)
	for i > 0 && path[i-1] >= '0' && path[i-1] <= '9' {
		i--
	}
	if i == len(path) || i == 0 {
		return ""
	}
	stripped := path[:i]
	// nvme/loop devices use 'p' as the partition separator.
	if strings.HasSuffix(stripped, "p") &&
		(strings.Contains(stripped, "nvme") || strings.Contains(stripped, "loop")) {
		return stripped[:len(stripped)-1]
	}
	return stripped
}
