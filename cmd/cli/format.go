package main

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/bgrewell/cleanslate/internal/slate"
)

// These take an io.Writer and no other state so their output can be asserted
// on directly. The wording here is the whole user-facing surface of the tool,
// and golden tests over it are what stop it drifting back into mechanism.

func printStatus(w io.Writer, b slate.Booted, pending *slate.Pending) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	fmt.Fprintf(tw, "slate\t%s\n", b.Name)

	switch b.Mode {
	case slate.ModePersistent:
		fmt.Fprintf(tw, "mode\tpersistent — changes here are kept\n")
	case slate.ModeScratch:
		// The one place the word appears in routine output, because this is
		// the case where not knowing costs someone their work.
		fmt.Fprintf(tw, "mode\tscratch — everything written here is discarded at reboot\n")
	case slate.ModeRescue:
		fmt.Fprintf(tw, "mode\trescue — the baseline, mounted read-only\n")
	default:
		fmt.Fprintf(tw, "mode\tunknown\n")
	}

	if !b.BootedAt.IsZero() {
		fmt.Fprintf(tw, "booted\t%s (%s ago)\n",
			b.BootedAt.Format("2006-01-02 15:04 UTC"), humanDuration(time.Since(b.BootedAt)))
	}
	if b.Checkpoint != "" {
		fmt.Fprintf(tw, "checkpoint\ttaken at boot\n")
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if b.Diverged() {
		fmt.Fprintf(w, "\nThe boot entry asked for %s, which no longer exists; this booted %s instead.\n",
			b.Requested, b.Basis)
	}
	if pending != nil {
		fmt.Fprintf(w, "\nPending: %s, applied at the next boot.\n", pending.Reason)
	}
	if b.FromCmdline {
		fmt.Fprintf(w, "\nRead from the kernel command line; this machine may predate the boot record.\n")
	}
	return nil
}

func printSlates(w io.Writer, slates []slate.Slate, checkpoints []slate.Checkpoint, booted slate.Booted) error {
	if len(slates) == 0 {
		fmt.Fprintln(w, "no slates")
		return nil
	}

	counts := map[string]int{}
	for _, c := range checkpoints {
		counts[c.Slate]++
	}
	byUUID := map[string]string{}
	for _, s := range slates {
		byUUID[s.UUID] = s.Name
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tFROM\tCHECKPOINTS\t")
	for _, s := range slates {
		from := "—"
		if n, ok := byUUID[s.ParentUUID]; ok && s.ParentUUID != "" {
			from = n
		}
		marker := ""
		if s.Subvolume == booted.Basis {
			marker = "← running"
		}
		count := "—"
		if n := counts[s.Name]; n > 0 {
			count = fmt.Sprintf("%d", n)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Name, from, count, marker)
	}
	return tw.Flush()
}

func printHistory(w io.Writer, checkpoints []slate.Checkpoint, slateName string) error {
	if len(checkpoints) == 0 {
		if slateName != "" {
			fmt.Fprintf(w, "no checkpoints on %s\n", slateName)
		} else {
			fmt.Fprintln(w, "no checkpoints")
		}
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "REF\tWHEN\tKEPT\tWHAT")
	for _, c := range checkpoints {
		when := "—"
		if !c.CreatedAt.IsZero() {
			when = c.CreatedAt.Format("2006-01-02 15:04")
		}
		kept := "no"
		if !c.Automatic() {
			kept = "yes"
		}
		what := c.Message
		if what == "" {
			what = "taken at boot"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.Ref(), when, kept, what)
	}
	return tw.Flush()
}

// humanDuration renders a coarse age. Precision past the leading unit is
// noise for "how long have I been on this slate".
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}
