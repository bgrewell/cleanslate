package main

import (
	"os"

	"github.com/bgrewell/stencil"

	"github.com/bgrewell/testbox/internal/build"
)

// Populated at build time via -ldflags (see Makefile).
var (
	appVersion    = "dev"
	appBuildDate  = ""
	appCommitHash = ""
	appBranch     = ""
)

func main() {
	root := &stencil.Command{
		Name:            "testbox",
		Summary:         "Build customizable Ubuntu 24.04 OS images with ephemeral and named persistent layers.",
		PersistentFlags: stencil.NewFlagSet(),
		Flags:           stencil.NewFlagSet(),
	}
	root.PersistentFlags.String("log-level", "l", "Log level", "info").Enum = []string{"info", "debug", "trace"}
	root.PersistentFlags.Bool("quiet", "q", "Quiet output", false)

	buildCmd := &stencil.Command{
		Name:    "build",
		Summary: "Build the base OS disk image (wraps mkosi).",
		Flags:   stencil.NewFlagSet(),
		Run: func(ctx *stencil.Context) error {
			return build.Run(build.Options{
				ConfigDir:    ctx.Flags.String("config-dir"),
				Force:        ctx.Flags.Bool("force"),
				SkipRelayout: ctx.Flags.Bool("skip-relayout"),
			})
		},
	}
	buildCmd.Flags.String("config-dir", "C", "Directory containing mkosi.conf", ".")
	buildCmd.Flags.Bool("force", "f", "Pass --force to mkosi (clear prior output)", false)
	buildCmd.Flags.Bool("skip-relayout", "", "Skip the @base/@hostid relayout step (leave rootfs flat)", false)

	root.Sub = []*stencil.Command{buildCmd}

	app := stencil.NewApp(
		stencil.WithName("testbox"),
		stencil.WithDescription("Build customizable Ubuntu 24.04 OS images with ephemeral and named persistent layers."),
		stencil.WithVersionInfo(stencil.VersionInfo{
			Version:    appVersion,
			BuildDate:  appBuildDate,
			CommitHash: appCommitHash,
			Branch:     appBranch,
		}),
		stencil.WithRootCommand(root),
	)
	os.Exit(app.Execute(os.Args[1:]))
}
