package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/bgrewell/cleanslate/internal/slate"
)

// The wording is the product surface. These lock the vocabulary in place so a
// later edit cannot quietly reintroduce "state", "layer", or "snapshot", and
// so the one place "scratch" is allowed to appear in routine output stays the
// one place it appears.

var retiredWords = []string{"state", "layer", "snapshot", "subvolume", "ephemeral", "dirty"}

func assertNoRetiredWords(t *testing.T, got string) {
	t.Helper()
	lower := strings.ToLower(got)
	for _, w := range retiredWords {
		if strings.Contains(lower, w) {
			t.Errorf("output uses retired word %q:\n%s", w, got)
		}
	}
}

func TestStatusPersistent(t *testing.T) {
	var buf bytes.Buffer
	b := slate.Booted{
		Name:       "pg-tuned",
		Mode:       slate.ModePersistent,
		Basis:      "@pg-tuned",
		Checkpoint: "@pg-tuned.ckpt.0007.auto",
		BootedAt:   time.Now().UTC().Add(-3 * time.Hour),
	}
	if err := printStatus(&buf, b, nil, nil); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	for _, want := range []string{"pg-tuned", "persistent", "changes here are kept"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Persistent boots must not talk about discarding anything.
	if strings.Contains(got, "discarded") {
		t.Errorf("persistent status mentions discarding:\n%s", got)
	}
	assertNoRetiredWords(t, got)
}

func TestStatusScratchWarnsAboutLoss(t *testing.T) {
	var buf bytes.Buffer
	b := slate.Booted{Name: "baseline", Mode: slate.ModeScratch, Basis: "@baseline"}
	if err := printStatus(&buf, b, nil, nil); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	// This is the one case where not knowing costs someone their work, so the
	// consequence has to be stated, not just the mode name.
	if !strings.Contains(got, "discarded at reboot") {
		t.Errorf("scratch status does not say the work is lost:\n%s", got)
	}
}

func TestStatusReportsFallbackAndPending(t *testing.T) {
	var buf bytes.Buffer
	b := slate.Booted{
		Name: "baseline", Mode: slate.ModePersistent,
		Basis: "@baseline", Requested: "@gone",
	}
	p := slate.Pending{Slate: "@main", Source: "@main.ckpt.0003.keep", Reason: "rollback to checkpoint main.3"}
	if err := printStatus(&buf, b, &p, nil); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	if !strings.Contains(got, "@gone") || !strings.Contains(got, "no longer exists") {
		t.Errorf("a boot that fell back should say so:\n%s", got)
	}
	if !strings.Contains(got, "rollback to checkpoint main.3") {
		t.Errorf("a staged replacement should be reported:\n%s", got)
	}
}

func TestListSlates(t *testing.T) {
	var buf bytes.Buffer
	slates := []slate.Slate{
		{Name: "baseline", Subvolume: "@baseline", UUID: "u-base"},
		{Name: "main", Subvolume: "@main", UUID: "u-main", ParentUUID: "u-base"},
		{Name: "pg-tuned", Subvolume: "@pg-tuned", UUID: "u-pg", ParentUUID: "u-main"},
	}
	checkpoints := []slate.Checkpoint{
		{Slate: "main", Seq: 1, Class: slate.ClassAuto},
		{Slate: "main", Seq: 2, Class: slate.ClassKeep},
	}
	booted := slate.Booted{Name: "main", Basis: "@main", Mode: slate.ModePersistent}

	if err := printSlates(&buf, slates, checkpoints, booted, nil); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	for _, want := range []string{"NAME", "FROM", "CHECKPOINTS", "baseline", "pg-tuned", "← running"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Lineage is the column that makes fork history legible, and it is free:
	// btrfs already records the parent UUID.
	if !strings.Contains(got, "main") {
		t.Errorf("pg-tuned should report main as its origin:\n%s", got)
	}
	assertNoRetiredWords(t, got)
}

func TestHistory(t *testing.T) {
	var buf bytes.Buffer
	when := time.Date(2026, 8, 1, 9, 31, 0, 0, time.UTC)
	checkpoints := []slate.Checkpoint{
		{Slate: "main", Seq: 1, Class: slate.ClassAuto, CreatedAt: when},
		{Slate: "main", Seq: 2, Class: slate.ClassKeep, CreatedAt: when, Message: "benchmarks pass"},
	}
	if err := printHistory(&buf, checkpoints, "main"); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	for _, want := range []string{"main.1", "main.2", "benchmarks pass", "taken at boot"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	assertNoRetiredWords(t, got)
}

func TestHistoryEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := printHistory(&buf, nil, "main"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no checkpoints on main") {
		t.Errorf("unexpected empty-history output: %s", buf.String())
	}
}

func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second: "moments",
		20 * time.Minute: "20m",
		3 * time.Hour:    "3h 0m",
		50 * time.Hour:   "2d 2h",
	}
	for d, want := range cases {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%s) = %q, want %q", d, got, want)
		}
	}
}

// A checkpoint of a slate containing nested subvolumes looks complete and is
// not, and the consequence only appears at rollback — by which point the data
// is gone. Both surfaces have to say so before then.
func TestStatusWarnsAboutUncapturedData(t *testing.T) {
	var buf bytes.Buffer
	b := slate.Booted{Name: "main", Mode: slate.ModePersistent, Basis: "@main"}
	nested := []string{"srv/data", "var/lib/docker/btrfs/subvolumes/a1b2c3"}
	if err := printStatus(&buf, b, nil, nested); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	for _, want := range []string{"not captured", "empty after a rollback", "srv/data"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestListFlagsUncapturedData(t *testing.T) {
	var buf bytes.Buffer
	slates := []slate.Slate{{Name: "main", Subvolume: "@main", UUID: "u-main"}}
	if err := printSlates(&buf, slates, nil, slate.Booted{}, map[string]int{"main": 3}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "3 uncaptured") {
		t.Errorf("list does not flag uncaptured data:\n%s", buf.String())
	}
}

func TestListDoesNotFlagCleanSlates(t *testing.T) {
	var buf bytes.Buffer
	slates := []slate.Slate{{Name: "main", Subvolume: "@main", UUID: "u-main"}}
	if err := printSlates(&buf, slates, nil, slate.Booted{}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "uncaptured") {
		t.Errorf("a slate with nothing nested should not be flagged:\n%s", buf.String())
	}
}
