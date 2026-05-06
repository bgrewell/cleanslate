package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/bgrewell/stencil"

	"github.com/bgrewell/testbox/internal/state"
)

// newStateCmd returns the `testbox state` parent command and its sub-subcommands.
func newStateCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:            "state",
		Summary:         "Manage testbox layers (named persistent btrfs subvolumes).",
		PersistentFlags: stencil.NewFlagSet(),
		Flags:           stencil.NewFlagSet(),
	}
	cmd.PersistentFlags.String("fs-root", "",
		"Path to a pre-mounted filesystem-root (subvol=/) view of the btrfs filesystem. Default: discover from / and mount a temp view (requires root).",
		"")

	listCmd := &stencil.Command{
		Name:    "list",
		Summary: "List testbox states (named persistent layers and reserved subvolumes).",
		Run: func(ctx *stencil.Context) error {
			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()
			states, err := state.List(fs)
			if err != nil {
				return err
			}
			return printStateList(os.Stdout, states)
		},
	}

	saveCmd := &stencil.Command{
		Name:    "save",
		Summary: "Snapshot a state into a new named layer.",
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
			if err := state.Save(fs, args[0], source); err != nil {
				return err
			}
			fmt.Printf("saved %q from %s\n", args[0], displaySource(source))
			return nil
		},
	}
	saveCmd.Flags.String("from", "F",
		"Source state to snapshot from. Defaults to the currently-running state ('current'). Use 'fresh' for the ephemeral working root, 'base' for the immutable base, or any named state.",
		"current")

	deleteCmd := &stencil.Command{
		Name:    "delete",
		Summary: "Remove a named state. Reserved subvolumes (@base, @runtime, @hostid) cannot be deleted.",
		Args:    stencil.ArgSpec{Min: 1, Max: 1, Names: []string{"name"}},
		Run: func(ctx *stencil.Context) error {
			args := ctx.Args
			fs, err := openFsRoot(ctx)
			if err != nil {
				return err
			}
			defer fs.Close()
			if err := state.Delete(fs, args[0]); err != nil {
				return err
			}
			fmt.Printf("deleted %q\n", args[0])
			return nil
		},
	}

	currentCmd := &stencil.Command{
		Name:    "current",
		Summary: "Print the active state name (parsed from /proc/cmdline).",
		Run: func(ctx *stencil.Context) error {
			name, err := state.Current()
			if err != nil {
				return err
			}
			fmt.Println(name)
			return nil
		},
	}

	switchCmd := &stencil.Command{
		Name:    "switch",
		Summary: "Set the next-boot target state. NOT YET IMPLEMENTED — pending S4 (bootloader installation).",
		Flags:   stencil.NewFlagSet(),
		Args:    stencil.ArgSpec{Min: 1, Max: 1, Names: []string{"name"}},
		Run: func(ctx *stencil.Context) error {
			return fmt.Errorf("state switch is not yet implemented; it depends on the bootloader work in S4")
		},
	}
	switchCmd.Flags.Bool("reboot", "r", "Reboot immediately after setting the target", false)

	cmd.Sub = []*stencil.Command{listCmd, saveCmd, deleteCmd, currentCmd, switchCmd}
	return cmd
}

// openFsRoot resolves the --fs-root flag into an FsRoot. If the flag is set,
// it is taken as a pre-mounted view; otherwise we mount one ourselves.
func openFsRoot(ctx *stencil.Context) (*state.FsRoot, error) {
	if path := ctx.Flags.String("fs-root"); path != "" {
		return state.FsRootAt(path), nil
	}
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("auto fs-root mount requires root; re-run under sudo or pass --fs-root <path>")
	}
	return state.MountFsRoot()
}

func displaySource(source string) string {
	if source == "" {
		return "current"
	}
	return source
}

func printStateList(w *os.File, states []state.State) error {
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
