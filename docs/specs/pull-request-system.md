# Pull Request System Specification

**Status:** Draft (pending Product Owner / CISO / Tester sign on this SHA)
**Last Updated:** September 2026
**Supersedes:** GitHub issue #18 (host per-skill `.git`); the May 2026 JSON PR-dashboard draft on this file
**Related:** `docs/specs/governance-court.md`, `docs/specs/sdlc-web-portal.md`

This document is the spec. Chat is not the spec. Store / Builder / Court-merge **code is forbidden** until Product Owner, CISO, and Tester sign this file at a new commit SHA.

## Purpose

Give an AegisClaw **instance user** real git for skills and their projects: clone, branch, pull request, rollback. Court reviews every change. **Store is the only merge.** The Merkle audit log remains the cryptographic source of truth; git is how the instance user versions tenant source.

This is not a JSON PR dashboard. Court `skipped` and merge “if required” are **out**. There is no optional git.

## Two planes

| Plane | What | Where |
| --- | --- | --- |
| Host TCB | AegisClaw itself: daemon, Hub, Store, Network Boundary | `PixnBits/AegisClaw` stays **GitHub**. Never bind-mount that tree into a guest. |
| Tenant | Skills and user projects inside a running instance | **Store remotes that speak git**. Never `workspace/skills/*/.git` on the host. |

Dogfooding AegisClaw-via-AegisClaw (exporting this GitHub tree through Store) is a later slice, not this spec.

GitHub **import/export of tenant repos** is later: Court plus a `network-access.yaml` change. This slice is Store-native.

## Issue #18 — superseded

#18 asked for per-skill history, branches, Court-gated change, and rollback plus re-review. That **job stays**. The proposed **place** (`workspace/skills/<name>/.git` on the host, optional, git inside the skill directory, host-filesystem git) is a tenancy break and is **rejected**.

Keep:

- Per-skill (and per-project) history, branches, tags, diffs
- Court-gated change; no commit/merge before Court
- Rollback that triggers a fresh Court review before it is live again

Drop:

- Host per-skill `.git`
- Optional / opt-in git (tenant source is **always** a Store repo)
- Git running on the host TCB disk for tenant source
- Git inside Coder VMs

Close #18 as superseded by this spec once this file is signed.

## Git-shaped to the instance user

Store remotes are **real git remotes**, not a private format wearing git words. Against a tenant’s own Store remote, these must work as git:

- `git clone`
- `git fetch` / `git pull`
- `git push` of a branch
- branch create / checkout (on a clone the user or Builder holds)
- open a pull request, review, merge (merge only via Store after Court)
- rollback (see below)

Clone/push use Hub/vsock only. No other git remote is reachable from Builder or Coder.

## Actors

| Actor | Role | Git? |
| --- | --- | --- |
| **Store** | Only disk for tenant code. Bare git remotes, PR state, Merkle log. Only merger. | Serves git. |
| **Hub** | Only hop. All Store git and PR RPCs are Hub/vsock. | No. |
| **Builder** | Untrusted, ephemeral. `store.git.clone` / `push` / `pr.create` for **that tenant’s** remotes only. | Yes, only via Store git API / that tenant’s remotes. Destroyed after the job. |
| **Court** | Votes on every PR. No skip. | No git. Reads diffs through Hub from Store. |
| **Coder** | Reasoning VM. | **Gitless.** No `git`, no host tree, no secrets. Diffs through Hub from Store. |
| **PM / Memory / other reasoning VMs** | Same isolation as Coder for source. | No git, no host trees, no secrets. |
| **Network Boundary** | Only secret/egress VM. | Not a git remote. |

Host daemon is TCB for lifecycle and Merkle roots only. It is not a git daemon for tenant source.

## Loop

1. Creating a skill or user project **always** creates a Store repo (bare git remote + ACL for that tenant). There is no unversioned tenant source.
2. Builder (not Coder) clones that remote via Hub/vsock, commits on a branch, `git push`, `pr.create`.
3. Court reviews the Store diff. Court skip **must not exist** in API, data model, or UI.
4. **Store-only merge** after Court approve. `pr.merge` from Coder, Builder, or host git **fails**.
5. Rollback is a Store revert that **opens a new PR**. That PR needs Court again. Rollback is not a host checkout and not a live reset that skips Court.
6. Builder is destroyed when the job ends. No git objects, worktree, or credentials remain.

## Tenancy locks (fail-closed)

- Hub/vsock only. No host git daemon. No Store git on the default pod/host network.
- Per-tenant ACL. No shared Store namespace across tenants.
- Tenant A cannot fetch or `git push` tenant B’s remotes.
- Builder cannot fetch **any other remote**: no extra remotes, no submodules, no hooks that phone home, no LFS egress.
- No git in Coder VMs. No GitHub tree bind-mounted into a guest.
- No batteries-included tools and no extra egress without Court plus a `network-access.yaml`.
- Secrets never leave the Network Boundary. They are not in Store remotes and not in Builder/Coder env.

## API (normative names)

Store git (instance user and Builder, Hub-mediated):

- `store.git.clone` / fetch / push — real git against that tenant’s remotes only
- `store.git.create` — new skill or project always allocates a Store repo

Pull requests:

- `pr.create` — Builder or instance user; PR always requires Court
- `pr.list` / `pr.get`
- `pr.merge` — **Store only**, and only after Court **approved**. No `skipped`. No “if required.”
- `pr.close`
- `pr.rollback` — creates a new PR from a prior Store ref; Court required again

Court:

- `court.review` states: `pending`, `in_progress`, `approved`, `rejected`
- **No** `skipped` state. Adding it is a spec break.

Coder has none of these. Coder may receive a Hub-mediated diff/patch for reasoning only.

## Fail-closed tests (must be in the build; not prose)

These are acceptance tests for the later Store implementation. Spec is unsigned until they are listed here; implementation is unsigned until they pass.

| ID | Test | Expected |
| --- | --- | --- |
| T1 | Merge without Court approve | fail |
| T2 | Court skip (API, field, or merge path) | fail (must not exist) |
| T3 | Cross-tenant fetch | fail |
| T4 | Tenant A `git push` to tenant B | fail |
| T5 | Extra remote, submodule, hook egress, or LFS from Builder | fail |
| T6 | Coder has `git` or a host/GitHub tree mount | fail |
| T7 | `git clone` and `git push` against the tenant’s own Store remote | pass (real git) |
| T8 | `pr.merge` invoked from Coder, Builder, or host git | fail (only Store merges) |
| T9 | New skill or project | always has a Store repo |
| T10 | Rollback | opens a **new** PR that needs Court again; live skip-Court reset fails |
| T11 | Destroyed Builder | no leftover git state, worktree, or credentials |
| T12 | Force-push, history delete/rewrite, or fake Court approve | fail |
| T13 | Git over anything except Hub/vsock; host git daemon listens; or `workspace/skills/*/.git` exists | fail |

## Out of scope (this spec)

- Implementing Store, Builder, or Court-merge code (sign this file first)
- GitHub import/export of tenant repos
- Batteries-included tool pack
- Extra egress / cloud LLM without Court + `network-access.yaml`
- Bind-mounting host trees into guests
- JSON PR dashboard / `dashboard.pr.*` as the system of record

## Related

- GitHub issue #18 — close as superseded after this SHA is signed
- `docs/specs/governance-court.md`
- Host TCB development remains on `PixnBits/AegisClaw` (GitHub)
