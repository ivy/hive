# Retrospective

Hive is done. It is not archived because it failed to work — v1 worked, and the
first session it drove produced 50 commits. It is done because the bet
underneath it lost, and continuing to invest meant paying for the wrong
substrate twice.

This document records what the experiment settled, so the next attempt starts
from the answers instead of the questions.

## The bet

**A GitHub Projects board is a good enough backlog for an agent orchestrator.**
Drag an issue to Ready, an agent picks it up, a PR comes back.

The pipeline in [`docs/architecture.md`](architecture.md) is that bet made
concrete: a board column is the queue, an issue is the work item, the status
field is the state machine.

## What held up

These are the parts worth carrying into anything that replaces Hive.

- **The workspace as the contract.** A git worktree plus metadata on disk,
  handed between `prepare`, `exec`, and `publish`, survived crashes and made
  every stage independently re-runnable. See
  [`docs/core-principles.md`](core-principles.md).
- **`systemd-run` for isolation.** ~50ms startup, exact host tool parity, no
  image to maintain. The details that cost real debugging time are in
  [`docs/prototype-learnings.md`](prototype-learnings.md).
- **The issue body as the prompt.** A well-written issue needed no prompt
  engineering on top of it.
- **Unattended dispatch as systemd units.** One unit per session, with the
  session UUID in the unit name, made the running fleet inspectable with tools
  that already existed.

## Why it stopped

### The backlog was the wrong shape

GitHub Projects has no dependency graph, no cross-repo story, and no way to ask
"what is ready?" as a query. Readiness stayed a human judgement call expressed
by dragging cards, which put the operator back in the loop at exactly the point
the project existed to remove them from.

The failure was not in the loop. The loop worked. The failure was that the
thing feeding the loop could not answer the only question the loop needed
answered.

### Agents carried the operator's identity

Every agent ran with credentials that were, in practice, the operator's whole
online identity. `docs/explanation/security-model.md` describes mount-based
isolation against a helpful-agent-doing-something-unhelpful threat model, and
that isolation worked as designed — but the credential handed across the
boundary was still far broader than the work required. The right answer is
minted, scoped, per-request credentials the agent never holds, which is a
different project.

### Workflows were static

One hard-coded path: prepare, exec, publish. A docs-only change, a dependency
bump, and a feature all got the same treatment. Adding a second workflow meant
changing code, not writing a definition.

### Nothing was enforced

Every guardrail was prose in a prompt asking the agent to police itself. Steps
were skipped and nothing recorded which ones ran. A step is only trustworthy
when its completion is proven by evidence the orchestrator checks — a commit
SHA exists, tests exited 0, `gh pr view` returns a URL — rather than by an
agent reporting it did the step.

### Visibility was thin

`hive ls`, `hive attach`, and journal logs were enough to debug a session the
operator already knew to look at. They were not enough to answer "what is
happening right now, and does it need me?" without going and looking.

### The substrate was going to keep charging rent

Polling a project board is not how you find out something became ready. Making
it feel immediate meant webhooks, a local mirror of board state, and
reconciliation between the two — a meaningful pile of work whose entire payoff
was making a backlog that was already the wrong shape respond faster.

Static workflows, enforcement, and visibility were all planned and all
tractable. The judgement call was that building them on this substrate was not
worth it: the project management system was generating work rather than
draining it.

## What it taught

Three things, in the order they matter.

1. **The backlog must be dependency-aware and agent-native.** Readiness is a
   property of a graph, and it should be a query. This became a separate
   project rather than a feature.
2. **Agents must hold no secrets.** Credentials should be minted, scoped, and
   injected per request, never inherited from the human. This is the more
   interesting problem of the two, and the one worth writing about.
3. **A working v1 is worth building even when you already suspect the substrate
   is wrong.** Nothing above was knowable from a design document. The
   experiment paid for itself in answers.

None of this is a complaint about GitHub Projects, which remains a good board
for humans. It is a poor fit for agentic workloads today, and that is a
statement about the workload, not the tool.

## State of the artifact

The code in this repository works. It is unmaintained as of September 2026 and
has been dormant since March 2026. Read it for the pieces listed under
[What held up](#what-held-up); do not deploy it.
