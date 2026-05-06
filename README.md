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

S1 (image build) is verified end-to-end: `testbox build` produces a btrfs raw
disk image with `@base` and `@hostid` subvolumes laid out, `@base` set as the
default subvolume, and Ubuntu 24.04 noble installed inside `@base`. Booting
the produced image with `qemu -kernel <vmlinuz> -initrd <initrd> -append
"root=PARTUUID=... rootflags=subvol=@base ..."` reaches the systemd login
prompt cleanly.

S2 (initramfs hook + bootloader integration), S3 (`testbox state` subcommands),
and S4 (base-update workflow) are not yet implemented. See
[DESIGN.md](DESIGN.md) for the full roadmap.

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

## Verifying boot

Until S2 lands, boot via direct kernel pass-through. Extract the kernel and
initrds from the ESP, then run qemu:

```sh
sudo losetup --find --show --partscan mkosi.output/testbox.raw   # records as /dev/loopN
sudo udevadm settle
sudo mount /dev/loopNp1 /mnt/esp                                  # ESP is partition 1
ROOT_PARTUUID=$(sudo blkid -s PARTUUID -o value /dev/loopNp2)
cat /mnt/esp/testbox/microcode.initrd \
    /mnt/esp/testbox/initrd \
    /mnt/esp/testbox/*/kernel-modules.initrd > /tmp/initrd
cp /mnt/esp/testbox/*/vmlinuz /tmp/vmlinuz
sudo umount /mnt/esp
sudo losetup -d /dev/loopN

qemu-system-x86_64 -enable-kvm -m 2G -smp 2 \
    -kernel /tmp/vmlinuz -initrd /tmp/initrd \
    -append "root=PARTUUID=$ROOT_PARTUUID rootflags=subvol=@base console=ttyS0,115200" \
    -drive file=mkosi.output/testbox.raw,format=raw,if=virtio \
    -nographic -serial mon:stdio -display none
```

You should reach `localhost login:` on the serial console.

See [DESIGN.md](DESIGN.md) for the architecture and roadmap.
