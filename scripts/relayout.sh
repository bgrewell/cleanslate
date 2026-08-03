#!/usr/bin/env bash
# scripts/relayout.sh — runs AFTER mkosi finishes, OUTSIDE its sandbox.
#
# Rearranges the flat btrfs rootfs in a cleanslate raw disk image into:
#   @baseline    — the immutable baked OS (current rootfs contents go here)
#   @hostid  — empty carve-out, bind-mounted at /etc/ssh/host_keys at runtime
#              so the box keeps a stable SSH identity across state switches
#
# Sets @baseline as the default subvolume so the kernel mounts it on boot
# without needing rootflags=subvol= on the cmdline. The S2 work (initramfs
# hook + GRUB integration) will add the cmdline plumbing for ephemeral and
# named-state boots.
#
# Cannot run as a mkosi PostOutputScripts hook because mkosi's sandbox does
# not expose /dev/loop-control. The cleanslate build wrapper invokes this
# script after mkosi succeeds.
#
# Usage: relayout.sh <image.raw> [path-to-cleanslate-binary]
# Requires root.
#
# If a second argument is given, it is treated as a path to the cleanslate CLI
# and copied into @baseline/usr/local/bin/cleanslate so state-management commands
# are runnable on the booted image.

set -euo pipefail

PROG=$(basename "$0")
IMG="${1:-}"
CLEANSLATE_BIN="${2:-}"
if [[ -z "$IMG" ]]; then
    echo "usage: $PROG <image.raw> [path-to-cleanslate-binary]" >&2
    exit 64
fi
if [[ "$EUID" -ne 0 ]]; then
    echo "$PROG: must run as root (loop-mounts the raw image)" >&2
    exit 1
fi
if [[ ! -f "$IMG" ]]; then
    echo "$PROG: image not found at $IMG" >&2
    exit 1
fi

LOOP=$(losetup --find --show --partscan "$IMG")
MNT=$(mktemp -d)
cleanup() {
    if mountpoint -q "$MNT" 2>/dev/null; then
        umount "$MNT" || true
    fi
    rmdir "$MNT" 2>/dev/null || true
    losetup -d "$LOOP" || true
}
trap cleanup EXIT

# Wait for partition device nodes to settle after partscan.
udevadm settle 2>/dev/null || sleep 1

# Find the btrfs partition. Our layout has only one btrfs filesystem
# (mkosi adds an ESP and BIOS boot partition for the GRUB hybrid setup),
# so matching on FSTYPE is unambiguous and arch-agnostic.
ROOT_DEV=$(lsblk -no PATH,FSTYPE "$LOOP" | awk '$2=="btrfs"{print $1; exit}')
if [[ -z "$ROOT_DEV" ]]; then
    echo "$PROG: no btrfs partition found on $LOOP" >&2
    exit 1
fi

mount -o subvol=/ "$ROOT_DEV" "$MNT"

# Move the existing rootfs into @baseline.
btrfs subvolume create "$MNT/@baseline"
shopt -s dotglob
for entry in "$MNT"/*; do
    case "${entry##*/}" in
        '@baseline'|'@hostid') continue ;;
    esac
    mv "$entry" "$MNT/@baseline/"
done

# Empty carve-out for the host-identity bind mount used at runtime.
btrfs subvolume create "$MNT/@hostid"

# Make @baseline the default subvolume so the kernel mounts it without
# rootflags=subvol= on the cmdline.
BASE_ID=$(btrfs subvolume show "$MNT/@baseline" | awk -F': *' '/Subvolume ID:/ {print $2; exit}')
btrfs subvolume set-default "$BASE_ID" "$MNT"

# Patch /etc/fstab inside @baseline so re-mounts know the subvolume.
FSTAB="$MNT/@baseline/etc/fstab"
if [[ -f "$FSTAB" ]]; then
    sed -i -E 's|(\s/\s+btrfs\s+)([^[:space:]]+)|\1subvol=@baseline|' "$FSTAB"
fi

if [[ -n "$CLEANSLATE_BIN" ]]; then
    if [[ ! -f "$CLEANSLATE_BIN" ]]; then
        echo "$PROG: warning: cleanslate binary not found at $CLEANSLATE_BIN; skipping install" >&2
    else
        install -m 755 "$CLEANSLATE_BIN" "$MNT/@baseline/usr/local/bin/cleanslate"
        echo "$PROG: installed cleanslate binary at /usr/local/bin/cleanslate in @baseline"
    fi
fi

# ----------------------------------------------------------------------------
# Bootloader installation (systemd-boot, UEFI). mkosi v26's Bootloader=grub
# and Bootloader=systemd-boot paths are unreliable on Ubuntu noble (no
# grub.cfg emitted; no systemd-bootx64.efi installed at the firmware fallback
# path), so we set Bootloader=none and install systemd-boot ourselves here.
# ----------------------------------------------------------------------------

ESP_DEV=$(lsblk -no PATH,FSTYPE "$LOOP" | awk '$2=="vfat"{print $1; exit}')
if [[ -z "$ESP_DEV" ]]; then
    echo "$PROG: no ESP (vfat partition) found on $LOOP — skipping bootloader install" >&2
else
    ESP_MNT=$(mktemp -d)
    mount "$ESP_DEV" "$ESP_MNT"

    # Cleanup ESP mount on exit too.
    bootloader_cleanup() {
        if mountpoint -q "$ESP_MNT" 2>/dev/null; then
            umount "$ESP_MNT" || true
        fi
        rmdir "$ESP_MNT" 2>/dev/null || true
    }
    trap 'bootloader_cleanup; cleanup' EXIT

    # Copy systemd-boot.efi from the rootfs to the firmware fallback path
    # and to the canonical /EFI/systemd/ path. The fallback path is what
    # firmware launches when no NVRAM entry exists (the qemu/OVMF case).
    BOOT_EFI="$MNT/@baseline/usr/lib/systemd/boot/efi/systemd-bootx64.efi"
    if [[ ! -f "$BOOT_EFI" ]]; then
        echo "$PROG: systemd-bootx64.efi not found in @baseline — install the systemd-boot package" >&2
        exit 1
    fi
    install -d "$ESP_MNT/EFI/BOOT" "$ESP_MNT/EFI/systemd"
    install -m 644 "$BOOT_EFI" "$ESP_MNT/EFI/systemd/systemd-bootx64.efi"
    install -m 644 "$BOOT_EFI" "$ESP_MNT/EFI/BOOT/BOOTX64.EFI"

    # loader.conf — systemd-boot's top-level config.
    cat > "$ESP_MNT/loader/loader.conf" <<EOF
default cleanslate-fresh.conf
timeout 3
console-mode max
editor no
EOF

    # Discover the kernel version from the kernel filename mkosi placed on
    # the ESP via CopyFiles=/boot:/.
    KVER=$(basename "$(ls "$ESP_MNT"/vmlinuz-* | head -1)" | sed 's/^vmlinuz-//')
    if [[ -z "$KVER" ]]; then
        echo "$PROG: no /vmlinuz-* on ESP; cannot generate BLS entries" >&2
        exit 1
    fi

    # Discover the root partition's PARTUUID so the kernel can find / by GPT.
    ROOT_PARTUUID=$(blkid -s PARTUUID -o value "$ROOT_DEV")
    if [[ -z "$ROOT_PARTUUID" ]]; then
        echo "$PROG: could not read PARTUUID for $ROOT_DEV" >&2
        exit 1
    fi

    # Common cmdline. console=ttyS0 makes serial-only boxes (and our qemu
    # tests) usable; console=tty0 keeps a video console too. We do NOT add
    # `ro` — without an fstab declaring "/", systemd never remounts the root
    # writable, which breaks first-boot SSH host-key generation and a lot
    # else. The runtime is supposed to be writable; durability is provided
    # by the snapshot-on-boot model, not by mounting read-only.
    BASE_OPTS="root=PARTUUID=$ROOT_PARTUUID console=ttyS0,115200 console=tty0"

    write_entry() {
        local name="$1" subvol="$2" title="$3"
        cat > "$ESP_MNT/loader/entries/cleanslate-${name}.conf" <<EOF
title    $title
version  $KVER
linux    /vmlinuz-$KVER
initrd   /initrd.img-$KVER
options  $BASE_OPTS rootflags=subvol=$subvol
EOF
    }

    write_entry "fresh" "@runtime"  "cleanslate: fresh (ephemeral)"
    write_entry "base"  "@baseline"     "cleanslate: base (rescue, read-only)"

    # Remove the default mkosi-generated BLS entry — its `options` line is
    # missing root= and we have our own entries now.
    rm -f "$ESP_MNT"/loader/entries/cleanslate-*-generic.conf

    echo "$PROG: installed systemd-boot at /EFI/{BOOT,systemd}/ on ESP"
    echo "$PROG: wrote BLS entries: cleanslate-fresh, cleanslate-base (kver=$KVER)"
fi

# Mark @baseline as read-only at the btrfs level — done LAST, after all writes
# to @baseline (cleanslate binary install, fstab edit). Snapshot-from-readonly is
# allowed (so the local-top hook still creates @runtime from @baseline), but
# direct boots into @baseline — the rescue path — are inherently immutable.
btrfs property set "$MNT/@baseline" ro true

echo "$PROG: created @baseline + @hostid; default subvolume = @baseline; @baseline sealed read-only"
