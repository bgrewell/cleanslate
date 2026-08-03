# cleanslate

A custom Ubuntu-based distro for machines that keep your work and can always
go back.

You build the baseline once and install it on as many machines as you like.
From then on, each machine runs a **slate**: a named, persistent line of work.
Boot it, install things, break things, reboot — it is all still there. Every
boot leaves a checkpoint behind, so there is always a way back to how things
were.

When a setup is worth keeping separately, fork it into its own slate. When a
machine needs to go to someone else, reset it to the pristine baseline, which
is never deleted no matter how far a slate has drifted.

```
$ cleanslate status
slate       pg-tuned
mode        persistent — changes here are kept
booted      2026-08-03 09:12 UTC (7h 4m ago)
checkpoint  taken at boot
```

## Working on a machine

Everything is where you left it, so most of the time there is nothing to do.
The commands are for the moments when there is.

**Mark a state worth returning to.** Checkpoints made this way are kept until
you remove them, unlike the ones taken automatically at each boot, which roll
off as newer ones arrive.

```sh
cleanslate checkpoint -m "postgres tuned, benchmarks pass"
```

**Go back.** Rollback takes effect at the next boot — a slate is the running
system and cannot be replaced underneath itself. The state you are leaving is
checkpointed first, so a rollback can itself be rolled back.

```sh
cleanslate history
cleanslate rollback pg-tuned.4 --reboot
```

**Start a new line of work** from where you are, leaving the current slate
untouched:

```sh
cleanslate fork pg-tuned-v2
cleanslate switch pg-tuned-v2 --reboot
```

**Try something risky** without consequences. A scratch run copies the slate,
runs from the copy, and throws it away at the next reboot:

```sh
cleanslate switch pg-tuned --scratch --reboot
```

**Hand the machine over.** `reset` returns a slate to the freshly built
baseline. The old state is checkpointed first, and the baseline is never
pruned, so this works however long the machine has been in use.

```sh
cleanslate reset --reboot
```

## What to keep

Keeping things is the default here, which makes it worth being deliberate
about what deserves its own name.

The rule: **explore on one slate, fork from another.** A slate worth naming
should hold a conclusion you reached, not the search that got you there.

Say you spend a day tuning Postgres. You try three storage layouts and four
`postgresql.conf` variants, build `pgbench` from source to compare them, load
40 GB of test data, and leave a dozen half-edited files behind. By the end you
know the answer: XFS on the NVMe, `shared_buffers=32GB`, WAL on a separate
device.

Don't fork that. Roll back to the checkpoint from before you started, do only
what the conclusion requires, and fork *that*:

```sh
cleanslate rollback pg-tuned.11 --reboot

sudo mkfs.xfs /dev/nvme1n1
sudo mount /dev/nvme1n1 /var/lib/postgresql
sudo apt install -y postgresql-16
sudo cp postgres-tuned.conf /etc/postgresql/16/main/postgresql.conf
sudo systemctl restart postgresql
pg_isready && sudo -u postgres psql -c 'show shared_buffers'

cleanslate checkpoint -m "postgres tuned for the ingest workload"
cleanslate fork pg-tuned-v1
```

Six commands and a check. The new slate holds exactly what someone needs, it
is small, and anyone can read those steps and know what is in it. The 40 GB of
test data and the three layouts that lost are not in it, because they were
never part of the answer.

A useful test: **if you can't write down what's in a slate, it isn't ready to
be forked.** Prefer `v1` → `v2` over editing a good slate in place — the
working one keeps working while you build the next.

### One thing checkpoints cannot capture

If something on a slate creates its own filesystem inside it — a container
runtime configured to do so, or a manually created btrfs subvolume — a
checkpoint cannot capture what is inside it, and rolling back would leave it
empty. `cleanslate status` warns while this is true, `list` marks the slate,
and `checkpoint` refuses unless you pass `--allow-incomplete`.

Data that needs to survive a rollback belongs on the slate itself or on a
separate disk, not in a filesystem nested inside the slate.

## The boot menu

Three entries, plus one per slate:

| Entry | What it does |
|---|---|
| a slate, e.g. `main` | Boots it and keeps what happens. This is the normal case. |
| `scratch` | A throwaway copy of the baseline, discarded at reboot. |
| `rescue` | The baseline itself, read-only, for when a slate will not boot. |

A machine's first boot creates a slate called `main` from the baseline, so a
new install behaves like an ordinary Ubuntu box until you decide otherwise.

## A note on shared machines

Nothing is erased automatically. Two people using the same machine one after
the other see each other's work unless someone resets it or boots `scratch`.

That is a deliberate trade. Erasing on every reboot would make any procedure
that reboots part-way through — driver installs, firmware, multi-stage
installers — impossible to finish, and that is ordinary work on the hardware
this is built for.

If a machine needs to come up clean for the next person, `cleanslate reset` is
the command, and the `scratch` entry is the way to work without leaving
anything behind.

## Building and installing machines

```sh
make build                              # build the CLI
sudo ./bin/cleanslate build             # bake the baseline image
sudo ./bin/cleanslate install /dev/sdX  # write it to a disk
```

Building needs root: the last step loop-mounts the image to lay out its
subvolumes. See [docs/development.md](docs/development.md) for requirements,
the qemu recipe, and repository layout.

## What works today

Implemented and verified end to end: building and installing images,
persistent slates, automatic and manual checkpoints, retention, rollback,
fork, switch, scratch runs, reset, rescue boot, and stable SSH host identity
across slates.

Not built yet:

- **Updating the baseline in place.** Changing the baked image means building
  and reinstalling it. A guarded update path is planned, deliberately scoped
  to updates and security patching rather than changes of purpose.
- **Moving slates between machines** (`export` / `import`).
- **Per-slate disk usage** in `list`. Deliberately omitted rather than
  deferred — see [docs/design.md](docs/design.md) for why the honest number is
  expensive and the cheap one is misleading.

UEFI is the only supported boot path. BIOS is not installed by the build.

See [docs/design.md](docs/design.md) for the architecture and the reasoning
behind it, and [docs/terminology.md](docs/terminology.md) for what the words
mean.
