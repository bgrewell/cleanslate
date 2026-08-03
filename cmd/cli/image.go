package main

import (
	"github.com/bgrewell/stencil"

	"github.com/bgrewell/cleanslate/internal/build"
	"github.com/bgrewell/cleanslate/internal/install"
)

func newBuildCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:    "build",
		Summary: "Bake a baseline image (wraps mkosi).",
		Long: "Builds the baseline: the immutable image every slate on a machine\n" +
			"starts from. Requires root, because the last step loop-mounts the\n" +
			"produced image to lay out its subvolumes.",
		Flags: stencil.NewFlagSet(),
		Args:  noArgs("build"),
		Run: func(ctx *stencil.Context) error {
			return build.Run(build.Options{
				ConfigDir:    ctx.Flags.String("config-dir"),
				Force:        ctx.Flags.Bool("force"),
				SkipRelayout: ctx.Flags.Bool("skip-relayout"),
			})
		},
	}
	cmd.Flags.String("config-dir", "C", "Directory containing mkosi.conf", ".")
	cmd.Flags.Bool("force", "f", "Discard any previous build output first", false)
	cmd.Flags.Bool("skip-relayout", "", "Leave the image's filesystem flat, skipping the baseline layout step", false)
	return cmd
}

func newInstallCmd() *stencil.Command {
	cmd := &stencil.Command{
		Name:    "install",
		Summary: "Write a baseline image to a disk or file.",
		Long: "Writes the image byte-for-byte to the target, which destroys whatever\n" +
			"is already there. Refuses targets that are mounted or that back the\n" +
			"running system.",
		Flags: stencil.NewFlagSet(),
		Args:  stencil.ArgSpec{Min: 1, Max: 1, Names: []string{"target"}},
		Run: func(ctx *stencil.Context) error {
			return install.Run(install.Options{
				ImagePath: ctx.Flags.String("image"),
				Target:    ctx.Args[0],
				Force:     ctx.Flags.Bool("force"),
			})
		},
	}
	cmd.Flags.String("image", "i", "Image to write", "mkosi.output/cleanslate.raw")
	cmd.Flags.Bool("force", "f", "Skip the confirmation prompt when writing to a disk", false)
	return cmd
}
