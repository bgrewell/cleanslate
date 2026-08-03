"""Confirms auto_checkpoint=off is honoured by the hook and reported by status."""
import os, re, sys, pexpect
src = open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "verify-boot.py")).read().split("def main(")[0]
ns = {}; exec(src, ns)
run, wait_boot, QEMU = ns["run"], ns["wait_boot"], ns["QEMU"]

vm = pexpect.spawn("sudo", QEMU, timeout=240, encoding=None)
vm.logfile_read = open(os.environ.get("CLEANSLATE_CONSOLE_LOG", "/tmp/cleanslate-optout-console.log"), "wb")
ok = []
def check(l, c, d=""):
    ok.append((l,c)); print(("PASS  " if c else "FAIL  ")+l+("" if c else f"  [{d}]"))

wait_boot(vm, "boot 1 (checkpoints on)")
before = run(vm, "btrfs subvolume list / | grep -c ckpt")
out = run(vm, "cleanslate status")
check("default: a checkpoint was taken at boot", "taken at boot" in out, out)

run(vm, "mkdir -p /tmp/fsr && mount -o subvol=/ /dev/vda2 /tmp/fsr && mkdir -p /tmp/fsr/.cleanslate && echo auto_checkpoint=off > /tmp/fsr/.cleanslate/config && umount /tmp/fsr")
vm.sendline("reboot")
wait_boot(vm, "boot 2 (checkpoints off)")

after = run(vm, "btrfs subvolume list / | grep -c ckpt")
check("no new checkpoint was taken", before.strip() == after.strip(), f"before={before} after={after}")
out = run(vm, "cleanslate status")
check("status says checkpoints are off", "automatic checkpoints are off" in out, out)
check("status says this boot left no way back", "left no rollback point" in out, out)
out = run(vm, "cleanslate checkpoint -m 'manual still works' 2>&1")
check("manual checkpoints still work", "checkpoint" in out.lower() and "error" not in out.lower(), out)

vm.sendline("poweroff"); vm.expect(pexpect.EOF, timeout=90)
failed=[l for l,c in ok if not c]
print(f"\n{len(ok)-len(failed)}/{len(ok)} checks passed")
sys.exit(1 if failed else 0)
