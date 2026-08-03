#!/usr/bin/env python3
"""Measure what a checkpoint costs a nodatacow workload on btrfs.

The question from issue #3: `chattr +C` avoids copy-on-write for random
rewrites, but a nodatacow file is still copied on the first write to each block
after a snapshot. cleanslate now snapshots every slate on every boot, so the
question is whether that penalty is a one-off paid down after each boot, or a
standing tax.

Four cases, same workload each time:

  cow            default btrfs, no nodatacow, no snapshot  — the baseline case
  nodatacow      chattr +C, no snapshot                    — the mitigation
  post-snapshot  chattr +C, snapshot taken, first pass      — the penalty
  settled        chattr +C, same file, second pass          — does it decay?

`settled` is the one that decides the design question. If it returns to
nodatacow numbers, the cost is bounded per boot and the checkpoint schedule is
defensible. If it stays slow, per-boot checkpoints are the wrong default for
stateful workloads.
"""
import os, random, subprocess, sys, time

FILE_MB = 512
BLOCK = 4096
WRITES = 20000
FSYNC_EVERY = 100   # emulates transactional commits rather than a bulk rewrite
SEED = 20260803


def sh(*args, check=True):
    return subprocess.run(args, check=check, capture_output=True, text=True)


def extents(path):
    out = sh("filefrag", path, check=False).stdout
    try:
        return int(out.split(":")[1].strip().split()[0])
    except Exception:
        return -1


def allocate(path):
    with open(path, "wb") as f:
        chunk = os.urandom(1024 * 1024)
        for _ in range(FILE_MB):
            f.write(chunk)
        f.flush()
        os.fsync(f.fileno())


def random_writes(path):
    """Random 4K rewrites with periodic fsync, as a database would."""
    rnd = random.Random(SEED)
    size = os.path.getsize(path)
    blocks = size // BLOCK
    payload = os.urandom(BLOCK)

    fd = os.open(path, os.O_WRONLY)
    start = time.monotonic()
    try:
        for i in range(WRITES):
            os.lseek(fd, rnd.randrange(blocks) * BLOCK, os.SEEK_SET)
            os.write(fd, payload)
            if (i + 1) % FSYNC_EVERY == 0:
                os.fsync(fd)
        os.fsync(fd)
    finally:
        os.close(fd)
    return time.monotonic() - start


def prepare(root, name, nodatacow):
    d = os.path.join(root, name)
    os.makedirs(d, exist_ok=True)
    if nodatacow:
        # Must be set on the directory before the file exists: the flag only
        # affects files created afterwards, which is the classic way to get
        # this wrong and conclude nodatacow "does nothing".
        sh("chattr", "+C", d)
    p = os.path.join(d, "data")
    allocate(p)
    return p


def report(label, secs, path):
    iops = WRITES / secs
    print(f"{label:<16}{secs:8.2f}s{iops:12.0f} writes/s{extents(path):10d} extents")
    return iops


def main(root):
    print(f"{FILE_MB} MiB file, {WRITES} random {BLOCK}B writes, fsync every {FSYNC_EVERY}\n")
    print(f"{'case':<16}{'elapsed':>9}{'throughput':>22}{'fragmentation':>18}")
    print("-" * 66)
    results = {}

    p = prepare(root, "cow", nodatacow=False)
    results["cow"] = report("cow", random_writes(p), p)

    p = prepare(root, "nodatacow", nodatacow=True)
    results["nodatacow"] = report("nodatacow", random_writes(p), p)

    # The cleanslate case: a checkpoint exists, so every first write to a block
    # has to be copied even though the file is nodatacow.
    p = prepare(root, "snap", nodatacow=True)
    sh("btrfs", "subvolume", "create", os.path.join(root, "@slate"), check=False)
    sh("btrfs", "subvolume", "snapshot", "-r", root, os.path.join(root, "snapshot"), check=False)
    results["post-snapshot"] = report("post-snapshot", random_writes(p), p)

    # Same file again, no new snapshot: the blocks touched above are now
    # unshared, so this is what the workload settles to until the next boot.
    results["settled"] = report("settled", random_writes(p), p)

    print("-" * 66)
    base = results["nodatacow"]
    print(f"\nrelative to nodatacow:")
    for k, v in results.items():
        print(f"  {k:<16}{v / base * 100:6.0f}%")
    return results


if __name__ == "__main__":
    main(sys.argv[1])
