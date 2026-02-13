# E2E Integration Branch Tests: Investigation & Design

> **Context**: PR #1226 (Xexr) adds integration branch support to Gas Town.
> Julian's [manual test plan](https://gist.github.com/julianknutsen/04217bd547d382ce5c6e37f44d3700bf) covers 28 tests across 6 parts.
> Xexr's [adapted version](https://gist.github.com/Xexr/f6c6902fe818aad7ccdf645d21e08a49) accounts for fork environment differences.
> This document investigates how to automate those tests as CI/CD integration tests.

---

## 1. What We're Testing

The integration branch lifecycle is a multi-component pipeline:

```
gt mq integration create <epic>     → creates branch off main, pushes to origin
gt sling <bead> <rig>               → dispatches work to polecat
gt done                              → polecat submits MR (target: integration branch)
Refinery (Claude + formula)          → merges polecat branch → integration branch
gt mq integration land <epic>        → merges integration branch → main (with guardrails)
```

Julian's test plan has 28 tests across 6 parts:
- **Part A** (A1-A3): Unit tests — config parsing, branch naming, target detection
- **Part B** (B1-B3): Regression — pre-closed epic, stale ref, duplicate create
- **Part C** (C1-C8): Lifecycle — create → sling → MR → merge → land (the hard part)
- **Part D** (D1-D4): Config & overrides — template overrides, `auto_land=false`
- **Part E** (E1-E5): Edge cases — conflicts, concurrent MRs, force push
- **Part F** (F1-F3): Deprecation — doctor checks, settings migration

---

## 2. Production Architecture: How the Refinery Actually Works

### The production stack (5 layers)

In production, the refinery merge queue works through a 5-layer stack:

```
Layer 1: Daemon (daemon.go)
  └─ Heartbeat loop calls ensureRefineryRunning() every 3 min
     └─ Creates refinery.Manager, calls mgr.Start()

Layer 2: Refinery Manager (manager.go)
  └─ Creates tmux session "gt-<rig>-refinery"
     └─ Working dir: <rig>/refinery/rig/
        └─ Starts Claude Code with initial prompt: "Run gt prime --hook and begin patrol."

Layer 3: Claude Code (LLM instance)
  └─ SessionStart hook runs "gt prime --hook"
     └─ Detects role: refinery
        └─ Outputs formula context to Claude's conversation

Layer 4: Formula (mol-refinery-patrol.formula.toml)
  └─ Step-by-step instructions Claude follows:
     inbox-check → queue-scan → process-branch → run-tests →
     handle-failures → merge-push → loop-check → ...

Layer 5: Shell commands (what Claude actually executes)
  └─ git fetch --prune origin
  └─ gt refinery ready <rig>          ← uses Engineer.ListReadyMRs()
  └─ git checkout -b temp origin/<polecat-branch>
  └─ git rebase origin/main
  └─ git merge --ff-only temp
  └─ git push origin main
  └─ bd close <mr-bead-id>
  └─ gt mail send <rig>/witness -s "MERGED <polecat>"
```

The merge itself — the rebase, merge, and push — is performed by **Claude executing raw git commands**, following the formula's step-by-step instructions. The formula IS the control plane.

### The Engineer struct: two roles, only one used in production

The `Engineer` struct (`internal/refinery/engineer.go`) contains two categories of methods:

**Query/state methods — USED in production** (called by CLI commands that the formula tells Claude to run):

| Method | Called by | What it does |
|--------|----------|-------------|
| `ListReadyMRs()` | `gt refinery ready` | Lists unclaimed, unblocked MRs |
| `ListBlockedMRs()` | `gt refinery blocked` | Lists MRs blocked by open tasks |
| `ListQueueAnomalies()` | `gt refinery ready` | Detects stale claims, orphaned branches |
| `ClaimMR()` | `gt refinery claim` | Assigns MR to a worker |
| `ReleaseMR()` | `gt refinery release` | Returns MR to queue |

**Merge execution methods — ZERO callers anywhere in the codebase:**

| Method | Callers | Status |
|--------|---------|--------|
| `ProcessMR()` | None | Dead code |
| `ProcessMRInfo()` | None | Dead code |
| `doMerge()` | Only by ProcessMR/ProcessMRInfo (internal) | Dead code |
| `handleSuccess()` | None | Dead code |
| `HandleMRInfoSuccess()` | None | Dead code |
| `HandleMRInfoFailure()` | None | Dead code |

There is no `gt refinery process` or `gt refinery merge` command. No CLI command calls any of the merge execution methods. The formula tells Claude to run git commands directly — it never invokes the Engineer's merge logic.

### Why the merge methods exist but aren't used

The Engineer's merge methods appear to be a **Go library API** that encapsulates the same merge logic as the formula's git commands:

```go
// engineer.go — doMerge() mirrors the formula's process-branch + merge-push steps
func (e *Engineer) doMerge(ctx, branch, target, sourceIssue) ProcessResult {
    e.git.BranchExists(branch)           // formula: git branch -r | grep <branch>
    e.git.Checkout(target)               // formula: git checkout main
    e.git.Pull("origin", target)         // formula: (implicit in git fetch)
    e.git.CheckConflicts(branch, target) // formula: git rebase detects conflicts
    e.git.MergeSquash(branch, msg)       // formula: git merge --ff-only temp
    e.git.Push("origin", target)         // formula: git push origin main
}
```

But these methods are never wired into any command or production path. The production refinery relies entirely on Claude following formula instructions. The Go library API was likely either:
- Built as a programmatic foundation that was superseded by the formula-driven approach
- Intended for a future `gt refinery process` command that was never created
- A testable encapsulation of merge logic that predates the current formula architecture

**The formula and the Engineer's merge methods are NOT "two parallel implementations" — the formula is the only implementation used in production. The Engineer's merge methods are unused code.**

### Implications for e2e testing

This is a critical finding for test design. We have three options for testing the merge step:

| Approach | What it tests | Fidelity |
|----------|-------------|----------|
| **A: Call `Engineer.ProcessMRInfo()` directly** | The Go merge logic (squash merge via `e.git.*`) | Tests code that is never called in production |
| **B: Simulate what Claude does (raw git commands)** | The actual production merge path | Tests what actually runs, but just tests git itself |
| **C: Test the CLI commands the formula references** | `gt refinery ready`, `gt refinery claim`, etc. | Tests the query/state layer that IS used in production |

**Option A** (what the original investigation proposed) tests the unused Go library path. While this validates the merge mechanics work correctly, it's testing dead code — if the merge methods had a bug, production wouldn't be affected because production doesn't call them.

**Option B** would essentially be testing `git rebase` and `git merge` — we'd be testing git, not our code.

**Option C** tests the parts of the Engineer that production actually uses: queue listing, MR claiming, anomaly detection.

**Recommended approach for Part C lifecycle tests:**
- Use **subprocess calls** for the entire lifecycle: `gt mq integration create`, simulate polecat work with git commands, simulate the merge with git commands (mimicking what Claude does), then `gt mq integration land`
- Use **Option C** to test that `gt refinery ready` correctly lists MRs targeting integration branches
- Reserve **Option A** only if the merge methods are ever wired into a production command

### What we're really testing (revised)

The e2e tests for integration branches should focus on what our code actually does:

| What we're testing | How | Our code involved |
|-------------------|-----|-------------------|
| Branch creation | `gt mq integration create` subprocess | `runMqIntegrationCreate()` |
| Target detection | `gt done` subprocess or `detectIntegrationBranch()` | `mq_submit.go` |
| MR listing for integration branch | `gt refinery ready` subprocess | `Engineer.ListReadyMRs()` |
| Integration status reporting | `gt mq integration status` subprocess | `runMqIntegrationStatus()` |
| Landing with guardrails | `gt mq integration land` subprocess | `runMqIntegrationLand()` |
| The merge itself | Raw git commands in test (simulating Claude/formula) | None — git does this, not our code |

The merge step in production is Claude running `git rebase` + `git merge --ff-only` + `git push`. Our code doesn't participate in the merge execution — it only sets up the branches (create), detects the target (done/submit), lists the queue (refinery ready), and tears down afterward (land). **Those are the boundaries we should test.**

---

## 3. Formula vs Engineer: Divergences and LLM-Dependent Weak Points

### Merge strategy divergence

The formula and the Engineer's unused merge methods implement **different merge strategies**:

| | Formula (production) | Engineer (unused) |
|---|---|---|
| **Strategy** | `rebase` + `merge --ff-only` | `merge --squash` |
| **History** | Linear — original commits preserved | Single squash commit |
| **Conflict detection** | Side effect of rebase attempt | Separate pre-flight `CheckConflicts()` (test merge with `--no-commit --no-ff`, inspect, abort) |
| **Branch handling** | Creates `temp` branch from `origin/<branch>` | Works with local branch directly |
| **Target update** | Implicit via `git fetch` | Explicit `e.git.Pull("origin", target)` |

These produce **different commit histories** on the same inputs.

### Conflict resolution lifecycle divergence

When a merge conflict is detected, the two paths diverge significantly:

**Formula (production):**
1. Rebase fails → `git rebase --abort`
2. Claude runs `bd create --type=task ...` to create a conflict resolution task
3. Skip MR, move to next branch
4. **No dependency created** between MR and conflict task
5. Next patrol cycle → `gt mq list` shows MR still "open" → Claude tries rebase again
6. If branch was force-pushed with resolution → rebase succeeds
7. If not → Claude hits same conflict, potentially creates **duplicate** conflict task

**Engineer (unused):**
1. `doMerge` returns `Conflict: true`
2. `HandleMRInfoFailure()` creates conflict task AND calls `e.beads.AddDependency(mr.ID, taskID)` — **blocks the MR on the task**
3. Merge slot acquired — serializes conflict resolution (only one at a time)
4. `ListReadyMRs()` filters out blocked MRs — refinery won't retry until task is closed
5. When someone closes the task → MR unblocks → appears in `gt refinery ready` again

| Behavior | Formula | Engineer |
|----------|---------|----------|
| MR blocked until resolution? | No — retries every patrol cycle | Yes — blocked via beads dependency |
| Duplicate conflict tasks? | Possible on each retry | No — MR is blocked, won't be reprocessed |
| Serialized resolution? | No | Yes — merge slot |
| Retry trigger | Next patrol cycle (time-based) | Task closure (event-based) |

### Who resolves the conflict task?

A dedicated **conflict resolution formula** exists: `mol-polecat-conflict-resolve.formula.toml`. It guides a polecat through conflict resolution with a fundamentally different merge path than normal polecat work:

| Aspect | Regular polecat work | Conflict resolution |
|--------|---------------------|---------------------|
| Branch | Create new branch | Checkout existing MR branch |
| Merge path | Submit to queue via `gt done` | **Push directly to target branch** |
| Issue closure | Refinery closes after merge | Polecat closes MR bead itself |
| Serialization | None | Merge-slot gate required |
| Formula | `mol-polecat-work` (auto-applied) | `mol-polecat-conflict-resolve` (must be explicit) |

The conflict resolution formula's steps:
1. **load-task** — parse metadata (original MR, branch, conflict SHA) from task description
2. **acquire-slot** — `bd merge-slot acquire --wait` (serializes resolution)
3. **checkout-branch** — `git checkout -b temp-resolve origin/<branch>`
4. **rebase-resolve** — `git rebase origin/main`, resolve conflicts using judgment
5. **run-tests** — verify resolution doesn't break anything
6. **push-to-main** — `git push origin temp-resolve:<target>` (**bypasses merge queue**)
7. **close-beads** — close original MR bead AND source issue (refinery normally does this)
8. **release-slot** — `bd merge-slot release`
9. **cleanup-and-exit** — close conflict task, `gt done`

**Why direct push?** Going back through the merge queue would create an infinite loop — the MR was already reviewed/approved, the polecat is just resolving conflicts.

### Dispatch gap: nobody auto-slings conflict tasks

The conflict task is created as a regular bead (`type: task`) and appears in `bd ready`. However:

1. **The witness does NOT scan `bd ready`** for unassigned tasks. It only reacts to mail (MERGE_READY, POLECAT_DONE, etc.). The witness patrol formula has no step for proactive task dispatch.

2. **`gt sling` auto-applies `mol-polecat-work`** (line 387-389 of `sling.go`), NOT `mol-polecat-conflict-resolve`. To use the correct formula, you must explicitly specify it:
   ```bash
   gt sling mol-polecat-conflict-resolve --on <conflict-task-id> gastown
   ```

3. **No agent proactively monitors `bd ready`** and dispatches conflict tasks.

The task sits in `bd ready` until someone (human or agent) manually runs the sling command with the correct formula. **This is a manual gap in an otherwise automated pipeline.**

### MERGE_FAILED protocol: notification only, no dispatch

The formula's `handle-failures` step sends `MERGE_FAILED` to the witness for **test failures** (not conflicts). For conflicts, the formula just creates a task and skips — no MERGE_FAILED is sent.

The witness's `HandleMergeFailed()` handler (`handlers.go:368-416`) only notifies the original polecat that their merge was rejected. It does NOT:
- Create tasks
- Dispatch work
- Trigger conflict resolution

And the original polecat may already be nuked after `gt done`, so the notification may go nowhere.

### End-to-end conflict resolution flow (actual)

```
Refinery patrol: rebase fails
  ↓
Formula: Claude creates conflict task (bd create --type=task ...)
  ↓
Task appears in `bd ready`
  ↓
 ── MANUAL GAP ──
  ↓
Human/agent runs: gt sling mol-polecat-conflict-resolve --on <task> <rig>
  ↓
Polecat spawned with conflict resolution formula
  ↓
Polecat: acquires merge slot
  ↓
Polecat: git checkout + rebase + resolve conflicts (LLM judgment)
  ↓
Polecat: runs tests
  ↓
Polecat: pushes DIRECTLY to target branch (bypasses merge queue)
  ↓
Polecat: closes MR bead + source issue + conflict task
  ↓
Polecat: releases merge slot, runs gt done
  ↓
Witness: receives POLECAT_DONE, nukes polecat
```

**This is an untested lifecycle with a manual dispatch gap and no deterministic guarantee of completion.**

### LLM-dependent weak points in the formula

Every step in the formula is prose that Claude must follow correctly. With different LLM models configured for refineries, behavior becomes uncertain at these points:

| Formula Step | What Claude must do | Failure mode with weaker model |
|---|---|---|
| **inbox-check** | Parse MERGE_READY mail, extract and **remember** branch, issue, polecat name, MR bead ID across multiple later steps | Forgets polecat name → MERGED notification fails → polecat worktrees accumulate indefinitely |
| **process-branch** | Substitute correct branch names into git commands, detect conflict state from exit codes | Wrong branch name → merges wrong code or loses work |
| **handle-failures** | Diagnose whether test failure is a branch regression vs pre-existing on target branch | Wrong diagnosis → merges broken code OR rejects good code |
| **merge-push** | Verify SHA match after push, send MERGED mail with correct polecat name, close correct MR bead, archive correct message — all in sequence | Any dropped step → silent lifecycle breakage (orphaned worktrees, orphaned beads, inbox bloat) |
| **check-integration-branches** | Read `auto_land` config value, respect FORBIDDEN directive | Ignores FORBIDDEN → lands integration branch autonomously when it shouldn't |
| **conflict handling** | Create properly formatted conflict task with all metadata fields | Missing metadata → conflict task is useless. Duplicate tasks on retry cycles. |

**The pre-push hook is the ONE deterministic guardrail** — it's code that runs regardless of what the LLM does. Everything else in the merge pipeline is prose-dependent.

### Where deterministic Go code could replace LLM prose

The Engineer's unused merge methods already implement most of the merge-push sequence deterministically. A command like `gt refinery process-next` that wires in the Engineer's merge logic would make the critical path code-deterministic:

| Current (LLM-dependent) | Potential (deterministic) |
|---|---|
| Claude remembers polecat name across steps | Go struct carries `MRInfo` through the pipeline |
| Claude constructs git commands from prose | `doMerge()` calls `e.git.*` methods |
| Claude decides whether to send MERGED mail | `HandleMRInfoSuccess()` always sends notification |
| Claude creates conflict task from prose template | `createConflictResolutionTaskForMR()` with dependency blocking |
| Claude diagnoses test failure cause | Could be codified: run tests on target first, then on merge, compare |

The LLM's role would shrink from **executing the merge mechanics** to **orchestrating the patrol loop** (when to process, whether to continue or hand off) — a much smaller surface area for model-dependent behavior.

### Implications for e2e testing

These findings affect what we can and should test:

1. **Formula FORBIDDEN directives are untestable in Go** — they're Claude-level guardrails. The pre-push hook is the testable enforcement layer.
2. **Conflict resolution lifecycle is untested end-to-end** — no test covers: conflict detected → task created → task dispatched → conflict resolved → direct push → beads closed.
3. **The formula's retry-without-blocking approach risks duplicate conflict tasks** — this is a testable scenario (the Engineer's dependency-blocking approach prevents this, but is unused).
4. **`gt sling` formula routing is not conflict-aware** — auto-applies `mol-polecat-work` instead of `mol-polecat-conflict-resolve`. This could be tested: sling a conflict task to a polecat, verify the wrong formula is applied.
5. **Direct push bypasses merge queue** — the conflict resolution formula pushes directly to the target branch. This is intentional (avoids infinite loop) but means the merge queue's test/validation step is skipped for resolved conflicts. Testable: verify push goes to correct target branch.
6. **If `gt refinery process-next` existed**, the merge pipeline + conflict handling would be deterministically testable without simulating Claude. The Engineer already has dependency-blocking and merge-slot serialization — the formula has neither.

---

## 4. Existing Test Infrastructure

### Test helpers already available

| Helper | Location | Purpose |
|--------|----------|---------|
| `buildGT(t)` | `internal/cmd/test_helpers_test.go` | Compiles `gt` binary, caches across tests |
| `createTestGitRepo(t, name)` | `internal/cmd/rig_integration_test.go` | Creates git repo with initial commit on `main` |
| `setupTestTown(t)` | `internal/cmd/rig_integration_test.go` | Creates `townRoot/mayor/rigs.json` + `.beads/` |
| `mockBdCommand(t)` | `internal/cmd/rig_integration_test.go` | Fake `bd` binary on PATH (handles init, create, show) |
| `cleanGTEnv(t)` | various | Strips `GT_*` env vars |

### CI infrastructure

- **`ci.yml`** integration job: builds `bd` from source, builds `gt`, runs `go test -tags=integration -timeout=5m`
- **`integration.yml`**: path-filtered trigger, 8-min timeout, same setup
- Both set `git config --global user.name "CI Bot"`
- No dolt server in CI. No daemon. No tmux.
- 10 existing integration test files with `//go:build integration` tag
- No dedicated e2e test category exists today

### Existing refinery tests

`internal/refinery/engineer_test.go` tests config loading and `NewEngineer` construction with `rig.Rig{Name: "test-rig", Path: tmpDir}`. These are unit tests — no merge operations are tested.

---

## 5. Proposed Architecture

### New build tag: `e2e`

```go
//go:build e2e
```

**Why not `integration`?**
- `integration` tag is already used for 10 test files with 5-8 min timeouts
- E2E tests will be heavier (Part C lifecycle could take 30-60s per subtest)
- Separate tag allows separate CI trigger rules and timeout budgets

### Package structure

```
internal/e2e/
  testutil/
    town.go           ← SetupTestTown, SetupTestRig, SetupTestGitRemote
    beads.go           ← SetupMockBeads (or real bd with tmpdir)
    engineer.go        ← SetupTestEngineer (wraps refinery.NewEngineer)
    git.go             ← Helpers for creating branches, commits, worktrees
    assertions.go      ← AssertBranchExists, AssertBranchDeleted, AssertCommitOn
  integration_branch_test.go  ← All 28 tests (subtests within TestIntegrationBranch*)
  (future: sling_test.go, convoy_test.go, etc.)
```

**Why a new `e2e` package (not inside `internal/cmd`)?**
- `internal/cmd` tests are tightly coupled to cobra commands and `cmd` package internals
- E2E tests need to import from multiple packages (`refinery`, `beads`, `git`, `rig`, `config`)
- Separate package forces clean API boundaries
- Future e2e tests for other features (sling, convoy, etc.) can share the same `testutil` fixtures

### CI workflow

```yaml
# .github/workflows/e2e.yml
name: E2E Tests
on:
  pull_request:
    paths:
      - 'internal/refinery/**'
      - 'internal/cmd/mq_*'
      - 'internal/cmd/done*'
      - 'internal/cmd/sling*'
      - 'internal/e2e/**'
      - '.github/workflows/e2e.yml'
jobs:
  e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      # ... standard Go setup, build bd, build gt ...
      - run: go test -v -tags=e2e -timeout=10m ./internal/e2e/...
```

---

## 6. Challenges & Mitigations

### Challenge 1: Beads (`bd`) dependency

The beads CLI talks to a Dolt SQL server in production. CI doesn't have dolt running.

| Approach | Pros | Cons |
|----------|------|------|
| **A: Mock bd** (like existing tests) | Fast, no dolt needed | Doesn't test real bead CRUD; MR fields may drift |
| **B: Real bd with embedded mode** | Tests real paths | Embedded dolt crashes in certain CWDs; slow init |
| **C: Real bd with flat-file backend** | Tests real paths, no dolt | Needs verification that `bd` supports this |
| **D: Build bd from source + dolt server** | Full fidelity | Complex CI setup, slow |

**Recommended:** Start with **A (mock bd)**, graduate to C if flat-file works. The existing `mockBdCommand` pattern is proven. The things we care about testing (git operations, branch targeting, merge mechanics) don't depend on beads persistence fidelity.

### Challenge 2: Git remote operations

The Engineer calls `git push origin` and `git pull origin`. In tests, "origin" needs to be a local bare repo, not GitHub.

**Mitigation:** Already solved by `createTestGitRepo(t, name)`. We create a bare repo on the filesystem and use it as the remote:

```
tmpDir/
  remote.git/           ← bare repo (simulates GitHub)
  town/
    testrig/
      refinery/rig/     ← clone of remote.git (refinery's worktree)
      mayor/rig/        ← clone of remote.git (mayor's clone)
      polecats/nux/     ← git worktree (simulates polecat)
      config.json
```

### Challenge 3: Polecat simulation

Real polecats are git worktrees created by the Witness/Sling system, backed by Claude sessions. We don't need any of that for testing.

A polecat's contribution to the lifecycle is just:
1. Create a git worktree from the rig's shared `.repo.git`
2. Make commits on a branch named `polecat/<name>`
3. Push the branch
4. Call `gt done` (which creates an MR bead)

For testing, we can simulate steps 1-3 with raw git commands and step 4 by directly creating an MR bead via the mock `bd`.

### Challenge 4: `gt done` is a cobra command

`gt done` discovers the workspace via CWD, reads rig config, finds the current polecat's branch, and creates an MR bead. It's heavily tied to the `cmd` package.

| Approach | Feasibility |
|----------|-------------|
| Call `gt done` as subprocess | Works — we have the built binary from `buildGT(t)` |
| Extract MR creation into a testable function | Cleaner but large refactor |

**Recommended:** Subprocess approach. Run `gt done` from within the simulated polecat worktree. The mock `bd` handles bead creation. This tests the actual binary path.

### Challenge 5: `gt mq integration land` has guardrails

The land command pushes to main, which triggers the pre-push hook. The hook checks for `GT_INTEGRATION_LAND=1`.

**Mitigation:** In tests, we can:
- Test **with** hook installed (verify guardrails work — blocked without env var)
- Test **with** `GT_INTEGRATION_LAND=1` env (verify the bypass works)
- Test **without** hook (baseline merge works)

### Challenge 6: `auto_land` config (from PR #1226)

The `integration_branch_auto_land` config controls whether the refinery can autonomously land integration branches. Default is `false`. The formula FORBIDDEN directives and pre-push hook enforce this.

Tests needed:
- `auto_land=false` (default) → `gt mq integration status` reports auto-land disabled
- `auto_land=true` → `gt mq integration status` reports auto-land enabled, `ready_to_land` reflects epic state
- Interplay between `auto_land` config and pre-push hook `GT_INTEGRATION_LAND` env var
- Note: The formula's FORBIDDEN directives are Claude-level guardrails — they can't be tested in Go code. The pre-push hook is the enforceable guardrail we can test.

### Challenge 7: Local vs CI runability

**Both should work.** The test scaffolding uses `t.TempDir()`, mock binaries, and local git repos. No external services needed. CI-specific concerns:
- Build tags (`-tags=e2e`) need to be in a workflow
- Timeout budget: Part C lifecycle tests need ~2 min total
- `bd` binary needs to be on PATH (mock or real)

---

## 7. Test Coverage Mapping

### Julian's 28 tests → Automated tests

| Part | Tests | Automation Approach |
|------|-------|-------------------|
| **A: Unit** (A1-A3) | Config parsing, branch naming, target detection | Already exist as unit tests; add any missing |
| **B: Regression** (B1-B3) | Pre-closed epic, stale ref, duplicate create | Pure Go tests calling `runMqIntegration*` functions |
| **C: Lifecycle** (C1-C8) | Create → Sling → MR → Merge → Land | Full scaffolding: bare repo + town + engineer |
| **D: Config** (D1-D4) | Template overrides, `auto_land=false` | Config file variations, subprocess calls |
| **E: Edge** (E1-E5) | Conflicts, concurrent MRs, force push | Git state manipulation |
| **F: Deprecation** (F1-F3) | Doctor checks, settings migration | `gt doctor` subprocess calls |

### Part C test flow (the crown jewel)

```
TestIntegrationBranchLifecycle(t *testing.T):
  1. SetupTestTown(t)                  → town with rig, bare git remote
  2. gt mq integration create <epic>   → creates integration branch (OUR CODE)
  3. Simulate polecat work             → git worktree, commit, push branch
  4. Create MR bead (target: integration/...)
  5. gt refinery ready                 → verify MR appears in queue (OUR CODE)
  6. Simulate merge (raw git commands) → rebase + merge + push (MIMICS CLAUDE)
  7. Verify: integration branch has the commit
  8. gt mq integration land            → merges integration → main (OUR CODE)
  9. Verify: main has the commit, integration branch deleted
```

Note: Step 6 uses raw git commands (what Claude would do following the formula), not
`Engineer.ProcessMRInfo()` which has zero callers in production. Steps 2, 5, and 8
test our actual code paths.

---

## 8. Open Questions

1. **Mock bd vs real bd?** Mock is simpler and faster. Real bd tests more but needs either flat-file backend or dolt. Recommend starting with mock.

2. **How to handle the merge step in Part C?** The production merge is Claude running git commands — our code doesn't participate. Recommend simulating the merge with raw git commands in the test (rebase + merge + push), same as what Claude does. This tests the full lifecycle end-to-end while focusing our assertions on the code boundaries we own (create, target detection, queue listing, land).

3. **New `e2e.yml` workflow vs existing `ci.yml`?** New workflow with path filters — e2e tests only run when integration-related code changes.

4. **Should `testutil` fixtures live in the e2e package or be shared?** Start in `internal/e2e/testutil/`. If other packages need them later, promote to `internal/testutil/`.

5. **What to do about Engineer's unused merge methods?** `ProcessMR()`, `ProcessMRInfo()`, `doMerge()`, `handleSuccess()`, `HandleMRInfoSuccess()`, `HandleMRInfoFailure()` have zero callers in the entire codebase. Options: (a) leave as-is (harmless dead code), (b) wire them into a `gt refinery process` command so the production path has a programmatic option, (c) remove them. If (b), then e2e tests could call the command instead of simulating git operations. This is a design question for the upstream maintainers.

---

## 9. References

- Julian's manual test plan: https://gist.github.com/julianknutsen/04217bd547d382ce5c6e37f44d3700bf
- Xexr's adapted test plan: https://gist.github.com/Xexr/f6c6902fe818aad7ccdf645d21e08a49
- Engineer source: `internal/refinery/engineer.go`
- Existing integration tests: `internal/cmd/rig_integration_test.go`
- CI workflow: `.github/workflows/ci.yml`
- Pre-push hook: `.githooks/pre-push`
