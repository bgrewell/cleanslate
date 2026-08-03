package slate

import "path/filepath"

// AppName is the product name. It is the CLI binary name, the prefix on every
// boot-loader entry this tool owns, and the runtime state directory name.
const AppName = "cleanslate"

// EntryPrefix prefixes the Boot Loader Specification entry filenames owned by
// this tool, which is how they are told apart from entries written by the
// distribution's kernel postinst.
const EntryPrefix = AppName + "-"

// DefaultSlate is the slate created from the baseline on a machine's first
// boot and used as the default boot target. Its presence is what lets a fresh
// install behave like an ordinary Ubuntu box without anyone having to know
// slates exist.
const DefaultSlate = "main"

// TemplateEntry names the boot-loader entry cloned when generating entries for
// new slates.
const TemplateEntry = DefaultSlate

// RunDir holds the boot-time facts recorded by the initramfs hook: which slate
// was booted, in what mode, and from what basis. Populated before pivot and
// carried into the booted system, so it is authoritative where /proc/cmdline
// only records what was requested.
const RunDir = "/run/" + AppName

// EntryName returns the boot-loader entry filename for a slate.
func EntryName(name string) string {
	return EntryPrefix + name + ".conf"
}

// EntryPath returns the full path to a slate's boot-loader entry under the
// mounted EFI system partition. Single source of truth: the filename was
// previously assembled independently in two places, which the rename would
// have been free to break in one and not the other.
func EntryPath(esp, name string) string {
	return filepath.Join(esp, "loader", "entries", EntryName(name))
}
