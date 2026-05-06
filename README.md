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

Early. `testbox build` (S1) wraps mkosi to produce a btrfs raw disk image
with `@base` and `@hostid` subvolumes pre-laid-out. The runtime pieces —
initramfs hook, GRUB integration, `testbox state` subcommands — are not yet
implemented (S2 / S3).

## Building an image

Requires [mkosi](https://github.com/systemd/mkosi) v26 or newer on the build
host. On Ubuntu, install via the [openSUSE Build Service apt repo](https://software.opensuse.org/download.html?project=system:systemd&package=mkosi)
or `pipx install git+https://github.com/systemd/mkosi.git`.

```sh
make build              # build the testbox CLI
sudo ./bin/testbox build
```

`sudo` is required because the post-output script loop-mounts the produced
image to lay out the `@base` / `@hostid` subvolumes. Output lands in
`mkosi.output/testbox.raw`.

See [DESIGN.md](DESIGN.md) for the architecture and roadmap.
