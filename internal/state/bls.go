package state

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BLS (Boot Loader Specification) entries live at <ESP>/loader/entries/*.conf
// and are read directly by systemd-boot. testbox owns entries named
// testbox-<state>.conf — testbox-fresh and testbox-base are written at build
// time by scripts/relayout.sh; testbox-<name> per named state is written
// here when `testbox state save` runs.
//
// We never edit files we don't own; entries written by the rootfs's normal
// kernel-package postinst (e.g. testbox-6.8.0-111-generic.conf) are left
// alone.

// DefaultESPPath returns the conventional ESP mount path on Ubuntu/Debian.
// Callers may override via the --esp CLI flag.
const DefaultESPPath = "/boot"

// blsTemplate is the testbox-fresh.conf path used as a template for new
// entries. We copy it verbatim and rewrite the rootflags=subvol= portion.
func blsTemplate(esp string) string {
	return filepath.Join(esp, "loader", "entries", "testbox-fresh.conf")
}

// blsEntryPath returns <esp>/loader/entries/testbox-<name>.conf.
func blsEntryPath(esp, name string) string {
	return filepath.Join(esp, "loader", "entries", "testbox-"+name+".conf")
}

// WriteBLSEntry creates a BLS entry for the named state by cloning the
// testbox-fresh.conf template and rewriting the rootflags=subvol= flag plus
// the title. The state's on-disk subvolume is "@"+name.
func WriteBLSEntry(esp, name string) error {
	src, err := os.ReadFile(blsTemplate(esp))
	if err != nil {
		return fmt.Errorf("read BLS template %s: %w", blsTemplate(esp), err)
	}

	subvol := "@" + name
	out := rewriteBLSEntry(string(src), name, subvol)

	dst := blsEntryPath(esp, name)
	if err := os.WriteFile(dst, []byte(out), 0644); err != nil {
		return fmt.Errorf("write BLS entry %s: %w", dst, err)
	}
	return nil
}

// DeleteBLSEntry removes the BLS entry for the named state. Missing entry
// is not an error.
func DeleteBLSEntry(esp, name string) error {
	if err := os.Remove(blsEntryPath(esp, name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete BLS entry: %w", err)
	}
	return nil
}

// rewriteBLSEntry applies title and rootflags=subvol= rewrites to a BLS
// entry's contents. Other lines pass through unchanged.
func rewriteBLSEntry(input, name, subvol string) string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(strings.TrimLeft(line, " \t"), "title"):
			lines = append(lines, "title    testbox: "+name)
		case strings.HasPrefix(strings.TrimLeft(line, " \t"), "options"):
			lines = append(lines, rewriteOptionsLine(line, subvol))
		default:
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// rewriteOptionsLine takes an "options ..." line from a BLS entry and
// replaces any rootflags=...subvol=... (whole rootflags= field) with one
// pointing at the new subvolume. Other tokens are preserved.
func rewriteOptionsLine(line, subvol string) string {
	// Preserve leading whitespace before "options".
	trimmed := strings.TrimLeft(line, " \t")
	prefix := line[:len(line)-len(trimmed)]
	if !strings.HasPrefix(trimmed, "options") {
		return line
	}
	rest := strings.TrimSpace(trimmed[len("options"):])

	var fields []string
	replaced := false
	for _, f := range strings.Fields(rest) {
		if strings.HasPrefix(f, "rootflags=") {
			fields = append(fields, replaceSubvolToken(f, subvol))
			replaced = true
			continue
		}
		fields = append(fields, f)
	}
	if !replaced {
		fields = append(fields, "rootflags=subvol="+subvol)
	}
	return prefix + "options  " + strings.Join(fields, " ")
}

// replaceSubvolToken replaces the subvol= component inside a rootflags= field
// while leaving other rootflags components alone. e.g.
//
//	rootflags=compress=zstd,subvol=@runtime  →  rootflags=compress=zstd,subvol=@base
func replaceSubvolToken(rootflags, subvol string) string {
	body := strings.TrimPrefix(rootflags, "rootflags=")
	parts := strings.Split(body, ",")
	for i, p := range parts {
		if strings.HasPrefix(p, "subvol=") {
			parts[i] = "subvol=" + subvol
			return "rootflags=" + strings.Join(parts, ",")
		}
	}
	parts = append(parts, "subvol="+subvol)
	return "rootflags=" + strings.Join(parts, ",")
}
