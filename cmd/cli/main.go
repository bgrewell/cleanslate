package main

import (
	"os"

	"github.com/bgrewell/stencil"
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
