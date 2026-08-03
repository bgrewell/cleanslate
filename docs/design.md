# Design

`cleanslate` builds Ubuntu 24.04 images and gives the machines running them a
layered runtime on btrfs: an immutable baseline, named persistent slates on top
of it, and read-only checkpoints for rollback.

The intended use is **shared research and validation hardware** — headless
servers that several people use in turn, each needing their own environment,
sometimes for ten minutes and sometimes for weeks.

See [terminology.md](terminology.md) for the vocabulary. This document uses
mechanism terms freely; user-facing text does not.

## Subvolume layout

One btrfs filesystem holds every root side by side. There is no overlayfs, no
FUSE, and no userland write redirection — every write is kernel-native btrfs
copy-on-write.

| Subvolume | Purpose |
|---|---|
| `@baseline` | The baked OS. Sealed read-only at build time. Never pruned. |
| `@<name>` | A slate: a persistent, writable root. Mounted directly. |
| `@<name>.ckpt.<seq>.<class>` | A checkpoint. Read-only from birth. |
| `@runtime` | The scratch root: deleted and recreated from a basis on every scratch boot. |
| `@hostid` | Host-identity carve-out, bind-mounted at `/etc/ssh/host_keys` so SSH identity survives switching slates. |

Everything else — `/etc`, `/var`, `/home` — lives inside the active root.

## Boot flow

`scripts/relayout.sh` writes three entries at build time; the CLI adds one per
slate. The initramfs hook at
`mkosi.extra/etc/initramfs-tools/scripts/local-top/cleanslate-state` reads the
kernel command line and prepares the root before it is mounted.

| Entry | Command line | Hook action | Root |
|---|---|---|---|
| a slate | `rootflags=subvol=@<name>` | apply any staged replacement, checkpoint, prune | `@<name>` rw |
| `scratch` | `rootflags=subvol=@runtime cleanslate.basis=@<x> cleanslate.mode=scratch` | delete `@runtime`, snapshot the basis into it | `@runtime` rw |
| `rescue` | `rootflags=subvol=@baseline cleanslate.mode=rescue` | none | `@baseline` ro |

`cleanslate.basis=` and `cleanslate.mode=` are dotted deliberately. The kernel
silently discards unrecognized parameters containing a dot, treating them as
module parameters for a module that may never load; a non-dotted unknown
`key=value` is injected into PID 1's environment and produces a console
warning. This also matches the `systemd.*` convention. initramfs-tools' own
command-line parser has an explicit case per key with no catch-all, so both are
inert to it.

### A missing target is created, not fatal

If the subvolume named by the boot entry does not exist, the hook creates it
from the baseline. This unifies two cases that would otherwise need separate
handling — a machine's first boot, and a boot entry that outlived the slate it
named — and it means a stale entry can never leave a machine unbootable.

### Failure policy

Fatal (panic into the initramfs shell): the root device never appears, the
filesystem root cannot be mounted, `@baseline` is missing, a target slate
cannot be created, or a staged replacement fails part-way. Dropping to a shell
is recoverable; silently booting the wrong root is not.

Non-fatal (warn and continue): a malformed or missing scratch basis falls back
to the baseline, and a checkpoint that cannot be taken is skipped. Losing a
rollback point is worse than losing the machine only in theory.

## Checkpoints

Taken by the initramfs hook before the root is mounted, so nothing is writing
and no quiescing is needed. Created with `snapshot -r` rather than snapshotted
and then sealed, so there is no window in which a checkpoint is writable.

The retention class is encoded in the subvolume name — `.auto` or `.keep` —
rather than in metadata. Pruning runs in the initramfs, which is a poor place
to parse files, and pruning must not depend on anything that can go missing or
be malformed. Messages and timestamps live in a sidecar under
`<fsroot>/.cleanslate/checkpoints/`; losing it degrades presentation and
nothing else.

Retention is `retain_auto` in `<fsroot>/.cleanslate/config`, default 10. It
lives at the filesystem root rather than inside a slate so it cannot be
captured into a checkpoint of itself. Kept checkpoints are never pruned.

## Staged replacements

A slate is the mounted root while it runs, so `rollback` and `reset` cannot act
in place. They write `<fsroot>/.cleanslate/pending` and the hook applies it at
the next boot, where nothing holds the subvolume open. The file is one
`key=value` per line with no quoting, because the component that applies it is
a shell script in the initramfs.

The state being replaced is checkpointed as `.keep` first, so a rollback is
itself reversible. The staged file is removed *before* the replacement begins:
a crash mid-way then leaves the slate missing, which the create-from-baseline
path handles, whereas leaving the file in place would retry a failing
replacement on every boot.

## Design decisions

1. **Slates are persistent; nothing is erased automatically.** The original
   design wiped the root on every boot and made persistence explicit, modelled
   on a network switch OS. That model cannot complete any procedure requiring a
   reboot part-way through — driver and firmware work, kernel modules,
   multi-stage installers — which is ordinary work on validation hardware.
   Safety comes from checkpoints instead.
2. **Tenant isolation is available, not automatic.** This is the cost of (1),
   and it is a real reduction from the original objective: two people using a
   machine in turn will see each other's work unless someone acts. What makes
   it acceptable is that the baseline is never pruned, so `reset` always works;
   the `scratch` entry covers working without leaving traces. The README states
   this plainly rather than letting people discover it.
3. **`@baseline` is immutable and is not a save target.** It is the operator's
   artifact. Changing it is a separate deliberate act, scoped to updates and
   security patching rather than changes of purpose. Not yet implemented.
4. **The first boot creates `@main` implicitly.** A new install behaves like an
   ordinary Ubuntu box; slates only become visible when someone wants a second
   line of work.
5. **All system trees live inside the active root.** No split-out `/etc` or
   `/var`. The simplicity is worth losing cross-slate journals.
6. **SSH host identity is shared across slates via `@hostid`.** Per-slate host
   keys produce a hostile UX on shared hardware.
7. **Per-slate disk usage is out of scope, not deferred.** The only honest
   number is exclusive bytes, and there are two ways to get it. btrfs qgroups
   add per-transaction accounting for the life of the filesystem and make
   `subvolume delete` markedly slower — on a workload whose hot path is
   snapshot-and-delete, that taxes everything to populate one column.
   `btrfs filesystem du` walks every extent of every file, which is seconds to
   minutes per row. Anything cheaper reports roughly the full image size per
   slate where the true incremental cost is a few tens of megabytes, which
   actively contradicts the sharing the design depends on. A misleading column
   is worse than no column.
8. **Checkpoint retention is by count, not age.** A slate untouched for two
   months would otherwise lose its entire history.

## Space efficiency

A snapshot shares every extent with its parent until something is overwritten,
so a hundred slates diverging slightly from the baseline cost roughly the
baseline plus the changed extents, not a hundred times the baseline. The same
applies to checkpoints: the cost of keeping one is the data only it still
references. The filesystem is mounted with `compress=zstd`.

## Known gaps

- Snapshots do not capture nested subvolumes, so a checkpoint of a slate using
  Docker's btrfs storage driver will not include the container data. Deletion
  handles nesting (`delete -R`, with a depth-first fallback); capture does not.
- `/etc/ssh/host_keys` is a mount point over `@hostid`, so checkpoints contain
  an empty directory there. This is intended.
- Baseline updates, slate export/import, and a file-level diff between slates
  are not implemented.
