package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bgrewell/stencil"

	"github.com/bgrewell/cleanslate/internal/slate"
)

// noArgs rejects stray positionals. A zero-value ArgSpec means "unlimited" in
// stencil, so commands that take no arguments have to say so or they silently
// accept anything.
func noArgs(name string) stencil.ArgSpec {
	return stencil.ArgSpec{Validate: func(args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("%s takes no arguments", name)
		}
		return nil
	}}
}

// fsRootFlag is registered on commands that read or change the slate store.
func fsRootFlag(fs *stencil.FlagSet) {
	fs.String("fs-root", "",
		"Pre-mounted view of the filesystem at subvol=/. Discovered automatically when unset, which needs root.",
		"")
}

// bootFlags is registered on commands that create or remove boot entries. The
// flag is named `bls` with a true default rather than `no-bls`: stencil parses
// --no-<name> by stripping the prefix and looking up the bare name, so a flag
// literally named "no-bls" can never be reached.
func bootFlags(fs *stencil.FlagSet) {
	fs.String("esp", "", "Path to the mounted EFI system partition. Discovered automatically when unset.", "")
	fs.Bool("bls", "", "Keep boot entries in step with the slates. Pass --no-bls to leave the bootloader alone.", true)
}

func newStatusCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:    "status",
		Summary: "Show what this machine is running.",
		Flags:   stencil.NewFlagSet(),
		Args:    noArgs("status"),
		Run: func(ctx *stencil.Context) error {
			b, err := slate.ReadBooted()
			if err != nil {
				return fmt.Errorf("this does not look like a cleanslate machine: %w", err)
			}
			return printStatus(os.Stdout, b, pendingFor(ctx, b))
		},
	}
	fsRootFlag(cmd.Flags)
	return cmd
}

// pendingFor reports a staged replacement when one can be read. status must
// work unprivileged, so failing to open the store is not an error here — it
// just means one less line of output.
func pendingFor(ctx *stencil.Context, b slate.Booted) *slate.Pending {
	fs, err := openFsRoot(ctx)
	if err != nil {
		return nil
	}
	defer fs.Close()
	if p, ok := slate.ReadPending(fs); ok {
		return &p
	}
	return nil
}

func newListCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:    "list",
		Summary: "List the slates on this machine.",
		Aliases: []string{"ls"},
		Flags:   stencil.NewFlagSet(),
		Args:    noArgs("list"),
		Run: func(ctx *stencil.Context) error {
			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()

			slates, err := slate.Slates(fs)
			if err != nil {
				return err
			}
			checkpoints, err := slate.ListCheckpoints(fs, "")
			if err != nil {
				return err
			}
			booted, _ := slate.ReadBooted()
			return printSlates(os.Stdout, slates, checkpoints, booted)
		},
	}
	fsRootFlag(cmd.Flags)
	return cmd
}

func newCheckpointCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:    "checkpoint",
		Summary: "Mark a point on this slate worth returning to.",
		Long: "Checkpoints made this way are kept until you remove them. The ones\n" +
			"taken automatically at every boot roll off as newer ones arrive, so a\n" +
			"state you want to keep is worth marking deliberately.",
		Flags: stencil.NewFlagSet(),
		Args:  noArgs("checkpoint"),
		Run: func(ctx *stencil.Context) error {
			b, err := slate.ReadBooted()
			if err != nil {
				return fmt.Errorf("this does not look like a cleanslate machine: %w", err)
			}
			if b.Mode != slate.ModePersistent {
				return fmt.Errorf("a %s boot has no slate to checkpoint", b.Mode)
			}

			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()

			message := ctx.Flags.String("message")
			if message == "" {
				message = "kept"
			}
			c, err := slate.CreateCheckpoint(fs, b.Name, message)
			if err != nil {
				return err
			}
			fmt.Printf("checkpoint %s on %s\n", c.Ref(), c.Slate)
			return nil
		},
	}
	cmd.Flags.String("message", "m", "What this checkpoint is, so it means something later", "")
	fsRootFlag(cmd.Flags)
	return cmd
}

func newHistoryCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:    "history",
		Summary: "Show the checkpoints on a slate.",
		Flags:   stencil.NewFlagSet(),
		Args:    stencil.ArgSpec{Min: 0, Max: 1, Names: []string{"slate"}},
		Run: func(ctx *stencil.Context) error {
			name := ""
			if len(ctx.Args) == 1 {
				name = ctx.Args[0]
			} else if b, err := slate.ReadBooted(); err == nil && b.Mode == slate.ModePersistent {
				name = b.Name
			}

			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()

			checkpoints, err := slate.ListCheckpoints(fs, name)
			if err != nil {
				return err
			}
			return printHistory(os.Stdout, checkpoints, name)
		},
	}
	fsRootFlag(cmd.Flags)
	return cmd
}

func newRollbackCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:    "rollback",
		Summary: "Return a slate to one of its checkpoints.",
		Long: "Takes effect at the next boot: a slate is the running root and cannot\n" +
			"be replaced underneath itself. The state being replaced is checkpointed\n" +
			"first, so a rollback can be rolled back.",
		Flags: stencil.NewFlagSet(),
		Args:  stencil.ArgSpec{Min: 1, Max: 1, Names: []string{"checkpoint"}},
		Run: func(ctx *stencil.Context) error {
			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()

			defaultSlate := ""
			if b, err := slate.ReadBooted(); err == nil {
				defaultSlate = b.Name
			}
			c, err := slate.FindCheckpoint(fs, ctx.Args[0], defaultSlate)
			if err != nil {
				return err
			}
			if err := slate.Rollback(fs, c); err != nil {
				return err
			}
			fmt.Printf("%s will return to checkpoint %s at the next boot\n", c.Slate, c.Ref())
			return maybeReboot(ctx)
		},
	}
	cmd.Flags.Bool("reboot", "r", "Reboot now instead of at your convenience", false)
	fsRootFlag(cmd.Flags)
	return cmd
}

func newForkCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:    "fork",
		Summary: "Start a new slate from this one.",
		Long: "The new slate begins as a copy and diverges from there; the original\n" +
			"is untouched. Forking is how a setup worth keeping becomes its own\n" +
			"line of work instead of accumulating in the one you are on.",
		Flags: stencil.NewFlagSet(),
		Args:  stencil.ArgSpec{Min: 1, Max: 1, Names: []string{"name"}},
		Run: func(ctx *stencil.Context) error {
			name := ctx.Args[0]
			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()

			if err := slate.Fork(fs, name, ctx.Flags.String("from")); err != nil {
				return err
			}
			fmt.Printf("created slate %s\n", name)

			if ctx.Flags.Bool("bls") {
				esp, err := resolveESP(ctx.Flags.String("esp"))
				if err != nil {
					return fmt.Errorf("slate %s exists but has no boot entry: %w", name, err)
				}
				if err := slate.WriteBLSEntry(esp, name); err != nil {
					return fmt.Errorf("slate %s exists but has no boot entry: %w", name, err)
				}
				fmt.Printf("added boot entry %s\n", slate.EntryName(name))
			}
			return nil
		},
	}
	cmd.Flags.String("from", "F", "Slate, checkpoint, or 'baseline' to fork from. Defaults to the running slate.", "")
	fsRootFlag(cmd.Flags)
	bootFlags(cmd.Flags)
	return cmd
}

func newSwitchCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:    "switch",
		Summary: "Boot a different slate.",
		Long: "Sets the next boot only; the machine returns to its usual slate after\n" +
			"that. With --scratch the slate is copied for a throwaway run and\n" +
			"whatever happens is discarded, leaving the slate itself untouched.",
		Flags: stencil.NewFlagSet(),
		Args:  stencil.ArgSpec{Min: 1, Max: 1, Names: []string{"name"}},
		Run: func(ctx *stencil.Context) error {
			name := ctx.Args[0]
			target := name
			if ctx.Flags.Bool("scratch") {
				// The scratch entry boots a disposable copy of the baseline,
				// so a throwaway run leaves every slate untouched.
				target = "scratch"
			}

			// Checked before handing off, so a typo produces a name rather
			// than an opaque bootctl failure.
			if esp, err := resolveESP(ctx.Flags.String("esp")); err == nil {
				if _, statErr := os.Stat(slate.EntryPath(esp, target)); statErr != nil {
					return fmt.Errorf("no boot entry for %q; run `cleanslate list` to see what exists", name)
				}
			}

			entry := slate.EntryName(target)
			out, err := exec.Command("bootctl", "set-oneshot", entry).CombinedOutput()
			if err != nil {
				return fmt.Errorf("could not set the next boot: %w (%s)", err, strings.TrimSpace(string(out)))
			}
			fmt.Printf("next boot: %s\n", name)
			return maybeReboot(ctx)
		},
	}
	cmd.Flags.Bool("reboot", "r", "Reboot now instead of at your convenience", false)
	cmd.Flags.Bool("scratch", "", "Take a throwaway run: changes are discarded and the slate is left alone", false)
	bootFlags(cmd.Flags)
	return cmd
}

func newResetCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:    "reset",
		Summary: "Return a slate to the pristine baseline.",
		Long: "This is how a machine is handed to someone else. The baseline is never\n" +
			"pruned, so however far a slate has drifted this always works. The state\n" +
			"being replaced is checkpointed first and takes effect at the next boot.",
		Flags: stencil.NewFlagSet(),
		Args:  stencil.ArgSpec{Min: 0, Max: 1, Names: []string{"slate"}},
		Run: func(ctx *stencil.Context) error {
			name := ""
			if len(ctx.Args) == 1 {
				name = ctx.Args[0]
			} else if b, err := slate.ReadBooted(); err == nil && b.Mode == slate.ModePersistent {
				name = b.Name
			}
			if name == "" {
				return fmt.Errorf("name the slate to reset")
			}

			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()

			if !ctx.Flags.Bool("force") {
				if err := confirm(fmt.Sprintf(
					"Reset %q to the baseline? Everything on it goes back to the freshly built image.\n"+
						"Its current state is checkpointed first, so this is reversible.\n"+
						"Type the slate name to confirm: ", name), name); err != nil {
					return err
				}
			}
			if err := slate.Reset(fs, name); err != nil {
				return err
			}
			fmt.Printf("%s will return to the baseline at the next boot\n", name)
			return maybeReboot(ctx)
		},
	}
	cmd.Flags.Bool("force", "f", "Skip the confirmation prompt", false)
	cmd.Flags.Bool("reboot", "r", "Reboot now instead of at your convenience", false)
	fsRootFlag(cmd.Flags)
	return cmd
}

func newDiscardCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:    "discard",
		Summary: "Delete a slate and its checkpoints.",
		Aliases: []string{"delete", "rm"},
		Flags:   stencil.NewFlagSet(),
		Args:    stencil.ArgSpec{Min: 1, Max: 1, Names: []string{"name"}},
		Run: func(ctx *stencil.Context) error {
			name := ctx.Args[0]
			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()

			checkpoints, err := slate.ListCheckpoints(fs, name)
			if err != nil {
				return err
			}
			if !ctx.Flags.Bool("force") {
				if err := confirm(fmt.Sprintf(
					"Delete slate %q and its %d checkpoint(s)? This cannot be undone.\n"+
						"Type the slate name to confirm: ", name, len(checkpoints)), name); err != nil {
					return err
				}
			}

			if err := slate.Discard(fs, name); err != nil {
				return err
			}
			fmt.Printf("discarded %s\n", name)

			if ctx.Flags.Bool("bls") {
				esp, err := resolveESP(ctx.Flags.String("esp"))
				if err == nil {
					if err := slate.DeleteBLSEntry(esp, name); err != nil {
						fmt.Fprintf(os.Stderr, "warning: boot entry left behind: %v\n", err)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags.Bool("force", "f", "Skip the confirmation prompt", false)
	fsRootFlag(cmd.Flags)
	bootFlags(cmd.Flags)
	return cmd
}

// openFsRoot resolves --fs-root into a mounted view of the filesystem root.
func openFsRoot(ctx *stencil.Context) (*slate.FsRoot, error) {
	if path := ctx.Flags.String("fs-root"); path != "" {
		return slate.FsRootAt(path), nil
	}
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("this needs root; re-run under sudo or pass --fs-root <path>")
	}
	return slate.MountFsRoot()
}

// resolveESP honours an explicit --esp and otherwise discovers the mounted EFI
// system partition.
func resolveESP(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	return slate.DetectESP()
}

func maybeReboot(ctx *stencil.Context) error {
	if ctx.Flags.Bool("reboot") {
		fmt.Println("rebooting")
		return exec.Command("systemctl", "reboot").Run()
	}
	fmt.Println("reboot when you are ready, or pass --reboot to do it now")
	return nil
}

// confirm requires the exact word back. Destructive operations fail closed
// when there is nobody to ask, so a script has to pass --force to mean it.
func confirm(prompt, want string) error {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("refusing to continue without confirmation; pass --force")
	}
	fmt.Print(prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return fmt.Errorf("aborted")
	}
	if strings.TrimSpace(line) != want {
		return fmt.Errorf("aborted")
	}
	return nil
}
