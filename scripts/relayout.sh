#!/usr/bin/env bash
# scripts/relayout.sh — runs AFTER mkosi finishes, OUTSIDE its sandbox.
#
# Rearranges the flat btrfs rootfs in a testbox raw disk image into:
#   @base    — the immutable baked OS (current rootfs contents go here)
#   @hostid  — empty carve-out, bind-mounted at /etc/ssh/host_keys at runtime
#              so the box keeps a stable SSH identity across state switches
#
# Sets @base as the default subvolume so the kernel mounts it on boot
# without needing rootflags=subvol= on the cmdline. The S2 work (initramfs
# hook + GRUB integration) will add the cmdline plumbing for ephemeral and
# named-state boots.
#
# Cannot run as a mkosi PostOutputScripts hook because mkosi's sandbox does
# not expose /dev/loop-control. The testbox build wrapper invokes this
# script after mkosi succeeds.
#
# Usage: relayout.sh <image.raw> [path-to-testbox-binary]
# Requires root.
#
# If a second argument is given, it is treated as a path to the testbox CLI
# and copied into @base/usr/local/bin/testbox so state-management commands
# are runnable on the booted image.

set -euo pipefail

PROG=$(basename "$0")
IMG="${1:-}"
TESTBOX_BIN="${2:-}"
if [[ -z "$IMG" ]]; then
    echo "usage: $PROG <image.raw> [path-to-testbox-binary]" >&2
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

# Move the existing rootfs into @base.
btrfs subvolume create "$MNT/@base"
shopt -s dotglob
for entry in "$MNT"/*; do
    case "${entry##*/}" in
        '@base'|'@hostid') continue ;;
    esac
    mv "$entry" "$MNT/@base/"
done

# Empty carve-out for the host-identity bind mount used at runtime.
btrfs subvolume create "$MNT/@hostid"

# Make @base the default subvolume so the kernel mounts it without
# rootflags=subvol= on the cmdline.
BASE_ID=$(btrfs subvolume show "$MNT/@base" | awk -F': *' '/Subvolume ID:/ {print $2; exit}')
btrfs subvolume set-default "$BASE_ID" "$MNT"

# Patch /etc/fstab inside @base so re-mounts know the subvolume.
FSTAB="$MNT/@base/etc/fstab"
if [[ -f "$FSTAB" ]]; then
    sed -i -E 's|(\s/\s+btrfs\s+)([^[:space:]]+)|\1subvol=@base|' "$FSTAB"
fi

if [[ -n "$TESTBOX_BIN" ]]; then
    if [[ ! -f "$TESTBOX_BIN" ]]; then
        echo "$PROG: warning: testbox binary not found at $TESTBOX_BIN; skipping install" >&2
    else
        install -m 755 "$TESTBOX_BIN" "$MNT/@base/usr/local/bin/testbox"
        echo "$PROG: installed testbox binary at /usr/local/bin/testbox in @base"
    fi
fi

echo "$PROG: created @base + @hostid; default subvolume = @base"
