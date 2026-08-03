"""Checks that a slate containing a nested filesystem is reported, not
silently half-captured. Needs a first-boot image; see docs/development.md."""
import os, re, sys, pexpect
# Reuse the harness helpers from verify-boot.py without running its main().
src = open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "verify-boot.py")).read()
src = src.split("def main(")[0]
ns = {}
exec(src, ns)
run, wait_boot, QEMU, PROMPT = ns["run"], ns["wait_boot"], ns["QEMU"], ns["PROMPT"]

vm = pexpect.spawn("sudo", QEMU, timeout=240, encoding=None)
vm.logfile_read = open(os.environ.get("CLEANSLATE_CONSOLE_LOG", "/tmp/cleanslate-nested-console.log"), "wb")
ok = []
def check(label, cond, detail=""):
    ok.append((label, cond))
    print(("PASS  " if cond else "FAIL  ") + label + ("" if cond else f"  [{detail}]"))

wait_boot(vm, "boot 1")
out = run(vm, "cleanslate status")
check("clean slate is not flagged", "not captured" not in out, out)

run(vm, "mkdir -p /srv && btrfs subvolume create /srv/data && echo INSIDE > /srv/data/payload")
out = run(vm, "cleanslate status")
check("status warns once a nested filesystem exists", "not captured" in out and "srv/data" in out, out)
out = run(vm, "cleanslate list")
check("list marks the affected slate", "uncaptured" in out, out)
out = run(vm, "cleanslate checkpoint -m 'should refuse' 2>&1")
check("checkpoint refuses and names the path", "srv/data" in out and "allow-incomplete" in out, out)
out = run(vm, "cleanslate checkpoint -m 'accepted' --allow-incomplete 2>&1")
check("checkpoint proceeds with --allow-incomplete", "checkpoint" in out.lower() and "refus" not in out.lower(), out)
out = run(vm, "cat /etc/docker/daemon.json")
check("baseline selects the overlay2 docker driver", "overlay2" in out, out)

vm.sendline("poweroff"); vm.expect(pexpect.EOF, timeout=90)
failed = [l for l,c in ok if not c]
print(f"\n{len(ok)-len(failed)}/{len(ok)} checks passed")
sys.exit(1 if failed else 0)
