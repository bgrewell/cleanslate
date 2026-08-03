# cleanslate — Design Document

## Overview

`cleanslate` is a tool for building and operating customizable Ubuntu 24.04 OS
images with a layered runtime model. The system boots from an immutable base,
exposes a transparent ephemeral writable layer, and lets the operator promote
ephemeral state into named persistent layers that can be saved, restored, and
switched between across reboots.

The primary use case is **shared research / validation hardware** — a headless
server that multiple researchers use sequentially, each needing a fully isolated
environment, sometimes for ten minutes, sometimes for weeks, with a hard
guarantee that one researcher's work never bleeds into another's session.

## Objectives

1. **Immutable base** — A baked Ubuntu 24.04 image that is the same on every
   fresh boot.
2. **Transparent ephemeral layer** — Userspace can write any file at runtime
   with no awareness of the layering. On reboot, every write is gone.
3. **Named persistent layers** — Operators can save the current ephemeral state
   under a name (e.g. `gnb-xyz`), boot into it later, and switch between layers
   from the OS without touching the bootloader.
4. **Tenant isolation** — Two consecutive users of the same physical box, both
   booting "fresh," see no trace of each other.
5. **Space efficiency by default** — Many named layers should not cost many
   times the base image; sharing is automatic.
6. **Headless operation** — All controls usable over SSH; no need for keyboard
   and monitor.

## Architecture

A single btrfs filesystem holds every root side-by-side as a subvolume. There
is no overlayfs, no FUSE, no userland write redirection — every write is
kernel-native btrfs CoW.

### Subvolume layout

| Subvolume    | Purpose                                                                                |
|--------------|----------------------------------------------------------------------------------------|
| `@baseline`      | Pristine baked OS. Mounted read-only during normal operation; writable only in base-update mode. |
| `@runtime`   | Ephemeral working root. Recreated as a fresh snapshot of `@baseline` on every fresh boot. |
| `@hostid`    | Tiny non-rolled-back carve-out, bind-mounted at `/etc/ssh/host_keys` so the box keeps a stable SSH identity across state switches. |
| `@<state>`   | Named persistent layer (snapshot of `@baseline`, or — advanced — of another `@<state>`). Survives reboots. Lives until explicitly deleted. |

Everything else — `/etc`, `/var`, `/var/log`, `/home`, `/tmp` — lives **inside**
the active root subvolume. Nothing persists across an ephemeral reboot unless
the operator explicitly saved it. This is a deliberate consequence of the
multi-tenant isolation requirement; the cost is that journals do not survive
ephemeral reboots, and cached package downloads are not retained.

### Boot flow

GRUB places `rootflags=subvol=<name>` on the kernel cmdline. An initramfs hook
shipped inside `@baseline` reads it and acts:

| GRUB entry          | initramfs action                                                | mounted root |
|---------------------|------------------------------------------------------------------|--------------|
| **fresh** (default) | delete `@runtime` if present; `btrfs subvolume snapshot @baseline @runtime` | `@runtime`   |
| **gnb-xyz** etc.    | nothing                                                          | `@<state>`   |
| **base (rescue)**   | mount read-only                                                  | `@baseline`      |
| **update base**     | mount `@baseline` read-write under a guarded path                    | base-update  |

The cleanup of the previous `@runtime` happens at *next* boot, not at shutdown,
so a crash, hang, or power loss still produces a clean fresh boot. The
ephemeral guarantee is structural: kernel cmdline plus initramfs hook enforce
it; nothing inside the running system can defeat it. **There is no recovery
window** — if a researcher reboots without saving, the work is gone.

`@hostid` is bind-mounted over `/etc/ssh/host_keys` regardless of which root
was selected, so SSH host identity is stable across switches and clients do
not see "REMOTE HOST IDENTIFICATION HAS CHANGED" warnings between layers.

### Switching states

Switching is always a reboot — the active subvolume is the one mounted as `/`,
and you cannot replace `/` on a running system. The `cleanslate state switch`
command writes a one-shot directive (`grub-reboot`) so the next boot picks the
target state, then optionally invokes `systemctl reboot`. The default GRUB
entry is unchanged, so an operator who switches without saving recovers to the
default on the boot after that. The GRUB menu lists every state and is the
fallback if SSH is unavailable.

## Space efficiency

Sharing is automatic. A snapshot shares every extent with its parent until
something is overwritten, so 100 named layers each diverging slightly from
`@baseline` cost roughly `@baseline` plus the sum of *only the changed extents*, not
100 × `@baseline`. Snapshot chains (advanced) inherit the same sharing behavior.
The image is mounted with `compress=zstd`, which typically buys another 20–50%
on OS data for negligible CPU cost. Block-level deduplication of *unrelated*
writes (`bees`, `duperemove`) is available but not enabled by default; the CoW
sharing is sufficient for the expected workload.

## CLI surface

```
cleanslate build [--config FILE] --out IMG       # mkosi wrapper, produces disk image
cleanslate install <device|image>                 # lay down @baseline + GRUB on a target

cleanslate state list                             # NAME / SIZE / BASIS / LAST-BOOTED
cleanslate state save <name> [--from current|@baseline]
cleanslate state delete <name>
cleanslate state switch <name> [--reboot]         # one-shot grub-reboot, default unchanged
cleanslate state current                          # active state name (read at boot)
cleanslate state diff <a> <b>                     # file-level diff (deferred)
cleanslate state export <name> > stream.btrfs     # btrfs send (deferred)
cleanslate state import < stream.btrfs            # btrfs receive (deferred)

cleanslate base update [--script FILE]            # one-shot boot into base-update mode
```

At runtime, `/run/cleanslate/current-state` and `/run/cleanslate/is-ephemeral` are
populated by the initramfs hook so shells, prompts, and monitoring scripts can
read them without parsing `/proc/cmdline`.

## Design decisions

The decisions below were made deliberately and should not be reopened without
a concrete reason.

1. **`@baseline` is read-only except in a dedicated update mode.** Normal `apt`
   operations during a session run in the active root (ephemeral or named) and
   follow that root's lifecycle. Mutating `@baseline` itself is a separate,
   intentional workflow (`cleanslate base update`) that boots into a guarded
   mode, applies the update, and reboots.
2. **All system trees live inside the active root subvolume.** No split-out
   `/etc` or `/var`. The simplicity is worth the loss of cross-reboot journals.
3. **Default is "everything ephemeral unless saved."** Driven by the
   multi-tenant research-server requirement.
4. **CLI is the primary switch surface; bootloader menu is the fallback.**
   These are headless servers.
5. **State chaining is supported but documented as an advanced feature.**
   Users see a flat model by default.
6. **No recovery window for ephemeral state.** Ephemeral means ephemeral;
   keeping the previous `@runtime` would leak data between tenants.
7. **SSH host identity is shared across states via `@hostid`.** Each state
   having its own host keys creates a hostile UX on shared hardware.
8. **Flat namespace, root-only mutation.** The admin owns state lifecycle;
   per-user ownership is out of scope for v1.

## Roadmap

| Slice | Deliverable                                                                 |
|-------|-----------------------------------------------------------------------------|
| **S1** | mkosi-driven base image build; `cleanslate build` produces a bootable raw image with `@baseline` and `@hostid`. |
| **S2** | initramfs hook + GRUB integration; ephemeral guarantee verified end-to-end in qemu. |
| **S3** | `cleanslate state {list,save,delete,current,switch}` subcommands.              |
| **S4** | `cleanslate base update` workflow.                                             |
| **S5** | `state export` / `state import` and chained-state UX (deferred).            |

## Out of scope

- Per-user state ownership and ACLs.
- Cross-state block-level deduplication beyond what btrfs CoW gives for free.
- Persistent-by-default `/var` or `/home` carve-outs.
- ZFS, OSTree, or overlayfs backends.
- Non-Ubuntu base distributions.
