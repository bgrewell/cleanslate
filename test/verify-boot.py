#!/usr/bin/env python3
"""Drive a cleanslate image through the scenarios that unit tests cannot reach.

The headline case is that work survives a reboot: that is the whole reason the
runtime model changed, and nothing below the initramfs can prove it.
"""
import os, re, sys, pexpect

IMAGE = "mkosi.output/cleanslate.raw"
VARS = os.environ.get("CLEANSLATE_OVMF_VARS", "/tmp/cleanslate-ovmf-vars.fd")
PROMPT = r"(?:root@[\w.-]+:[^\n]*[#$]|~#)\s*$"

QEMU = [
    "qemu-system-x86_64", "-enable-kvm", "-m", "2G", "-smp", "2",
    "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd",
    "-drive", f"if=pflash,format=raw,file={VARS}",
    "-drive", f"file={IMAGE},format=raw,if=virtio",
    "-netdev", "user,id=n", "-device", "virtio-net-pci,netdev=n",
    "-nographic", "-serial", "mon:stdio", "-display", "none",
]

results = []


def check(label, ok, detail=""):
    results.append((label, ok, detail))
    print(f"{'PASS' if ok else 'FAIL'}  {label}" + (f"  [{detail}]" if detail and not ok else ""))


ANSI = re.compile(r"\x1b\[[0-9;?]*[a-zA-Z]")


def run(vm, cmd, timeout=60):
    """Bracket output with sentinels the echoed command line cannot match.

    The shell echoes what it was sent, so a plain marker matches that echo
    before the command has even run. Splitting the marker across a string
    concatenation means the typed line contains `"__CS""B__"` while only the
    output contains `__CSB__`.
    """
    vm.sendline(f'echo "__CS""B__"; {cmd}; echo "__CS""E__"')
    vm.expect(r"__CSE__", timeout=timeout)
    raw = ANSI.sub("", vm.before.decode("utf-8", "replace")).replace("\r", "")
    raw = raw.split("__CSB__")[-1] if "__CSB__" in raw else raw
    lines = [l for l in raw.splitlines() if "__CS" not in l]
    vm.expect(PROMPT, timeout=timeout)
    return "\n".join(lines).strip()


def wait_boot(vm, what, timeout=240):
    vm.expect(PROMPT, timeout=timeout)
    # Settle: consume the MOTD and anything else queued behind the prompt so
    # the first real command starts from a known point.
    vm.sendline("")
    vm.expect(PROMPT, timeout=60)
    print(f"--- reached a shell: {what}")


def main():
    vm = pexpect.spawn("sudo", QEMU[0:1] + QEMU[1:], timeout=240, encoding=None)
    vm.logfile_read = open(os.environ.get("CLEANSLATE_CONSOLE_LOG", "/tmp/cleanslate-console.log"), "wb")

    # ---- boot 1: first boot creates main from the baseline -----------------
    wait_boot(vm, "boot 1")
    run(vm, "export PS1='root@box:~# '")

    st = run(vm, "cleanslate status")
    check("first boot lands on a persistent slate", "persistent" in st, st)
    check("first boot creates 'main'", "main" in st, st)

    subs = run(vm, "btrfs subvolume list / | awk '{print $NF}'")
    check("baseline and main exist", "@baseline" in subs and "@main" in subs, subs)
    check("boot took an automatic checkpoint", ".ckpt.0001.auto" in subs, subs)

    esp = run(vm, "findmnt -no TARGET /efi 2>/dev/null || echo none")
    check("ESP is mounted at /efi", "/efi" in esp, esp)

    ssh = run(vm, "systemctl is-active ssh.socket 2>/dev/null || systemctl is-active ssh")
    check("ssh survived the boot (no ordering cycle)", "active" in ssh, ssh)

    run(vm, "echo PROOF-OF-WORK > /root/marker")
    cp = run(vm, "cleanslate checkpoint -m 'known good'")
    check("manual checkpoint is created", "checkpoint" in cp.lower(), cp)

    hist = run(vm, "cleanslate history")
    check("history shows both checkpoints", "main.1" in hist and "main.2" in hist, hist)
    check("history marks the kept one", "known good" in hist, hist)

    # ---- boot 2: the headline behaviour ------------------------------------
    vm.sendline("reboot")
    wait_boot(vm, "boot 2")
    run(vm, "export PS1='root@box:~# '")

    marker = run(vm, "cat /root/marker 2>&1")
    check("*** work survives a reboot ***", "PROOF-OF-WORK" in marker, marker)

    subs = run(vm, "btrfs subvolume list / | awk '{print $NF}'")
    check("a second automatic checkpoint was taken", ".ckpt.0003." in subs or ".ckpt.0002." in subs, subs)

    lst = run(vm, "cleanslate list")
    check("list shows the slates", "baseline" in lst and "main" in lst, lst)
    check("list marks the running slate", "running" in lst, lst)

    # ---- boot 3: rollback --------------------------------------------------
    run(vm, "echo BROKEN > /root/marker")
    rb = run(vm, "cleanslate rollback main.2")
    check("rollback is staged", "next boot" in rb.lower(), rb)

    stg = run(vm, "cleanslate status")
    check("status reports the staged rollback", "pending" in stg.lower(), stg)

    vm.sendline("reboot")
    wait_boot(vm, "boot 3 (rollback applied)")
    run(vm, "export PS1='root@box:~# '")

    marker = run(vm, "cat /root/marker 2>&1")
    check("*** rollback restored the checkpoint ***", "PROOF-OF-WORK" in marker, marker)

    subs = run(vm, "btrfs subvolume list / | awk '{print $NF}'")
    kept = subs.count(".keep")
    check("the pre-rollback state was kept", kept >= 2, f"{kept} kept checkpoints: {subs}")

    # ---- fork --------------------------------------------------------------
    fk = run(vm, "cleanslate fork exp-1")
    check("fork creates a slate", "exp-1" in fk, fk)
    entries = run(vm, "ls /efi/loader/entries/")
    check("fork writes a boot entry", "cleanslate-exp-1.conf" in entries, entries)

    # ---- boot 4: scratch ---------------------------------------------------
    sw = run(vm, "cleanslate switch main --scratch")
    check("scratch run can be selected", "next boot" in sw.lower(), sw)

    vm.sendline("reboot")
    wait_boot(vm, "boot 4 (scratch)")
    run(vm, "export PS1='root@box:~# '")

    st = run(vm, "cleanslate status")
    check("scratch boot reports itself", "scratch" in st, st)
    check("scratch boot warns the work is lost", "discarded at reboot" in st, st)
    run(vm, "echo THROWAWAY > /root/scratch-marker")

    # ---- boot 5: scratch discarded, slate untouched ------------------------
    vm.sendline("reboot")
    wait_boot(vm, "boot 5 (back to main)")
    run(vm, "export PS1='root@box:~# '")

    gone = run(vm, "cat /root/scratch-marker 2>&1")
    check("*** scratch work is discarded ***", "THROWAWAY" not in gone, gone)
    marker = run(vm, "cat /root/marker 2>&1")
    check("the slate was untouched by the scratch run", "PROOF-OF-WORK" in marker, marker)

    hk = run(vm, "ssh-keygen -lf /etc/ssh/host_keys/ssh_host_ed25519_key.pub 2>&1 | awk '{print $2}'")
    check("host identity is stable across slates", "SHA256" in hk, hk)

    vm.sendline("poweroff")
    vm.expect(pexpect.EOF, timeout=90)

    failed = [r for r in results if not r[1]]
    print(f"\n{len(results) - len(failed)}/{len(results)} checks passed")
    for label, _, detail in failed:
        print(f"  FAILED: {label}\n    {detail}")
    return 1 if failed else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except pexpect.TIMEOUT as e:
        print(f"TIMEOUT waiting for the guest: {e}")
        sys.exit(2)
