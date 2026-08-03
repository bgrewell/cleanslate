# Development

## Requirements

- **mkosi v26 or newer.** Ubuntu noble's archive is too old; install upstream:
  ```sh
  pipx install git+https://github.com/systemd/mkosi.git@v26
  ```
  or use the [openSUSE Build Service apt repo](https://software.opensuse.org/download.html?project=system:systemd&package=mkosi).
- Host packages: `debootstrap`, `mtools`, `btrfs-progs`, `systemd-container`,
  `dosfstools`, `squashfs-tools`, `bubblewrap`, `debian-archive-keyring`,
  `ovmf`, `qemu-system-x86`, `qemu-utils`.
- Go 1.22 or newer.

## Repository layout

| Path | What it is |
|---|---|
| `cmd/cli/` | The CLI. `main.go` builds the command tree; `slates.go` holds the verbs; `format.go` is pure output, so it can be tested. |
| `internal/slate/` | Slates, checkpoints, staged replacements, boot facts, boot entries, btrfs plumbing. |
| `internal/build/`, `internal/install/` | Wrappers around mkosi and around writing an image to a disk. |
| `mkosi.conf`, `mkosi.conf.d/` | Image definition. |
| `mkosi.extra/` | Files copied into the image: initramfs hooks, systemd units, ssh and network configuration. |
| `mkosi.finalize` | Runs outside the build chroot; bakes the initramfs hooks into the initrd. |
| `scripts/relayout.sh` | Post-build: lays out the subvolumes, installs systemd-boot, writes the boot entries. |

## Building

```sh
make build                       # the CLI, into ./bin/cleanslate
make check                       # fmt, vet, test
sudo ./bin/cleanslate build      # the image, into mkosi.output/cleanslate.raw
```

`sudo` resets `PATH`, so if mkosi is installed under `~/.local/bin` the build
step needs it passed through:

```sh
sudo env "PATH=$HOME/.local/bin:$PATH" ./bin/cleanslate build
```

Pass `--skip-relayout` to leave the filesystem flat, which is useful when
iterating on mkosi configuration alone.

Note that `go build ./...` fails after an image build: mkosi leaves root-owned
`mkosi.cache/` and `mkosi.tools/` trees the Go tool cannot walk. The Makefile
targets name `./cmd/... ./internal/...` explicitly for this reason.

## Booting in qemu

The raw image is a self-contained UEFI disk. OVMF needs a **private, writable**
copy of the variables file — copying a fresh one per run silently discards any
`bootctl set-oneshot`, which looks exactly like a `switch` bug.

```sh
cp /usr/share/OVMF/OVMF_VARS_4M.fd /tmp/ovmf-vars.fd

qemu-system-x86_64 -enable-kvm -m 2G -smp 2 \
    -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \
    -drive if=pflash,format=raw,file=/tmp/ovmf-vars.fd \
    -drive file=mkosi.output/cleanslate.raw,format=raw,if=virtio \
    -netdev user,id=n,hostfwd=tcp:127.0.0.1:2222-:22 \
    -device virtio-net-pci,netdev=n \
    -nographic -serial mon:stdio -display none
```

The menu shows the slates plus `scratch` and `rescue`, counts down three
seconds, and boots the default. For SSH, set a `RootPassword=` in
`mkosi.local.conf` or drop a key into
`mkosi.extra/root/.ssh/authorized_keys` before building.

To inspect an image without booting it:

```sh
LOOP=$(sudo losetup --find --show --partscan mkosi.output/cleanslate.raw)
sudo udevadm settle
MNT=$(mktemp -d)
sudo mount -o subvol=/ "$(sudo lsblk -no PATH,FSTYPE "$LOOP" | awk '$2=="btrfs"{print $1; exit}')" "$MNT"
sudo btrfs subvolume list "$MNT"
sudo umount "$MNT"; sudo losetup -d "$LOOP"
```

## Things worth knowing before changing the boot path

**`/run` written from the initramfs survives.** `/init` mounts a tmpfs at
`/run` and later does `mount -n -o move /run ${rootmnt}/run`, so files the
local-top hook writes to `/run/cleanslate/` carry into the booted system with
no unit to order against. Writing to `$rootmnt/run/...` from local-bottom —
the obvious approach — is silently destroyed by that same move.

**The ESP is mounted at `/efi`, by an explicit unit.** `scripts/relayout.sh`
writes `efi.mount` addressed by PARTUUID and enables it. This is deliberate:
`systemd-gpt-auto-generator` is supposed to place the ESP at `/efi` and did
not, and the failure was invisible — the CLI wrote boot entries into an
unmounted directory and reported success. Do not remove the unit on the
assumption the generator covers it.

`/boot` in the rootfs is empty, because the kernel and initrd are copied to the
ESP root at build time, and there is no `/etc/fstab` in the image at all. Code
should call `slate.DetectESP()` rather than assume a path.

**Renaming an initramfs hook changes its ordering.** initramfs-tools orders
`local-top` scripts alphabetically. Nothing here currently depends on it — no
lvm2, mdadm, or cryptsetup in the package list, and the hook waits for udev
itself — but check the generated ORDER in the initrd after a rename.

**`editor no` is set in `loader.conf`,** so the kernel command line cannot be
edited from the boot menu. Testing a malformed command line means writing a
purpose-made entry file onto the ESP.

**Boot messages go to serial and video.** No `quiet` is set on the command
line; keep it that way, since the hook's warnings are the only signal when a
boot falls back.

## Testing

```sh
make check
```

Unit tests cover the parsers, the checkpoint name grammar, staged-replacement
round trips, the command tree, and the wording of every user-facing output
function. The wording tests assert the retired vocabulary in
[terminology.md](terminology.md) does not appear, so reintroducing "state" or
"snapshot" in output is a test failure.

What is not covered by unit tests: anything touching real btrfs, and the
initramfs hook. Those need the qemu run.

## Boot verification

Unit tests cannot reach the initramfs, the bootloader, or anything that only
shows up across a reboot. `test/verify-boot.py` drives a built image through
five boots with pexpect and asserts what the model claims:

```sh
sudo ./bin/cleanslate build
cp /usr/share/OVMF/OVMF_VARS_4M.fd /tmp/cleanslate-ovmf-vars.fd
python3 test/verify-boot.py
```

It checks first-boot slate creation, that work survives a reboot, automatic and
manual checkpoints, rollback actually restoring, fork and its boot entry, a
scratch run being discarded while leaving the slate untouched, the ESP being
mounted, ssh surviving the boot, and stable host identity.

It needs a **first-boot** image. Re-running against an already-booted image
starts from an existing `main` and the first few assertions will not hold; use
`--force` to rebuild, or delete every subvolume except `@baseline` and
`@hostid` plus the `.cleanslate` directory at the filesystem root.

The console log goes to `/tmp/cleanslate-console.log`, which is the only place
the initramfs hook's messages can be read — they do not reach the systemd
journal.
