# testbox

A tool for building customizable Ubuntu 24.04 OS images with a layered runtime
model: an immutable base, a transparent ephemeral writable layer, and optional
named persistent layers you can save, restore, and switch between.

## Concept

- **Base** — A baked, customizable Ubuntu 24.04 image that boots normally.
- **Ephemeral layer** — All writes at runtime go to a transparent overlay backed
  by a btrfs snapshot. On reboot the snapshot is discarded; the system returns
  to the fresh base state.
- **Named layers** — Promote the current ephemeral state to a named persistent
  layer (e.g. `gnb-xyz`). Switch between named layers and the fresh base across
  reboots from the bootloader.

Built on btrfs subvolumes and snapshots; images produced via mkosi.

## Status

Early scaffolding. Image-build and layer-management commands are not yet
implemented.
