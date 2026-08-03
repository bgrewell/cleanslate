package main

import (
	"os"

	"github.com/bgrewell/stencil"

	"github.com/bgrewell/cleanslate/internal/slate"
)

// Populated at build time via -ldflags (see Makefile).
var (
	appVersion    = "dev"
	appBuildDate  = ""
	appCommitHash = ""
	appBranch     = ""
)

const rootSummary = "A custom Ubuntu distro whose machines keep your work and can roll back."

// rootLong groups the verbs by what they are for. stencil sorts subcommands
// alphabetically in its help output, which buries the everyday verbs among the
// image-building ones, so the grouping is spelled out here instead.
const rootLong = `cleanslate machines run slates: named, persistent lines of work.
Boot one, work, reboot — what you did is still there. Every boot leaves a
checkpoint, so there is always a way back.

Working with slates:
  status      what you are running and where it came from
  list        the slates on this machine
  checkpoint  mark a point worth returning to
  history     the checkpoints on a slate
  rollback    return a slate to a checkpoint
  fork        start a new slate from this one
  switch      boot a different slate
  reset       return a slate to the pristine baseline
  discard     delete a slate and its checkpoints

Building and installing machines:
  build       bake a baseline image
  install     write an image to a disk`

func main() {
	app := stencil.NewApp(
		stencil.WithName(slate.AppName),
		stencil.WithDescription(rootSummary),
		stencil.WithVersionInfo(stencil.VersionInfo{
			Version:    appVersion,
			BuildDate:  appBuildDate,
			CommitHash: appCommitHash,
			Branch:     appBranch,
		}),
		stencil.WithRootCommand(newRootCmd()),
	)
	os.Exit(app.Execute(os.Args[1:]))
}

// newRootCmd is separate from main so tests can walk and drive the command
// tree without a subprocess.
func newRootCmd() *stencil.Command {
	root := &stencil.Command{
		Name:            slate.AppName,
		Summary:         rootSummary,
		Long:            rootLong,
		PersistentFlags: stencil.NewFlagSet(),
		Flags:           stencil.NewFlagSet(),
	}
	root.PersistentFlags.Bool("quiet", "q", "Print only errors and results", false)

	root.Sub = []*stencil.Command{
		newStatusCmd(),
		newListCmd(),
		newCheckpointCmd(),
		newHistoryCmd(),
		newRollbackCmd(),
		newForkCmd(),
		newSwitchCmd(),
		newResetCmd(),
		newDiscardCmd(),
		newBuildCmd(),
		newInstallCmd(),
	}
	return root
}
