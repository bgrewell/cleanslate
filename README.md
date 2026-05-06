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

S1–S4 are implemented and verified end-to-end in qemu/OVMF. The image
produced by `testbox build` is bootable on real UEFI hardware: it contains
`@base` (immutable rootfs, marked btrfs read-only), `@hostid` (stable SSH
identity carve-out), the testbox CLI at `/usr/local/bin/testbox`, an
initramfs hook that snapshots `@base→@runtime` before root mount, and
systemd-boot installed at the firmware fallback path with BLS entries for
`fresh` and `base (rescue)`.

Runtime commands:

- `testbox state list` — tabular dump of testbox-managed subvolumes.
- `testbox state save <name>` — snapshot the running state into `@<name>`
  and emit a `testbox-<name>.conf` BLS entry.
- `testbox state delete <name>` — remove the snapshot and its BLS entry.
- `testbox state current` — print the active state name.
- `testbox state switch <name> [--reboot]` — set a systemd-boot one-shot
  via `bootctl set-oneshot`. The default boot target is unchanged, so the
  box returns to `fresh` on the boot after the switched one.
- `testbox install <target>` — write the raw image to a block device or
  file (dd-equivalent, with safety checks against root-device overwrites).

UEFI is the primary target. BIOS boot is not currently installed by the
build; it would re-introduce GRUB and is left for a follow-up if needed.

See [DESIGN.md](DESIGN.md) for architecture and the full design rationale.

### Known limitation: no bootloader installed yet

S1 ships `Bootloader=none` in the mkosi config. The image contains a kernel
and initrds on the ESP partition, but no UEFI or BIOS bootloader binary, so
firmware can't pick the image up directly without help. This is deliberate —
S2 needs to install a bootloader configured for state-switching anyway, so
S1 deferred the bootloader question rather than build something we'd
immediately replace. Verify S1 builds with `qemu -kernel` (see below).

## Requirements

- mkosi v26 or newer. Ubuntu 24.04 noble's archive doesn't ship a recent
  enough version; install upstream:
  ```sh
  pipx install git+https://github.com/systemd/mkosi.git@v26
  ```
  or use the [openSUSE Build Service apt repo](https://software.opensuse.org/download.html?project=system:systemd&package=mkosi).
- Host packages: `debootstrap`, `mtools`, `btrfs-progs`, `systemd-container`,
  `dosfstools`, `squashfs-tools`, `bubblewrap`, `debian-archive-keyring`,
  `ovmf`, `qemu-system-x86`, `qemu-utils`. mkosi will tell you about anything
  else it needs.
- Go 1.22+ to build the testbox CLI.

## Building

```sh
make build                         # build the testbox CLI
sudo ./bin/testbox build           # build the OS disk image
```

`sudo` is required because the post-mkosi relayout step
(`scripts/relayout.sh`) loop-mounts the produced raw image to create the
`@base` and `@hostid` subvolumes. Pass `--skip-relayout` to leave the rootfs
flat (useful when iterating on mkosi config). Output lands in
`mkosi.output/testbox.raw`.

## Verifying boot in qemu (UEFI)

Need OVMF on the host (`apt install ovmf qemu-system-x86`). The raw image
is a self-contained UEFI-bootable disk:

```sh
cp /usr/share/OVMF/OVMF_VARS_4M.fd /tmp/ovmf-vars.fd
qemu-system-x86_64 -enable-kvm -m 2G -smp 2 \
    -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \
    -drive if=pflash,format=raw,file=/tmp/ovmf-vars.fd \
    -drive file=mkosi.output/testbox.raw,format=raw,if=virtio \
    -netdev user,id=n,hostfwd=tcp:127.0.0.1:2222-:22 \
    -device virtio-net-pci,netdev=n \
    -nographic -serial mon:stdio -display none
```

systemd-boot shows the menu (`fresh` / `base`), counts down 3 s, and boots
the default (`fresh`). The box accepts SSH on the forwarded port; set up
`mkosi.local.conf` with a `RootPassword=` or drop a public key into
`mkosi.extra/root/.ssh/authorized_keys` first.

See [DESIGN.md](DESIGN.md) for the architecture and roadmap.
