package slate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// espCandidates are the paths systemd-gpt-auto-generator will mount the EFI
// system partition on, in the order it prefers them. The generator mounts the
// ESP at /efi when that directory exists and is empty, and falls back to /boot
// only when it does not — and images built here ship an empty /efi, so /efi is
// where the ESP actually lands. A hardcoded /boot silently produced boot
// entries written to a directory nothing reads.
var espCandidates = []string{"/efi", "/boot"}

// DefaultESPPath is the fallback when the ESP cannot be located, kept so error
// messages name a plausible path rather than an empty string.
const DefaultESPPath = "/efi"

// DetectESP returns the mounted EFI system partition. bootctl is asked first
// because it consults the firmware's own idea of where it booted from; the
// directory probe covers the case where bootctl is unavailable or the machine
// booted without EFI variables (a qemu run with no NVRAM, for instance).
//
// A candidate counts only if it holds loader/entries, which distinguishes a
// mounted ESP from an empty mountpoint waiting for one.
func DetectESP() (string, error) {
	if out, err := exec.Command("bootctl", "--print-esp-path").Output(); err == nil {
		if p := strings.TrimSpace(string(out)); p != "" && isESP(p) {
			return p, nil
		}
	}
	for _, p := range espCandidates {
		if isESP(p) {
			return p, nil
		}
	}
	return "", &espNotFoundError{}
}

func isESP(path string) bool {
	fi, err := os.Stat(filepath.Join(path, "loader", "entries"))
	return err == nil && fi.IsDir()
}

type espNotFoundError struct{}

func (e *espNotFoundError) Error() string {
	return "no mounted EFI system partition found at /efi or /boot; " +
		"pass --esp <path> if it is mounted elsewhere"
}
