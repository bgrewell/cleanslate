# Terminology

This is the reference for anyone writing a user-facing string. The CLI and the
documentation drifted apart once already, when the code spoke in btrfs and the
docs spoke in layers; keeping one table means that argument only has to be had
once.

## The words

| Term | Means | On disk |
|---|---|---|
| **baseline** | The immutable baked image. Every slate starts as a copy of it, and it is never pruned, so it is always available as a way back. | `@baseline`, sealed btrfs read-only |
| **slate** | A named, persistent line of work. What you boot; what keeps your changes. | `@<name>` |
| **checkpoint** | A rollback point on a slate. Taken automatically at every boot, or deliberately with a message. | `@<name>.ckpt.<seq>.<auto\|keep>`, read-only |
| **scratch** | An opt-in throwaway boot. A copy of the baseline (or of a slate) that is discarded at the next reboot. | `@runtime` |
| **rescue** | A read-only boot of the baseline, for when a slate will not start. | `@baseline` |
| **basis** | What the running root was made from. Only interesting in scratch mode, where the root is a copy of something else. | — |

## Retired words

These must not appear in command output, help text, or the README. All of them
are correct in this directory and in code comments, where the audience is
someone reading the implementation.

| Retired | Use instead | Why |
|---|---|---|
| state | slate | Ambiguous, and it stuttered against the old `state` subcommand group |
| session | slate | Collides with login and SSH sessions, which are a different thing on a shared machine |
| environment | slate | Overloaded by dev/test/staging environments, virtualenvs, and environment variables |
| layer | slate, checkpoint | Implies a stack that users can address; they cannot |
| snapshot | checkpoint | Mechanism, not purpose |
| subvolume | slate, checkpoint | Mechanism |
| ephemeral | scratch | Jargon for a thing that has an ordinary word |
| dirty | — | Reads as a fault condition; in this model the normal state of a machine in use |
| fresh | baseline, scratch | Meant two different things in the old model and is now wrong for both |
| saved slate | slate | There is no unsaved counterpart any more — persistence is the default |

## Rules the wording follows

**No standing call to action.** `status` reports what is true and stops. An
instruction printed on every invocation trains people to run it, and the whole
point of the "what to keep" norm is that most work should be thrown away
rather than named.

**"scratch" appears in routine output exactly once**, in the `status` line for
a scratch boot. That is the one case where not knowing costs someone their
work, so it says the consequence — *everything written here is discarded at
reboot* — and not just the mode name. Everywhere else the word belongs to
documentation.

**Say the consequence, not the mechanism.** "changes here are kept" rather
than "mounted read-write"; "discarded at reboot" rather than "the subvolume is
deleted".

`cmd/cli/format_test.go` asserts the retired list against the real output, so
a regression here is a test failure rather than a review comment.
