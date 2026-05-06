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

**S1** (image build), **S2** (initramfs hook + ephemeral guarantee), and
**S3** (`testbox state` subcommands) are verified. `testbox build` produces
a btrfs raw disk image with `@base` (immutable rootfs), `@hostid` (stable
SSH identity carve-out), and the testbox CLI installed at
`/usr/local/bin/testbox`. The image's initrd contains the testbox
state-management hooks: booting with `rootflags=subvol=@runtime` deletes any
previous `@runtime` and snapshots `@base` into a fresh `@runtime` before the
kernel mounts root, so writes to `/` are wiped on next boot. Booting with
`rootflags=subvol=@<name>` is a passthrough so named persistent layers
survive reboots. SSH host keys auto-generate into `@hostid` on first boot
and persist across all state switches.

`testbox state {list,save,delete,current}` work both on the running OS and
against any btrfs filesystem with the testbox layout — point at one with
`--fs-root <path>`. `testbox state switch` is stubbed; it lands in S4
alongside bootloader installation.

**S4** (`testbox install` + real bootloader + state switching) is the next
slice. See [DESIGN.md](DESIGN.md) for the full roadmap.

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

Until S4 installs a real bootloader, boot via direct kernel pass-through.
Extract the Ubuntu kernel and initrd (which contains the testbox hooks) from
the ESP, then run qemu:

```sh
sudo losetup --find --show --partscan mkosi.output/testbox.raw   # records as /dev/loopN
sudo udevadm settle
ROOT_PARTUUID=$(sudo blkid -s PARTUUID -o value /dev/loopNp2)
sudo mount /dev/loopNp1 /mnt/esp                                  # ESP is partition 1
cp /mnt/esp/vmlinuz-* /tmp/vmlinuz
cp /mnt/esp/initrd.img-* /tmp/initrd
sudo umount /mnt/esp
sudo losetup -d /dev/loopN

# Boot fresh ephemeral state (rootflags=subvol=@runtime gets wiped on every boot).
qemu-system-x86_64 -enable-kvm -m 2G -smp 2 \
    -kernel /tmp/vmlinuz -initrd /tmp/initrd \
    -append "root=PARTUUID=$ROOT_PARTUUID rootflags=subvol=@runtime console=ttyS0,115200" \
    -drive file=mkosi.output/testbox.raw,format=raw,if=virtio \
    -nographic -serial mon:stdio -display none
```

You should reach `localhost login:` on the serial console. The boot log will
show `testbox: preparing fresh @runtime from @base` (local-top) and
`testbox: mounted @hostid at /etc/ssh/host_keys` (local-bottom).

Boot a named persistent state by changing `rootflags=subvol=@<name>` —
writes there persist across reboots. Boot rescue with
`rootflags=subvol=@base ro`.

See [DESIGN.md](DESIGN.md) for the architecture and roadmap.
