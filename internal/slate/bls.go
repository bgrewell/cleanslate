package slate

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// BLS (Boot Loader Specification) entries live at <ESP>/loader/entries/*.conf
// and are read directly by systemd-boot. cleanslate owns entries named
// cleanslate-<name>.conf. The entries for the default slate, scratch, and
// rescue are written at build time by scripts/relayout.sh; one entry per
// additional slate is written here.
//
// Entries this tool does not own — those written by the rootfs's kernel-package
// postinst, e.g. cleanslate-6.8.0-111-generic.conf — are never edited.

// DefaultESPPath returns the conventional ESP mount path on Ubuntu/Debian.
// Callers may override via the --esp CLI flag.
const DefaultESPPath = "/boot"

// blsTemplate is the entry cloned as the basis for new ones. It is written at
// build time and is the only place the build-time facts — kernel version,
// initrd path, root=PARTUUID, console arguments — are recorded, none of which
// can be rederived reliably at runtime.
func blsTemplate(esp string) string {
	return EntryPath(esp, TemplateEntry)
}

// WriteBLSEntry creates a BLS entry for the named slate by cloning the
// template entry and rewriting the rootflags=subvol= flag plus the title. The
// slate's on-disk subvolume is "@"+name.
func WriteBLSEntry(esp, name string) error {
	src, err := os.ReadFile(blsTemplate(esp))
	if err != nil {
		return fmt.Errorf("read BLS template %s: %w", blsTemplate(esp), err)
	}

	subvol := "@" + name
	out := rewriteBLSEntry(string(src), name, subvol)

	dst := EntryPath(esp, name)
	if err := os.WriteFile(dst, []byte(out), 0644); err != nil {
		return fmt.Errorf("write BLS entry %s: %w", dst, err)
	}
	return nil
}

// DeleteBLSEntry removes the BLS entry for the named state. Missing entry
// is not an error.
func DeleteBLSEntry(esp, name string) error {
	if err := os.Remove(EntryPath(esp, name)); err != nil && !os.IsNotExist(err) {
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
			lines = append(lines, "title    cleanslate: "+name)
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
//	rootflags=compress=zstd,subvol=@runtime  →  rootflags=compress=zstd,subvol=@baseline
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
