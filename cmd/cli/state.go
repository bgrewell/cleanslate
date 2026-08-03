package main

import (
	"fmt"
	"os"
	"os/exec"
	"text/tabwriter"

	"github.com/bgrewell/stencil"

	"github.com/bgrewell/cleanslate/internal/slate"
)

// newStateCmd returns the `cleanslate state` parent command and its sub-subcommands.
func newStateCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:            "state",
		Summary:         "Manage cleanslate layers (named persistent btrfs subvolumes).",
		PersistentFlags: stencil.NewFlagSet(),
		Flags:           stencil.NewFlagSet(),
	}
	cmd.PersistentFlags.String("fs-root", "",
		"Path to a pre-mounted filesystem-root (subvol=/) view of the btrfs filesystem. Default: discover from / and mount a temp view (requires root).",
		"")
	cmd.PersistentFlags.String("esp", "",
		"Path to the mounted ESP. Default: "+slate.DefaultESPPath+". Used for boot-loader entry management on save/delete/switch.",
		slate.DefaultESPPath)
	cmd.PersistentFlags.Bool("no-bls", "",
		"Skip Boot Loader Specification entry management on save/delete. Use when operating on a non-running image without an ESP available, or when only the btrfs side should change.",
		false)

	listCmd := &stencil.Command{
		Name:    "list",
		Summary: "List cleanslate states (named persistent layers and reserved subvolumes).",
		Run: func(ctx *stencil.Context) error {
			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()
			states, err := slate.List(fs)
			if err != nil {
				return err
			}
			return printSlateList(os.Stdout, states)
		},
	}

	saveCmd := &stencil.Command{
		Name:    "save",
		Summary: "Snapshot a state into a new named layer and add a boot-loader entry for it.",
		Flags:   stencil.NewFlagSet(),
		Args:    stencil.ArgSpec{Min: 1, Max: 1, Names: []string{"name"}},
		Run: func(ctx *stencil.Context) error {
			args := ctx.Args
			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()

			source := ctx.Flags.String("from")
			if err := slate.Save(fs, args[0], source); err != nil {
				return err
			}
			fmt.Printf("saved %q from %s\n", args[0], displaySource(source))

			if !ctx.Flags.Bool("no-bls") {
				esp := ctx.Flags.String("esp")
				if err := slate.WriteBLSEntry(esp, args[0]); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to write BLS entry: %v\n", err)
				} else {
					fmt.Printf("added boot-loader entry cleanslate-%s.conf\n", args[0])
				}
			}
			return nil
		},
	}
	saveCmd.Flags.String("from", "F",
		"Source state to snapshot from. Defaults to the currently-running state ('current'). Use 'fresh' for the ephemeral working root, 'base' for the immutable base, or any named state.",
		"current")

	deleteCmd := &stencil.Command{
		Name:    "delete",
		Summary: "Remove a named state and its boot-loader entry. Reserved subvolumes (@base, @runtime, @hostid) cannot be deleted.",
		Args:    stencil.ArgSpec{Min: 1, Max: 1, Names: []string{"name"}},
		Run: func(ctx *stencil.Context) error {
			args := ctx.Args
			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()

			if err := slate.Delete(fs, args[0]); err != nil {
				return err
			}
			fmt.Printf("deleted %q\n", args[0])

			if !ctx.Flags.Bool("no-bls") {
				esp := ctx.Flags.String("esp")
				if err := slate.DeleteBLSEntry(esp, args[0]); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to remove BLS entry: %v\n", err)
				}
			}
			return nil
		},
	}

	currentCmd := &stencil.Command{
		Name:    "current",
		Summary: "Print the active state name (parsed from /proc/cmdline).",
		Run: func(ctx *stencil.Context) error {
			name, err := slate.Current()
			if err != nil {
				return err
			}
			fmt.Println(name)
			return nil
		},
	}

	switchCmd := &stencil.Command{
		Name:    "switch",
		Summary: "Set the next-boot target state via systemd-boot one-shot. The default boot target is unchanged.",
		Flags:   stencil.NewFlagSet(),
		Args:    stencil.ArgSpec{Min: 1, Max: 1, Names: []string{"name"}},
		Run: func(ctx *stencil.Context) error {
			name := ctx.Args[0]
			entry := slate.EntryName(name)
			cmd := exec.Command("bootctl", "set-oneshot", entry)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("bootctl set-oneshot %s: %w", entry, err)
			}
			fmt.Printf("next boot will be: %s\n", name)
			if ctx.Flags.Bool("reboot") {
				fmt.Println("rebooting...")
				return exec.Command("systemctl", "reboot").Run()
			}
			fmt.Println("(reboot at your convenience to switch; pass --reboot to do it now)")
			return nil
		},
	}
	switchCmd.Flags.Bool("reboot", "r", "Reboot immediately after setting the next-boot target", false)

	cmd.Sub = []*stencil.Command{listCmd, saveCmd, deleteCmd, currentCmd, switchCmd}
	return cmd
}

// openFsRoot resolves the --fs-root flag into an FsRoot. If the flag is set,
// it is taken as a pre-mounted view; otherwise we mount one ourselves.
func openFsRoot(ctx *stencil.Context) (*slate.FsRoot, error) {
	if path := ctx.Flags.String("fs-root"); path != "" {
		return slate.FsRootAt(path), nil
	}
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("auto fs-root mount requires root; re-run under sudo or pass --fs-root <path>")
	}
	return slate.MountFsRoot()
}

func displaySource(source string) string {
	if source == "" {
		return "current"
	}
	return source
}

func printSlateList(w *os.File, states []slate.Slate) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSUBVOLUME\tRESERVED\tPARENT-UUID")
	for _, s := range states {
		parent := s.ParentUUID
		if parent == "" {
			parent = "—"
		}
		reserved := "no"
		if s.Reserved {
			reserved = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Name, s.Subvolume, reserved, parent)
	}
	return tw.Flush()
}
