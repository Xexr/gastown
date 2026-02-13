# Implementation Plan: `gt refinery process-next`

> **The biggest win**: Wire the Engineer's existing dead merge code into a single CLI command.
> This one change fixes 4 of 6 conflict resolution pipeline gaps, makes the merge path
> deterministic (model-independent), and enables automated e2e testing.

## Why This Is the Biggest Win

The investigation ([issue-tracking.md](./e2e/issue-tracking.md)) found 10 pre-existing issues
in the merge queue and conflict resolution pipeline. They cluster into two groups:

1. **Conflict resolution pipeline** (Issues 1, 2, 4, 5, 6, 13) — six compounding gaps
2. **LLM-dependent formula steps** (Issues 7, 8, 10, 12) — model-dependent behavior

Both groups share a root cause: the merge mechanics are prose instructions that Claude follows,
not deterministic code. The Engineer struct already has the code — `ProcessMRInfo()`, `doMerge()`,
`HandleMRInfoSuccess()`, `HandleMRInfoFailure()` — but it's dead (zero callers).

Wiring it into `gt refinery process-next` simultaneously:

| What it fixes | How |
|---------------|-----|
| Issue 1: Infinite retry / duplicate tasks | `HandleMRInfoFailure` calls `AddDependency(mr, task)` — blocks MR |
| Issue 2: Format incompatibility | `createConflictResolutionTaskForMR` writes structured `## Metadata` that the conflict-resolve formula expects |
| Issue 5: MERGE_FAILED silent on conflicts | `HandleMRInfoFailure` sends `MERGE_FAILED` mail for conflicts AND test failures |
| Issue 6: Merge slot not set up | `createConflictResolutionTaskForMR` calls `MergeSlotEnsureExists` + `MergeSlotAcquire` |
| Issues 7, 8: LLM-dependent merge sequence | Go code carries `MRInfo` through the pipeline — no values to forget |
| Testability | Deterministic code callable from Go tests — no Claude simulation needed |

**What it doesn't fix** (separate work):
- Issue 4: No auto-dispatch (needs witness/dispatcher changes)
- Issue 9: Sling formula routing (needs sling.go changes)
- Issue 13: Never exercised (fixed by writing e2e tests)

---

## Architecture

### Current: Formula drives merge

```
Formula (prose) → Claude (LLM) → git commands (shell)
                                → bd commands (shell)
                                → gt mail send (shell)
```

Claude must remember values across steps, construct commands correctly, execute
them in order, and handle all error paths. Model-dependent.

### Proposed: Command drives merge, formula orchestrates

```
Formula (prose) → Claude (LLM) → gt refinery process-next (Go)
                                      ├─ git operations (deterministic)
                                      ├─ bead updates (deterministic)
                                      ├─ mail notifications (deterministic)
                                      └─ conflict task creation (deterministic)
```

Claude's role shrinks to: "run `gt refinery process-next`, read output, decide
whether to continue patrol or hand off." The merge mechanics — the critical path
where bugs live — become deterministic code.

### Command interface

```bash
# Process the next ready MR in the queue
gt refinery process-next [rig-name]

# Output:
#   Processing MR gt-abc: polecat/Nux/gt-001 → main
#   Rebasing onto origin/main...
#   Running tests: go test ./...
#   Merging (ff-only) into main...
#   Pushing to origin...
#   Sending MERGED notification to witness...
#   Closing MR bead gt-abc...
#   Deleting branch polecat/Nux/gt-001...
#   Done: merged at abc1234

# On conflict:
#   Processing MR gt-abc: polecat/Nux/gt-001 → main
#   Rebasing onto origin/main...
#   CONFLICT: 3 files have conflicts
#   Creating conflict resolution task...
#   Blocking MR gt-abc on task gt-xyz...
#   Acquiring merge slot...
#   Sending MERGE_FAILED to witness...
#   Done: conflict — task gt-xyz created for resolution

# On test failure:
#   Processing MR gt-abc: polecat/Nux/gt-001 → main
#   Rebasing onto origin/main...
#   Running tests: go test ./...
#   FAILED: 2 test failures
#   Sending MERGE_FAILED to witness...
#   Reopening source issue gt-def...
#   Closing MR bead gt-abc (rejected)...
#   Done: rejected — test failures
```

Exit codes:
- 0: merged successfully
- 1: conflict (task created)
- 2: test failure (MR rejected)
- 3: queue empty (nothing to process)
- 4: error (infrastructure failure)

---

## Implementation Phases

### Phase 1: Add missing git operations

**File: `internal/git/git.go`**

Add `MergeFFOnly` method (doesn't exist yet):

```go
// MergeFFOnly performs a fast-forward-only merge. Returns error if not possible.
func (g *Git) MergeFFOnly(branch string) error {
    return g.run("merge", "--ff-only", branch)
}
```

Add configurable merge strategy to `MergeQueueConfig`:

```go
// MergeStrategy controls how branches are merged: "rebase-ff" (default) or "squash".
// "rebase-ff" matches the production formula: rebase onto target, then ff-only merge.
// "squash" is the original Engineer behavior: squash merge into single commit.
MergeStrategy string `json:"merge_strategy"`
```

Default: `"rebase-ff"` — matches what the production formula does.

**Modify `doMerge()`** to support both strategies:

```go
func (e *Engineer) doMerge(ctx context.Context, branch, target, sourceIssue string) ProcessResult {
    // ... existing setup (BranchExists, Checkout target, Pull) ...

    if e.config.MergeStrategy == "squash" {
        // Existing squash path
        e.git.CheckConflicts(branch, target)
        e.git.MergeSquash(branch, msg)
    } else {
        // Rebase + ff-only (matches production formula)
        e.git.Checkout(branch)
        err := e.git.Rebase("origin/" + target)
        if err != nil {
            e.git.AbortRebase()
            return ProcessResult{Conflict: true, Error: "rebase conflict"}
        }
        e.git.Checkout(target)
        e.git.MergeFFOnly(branch)
    }

    // ... existing Push ...
}
```

**Estimated scope**: ~40 lines in git.go, ~30 lines in engineer.go

### Phase 2: Create `gt refinery process-next` command

**File: `internal/cmd/refinery.go`**

Add new cobra command following the existing pattern:

```go
var refineryProcessNextCmd = &cobra.Command{
    Use:   "process-next [rig]",
    Short: "Process the next ready MR in the merge queue",
    Long: `Deterministically processes the next unclaimed, unblocked MR.

Performs: rebase → test → merge → push → notify → close bead.
On conflict: creates resolution task, blocks MR, notifies witness.
On test failure: rejects MR, notifies witness, reopens source issue.`,
    Args: cobra.MaximumNArgs(1),
    RunE: runRefineryProcessNext,
}
```

**Implementation of `runRefineryProcessNext`:**

```go
func runRefineryProcessNext(cmd *cobra.Command, args []string) error {
    // 1. Get rig (same pattern as runRefineryReady)
    rigName := inferRigOrArg(args)
    _, r, err := getRig(rigName)
    eng := refinery.NewEngineer(r)
    eng.LoadConfig()

    // 2. Find next MR
    ready, err := eng.ListReadyMRs()
    if len(ready) == 0 {
        fmt.Println("Queue empty")
        os.Exit(3)
    }
    mr := ready[0]

    // 3. Claim it
    eng.ClaimMR(mr.ID, workerID)
    defer eng.ReleaseMR(mr.ID, workerID)  // Release on any error

    // 4. Process it
    result := eng.ProcessMRInfo(ctx, mr)

    // 5. Handle result
    if result.Success {
        eng.HandleMRInfoSuccess(mr, result)
        // exit 0
    } else if result.Conflict {
        eng.HandleMRInfoFailure(mr, result)
        // exit 1
    } else {
        eng.HandleMRInfoFailure(mr, result)
        // exit 2
    }
}
```

**Key detail**: The Engineer already has `ProcessMRInfo`, `HandleMRInfoSuccess`, and
`HandleMRInfoFailure` fully implemented. This command is a thin CLI wrapper.

**Estimated scope**: ~80 lines in refinery.go

### Phase 3: Fix `HandleMRInfoSuccess` for integration branches

The current `HandleMRInfoSuccess` works but needs two adjustments for integration branches:

1. **Target branch from MR metadata**: The MR's target field may be `integration/foo`
   instead of `main`. The current code already reads `mr.Target` and passes it through
   `doMerge`, so this should work. Verify with a test.

2. **Don't close source issue for integration branch merges**: When merging to an
   integration branch, the source issue should stay open (it gets closed when the
   integration branch lands to main). Add a check:

```go
// In HandleMRInfoSuccess, after closing MR bead:
if mr.Target == e.config.TargetBranch {
    // Merging to main — close the source issue
    e.beads.CloseWithReason(closeReason, mr.SourceIssue)
} else {
    // Merging to integration branch — leave source issue open
    fmt.Fprintf(e.output, "[Engineer] Source issue %s left open (merged to integration branch %s)\n",
        mr.SourceIssue, mr.Target)
}
```

Wait — `TargetBranch` was removed by xexr. Need to use rig's `DefaultBranch()` instead:

```go
defaultBranch := e.rig.DefaultBranch()
if mr.Target == defaultBranch {
    // Merging to default branch — close source issue
} else {
    // Merging to integration branch — leave open
}
```

**Estimated scope**: ~15 lines in engineer.go

### Phase 4: Update formula to use the new command

**File: `internal/formula/formulas/mol-refinery-patrol.formula.toml`**

Replace the process-branch → run-tests → handle-failures → merge-push sequence with:

```toml
[[steps]]
id = "process-next"
title = "Process next merge request"
needs = ["queue-scan"]
description = """
Run the deterministic merge processor:

```bash
gt refinery process-next {{rig_name}}
```

Read the output and exit code:
- Exit 0: Merged successfully. Note the MR ID and commit SHA from output.
- Exit 1: Conflict detected. A conflict resolution task was created automatically.
  Note the task ID from output. Skip to loop-check.
- Exit 2: Test failure. The MR was rejected automatically.
  Note the failure reason from output. Skip to loop-check.
- Exit 3: Queue empty. Skip to check-integration-branches.
- Exit 4: Infrastructure error. Log the error, skip to loop-check.

The command handles all merge mechanics, notifications, and bead updates
deterministically. You do NOT need to run git commands, send mail, or
close beads yourself.

After exit 0: proceed to loop-check (target branch has moved, remaining
branches need rebasing on new baseline)."""
```

This replaces **four formula steps** (process-branch, run-tests, handle-failures,
merge-push) with **one command invocation**. Claude's job becomes reading output
and deciding what to do next — not executing the merge.

**The formula's inbox-check, queue-scan, loop-check, check-integration-branches,
and context-check steps remain unchanged.** Those are patrol orchestration concerns
that are appropriate for LLM control.

**Estimated scope**: ~30 lines replacing ~200 lines of formula prose

### Phase 5: E2E tests

With `gt refinery process-next`, the e2e tests become straightforward:

**File: `internal/refinery/engineer_integration_test.go`** (new, `//go:build integration`)

```go
func TestProcessNextMR_HappyPath(t *testing.T) {
    // Setup: bare repo, rig, engineer
    // Create polecat branch with commits, push
    // Create MR bead targeting main
    // Call: eng.ProcessMRInfo(ctx, mr) + eng.HandleMRInfoSuccess(mr, result)
    // Assert: commits on main, MR bead closed, branch deleted
}

func TestProcessNextMR_IntegrationBranch(t *testing.T) {
    // Setup: bare repo with integration/foo branch
    // Create polecat branch, MR bead targeting integration/foo
    // Process
    // Assert: commits on integration/foo (not main), MR closed, source issue OPEN
}

func TestProcessNextMR_Conflict(t *testing.T) {
    // Setup: bare repo, create conflicting changes on main and polecat branch
    // Create MR bead
    // Process
    // Assert: result.Conflict == true
    // Call: eng.HandleMRInfoFailure(mr, result)
    // Assert: conflict task created with structured ## Metadata
    // Assert: MR blocked on task (dependency)
    // Assert: merge slot acquired
    // Assert: MERGE_FAILED mail sent
}

func TestProcessNextMR_TestFailure(t *testing.T) {
    // Setup: configure TestCommand that will fail
    // Create MR bead
    // Process
    // Assert: result.TestsFailed == true
    // Assert: MR rejected, source issue reopened
}
```

**File: `internal/e2e/integration_branch_test.go`** (new, `//go:build e2e`)

Full lifecycle using subprocess calls:

```go
func TestIntegrationBranchLifecycle(t *testing.T) {
    // 1. gt mq integration create <epic>
    // 2. Simulate polecat work (git branch, commit, push)
    // 3. Create MR bead (target: integration/<name>)
    // 4. gt refinery process-next  ← DETERMINISTIC, not simulated git
    // 5. Assert: integration branch has the commit
    // 6. gt mq integration land <epic>
    // 7. Assert: main has the commit, integration branch deleted
}

func TestIntegrationBranchConflict(t *testing.T) {
    // 1. Create integration branch
    // 2. Create two polecat branches that conflict
    // 3. Process first MR → success
    // 4. Process second MR → conflict
    // 5. Assert: conflict task created, MR blocked
    // 6. Simulate resolution (rebase, force push)
    // 7. Close conflict task
    // 8. Process second MR again → success
}
```

**Estimated scope**: ~300 lines across two test files

---

## Execution Order

```
Phase 1: git.MergeFFOnly + configurable merge strategy     (~70 lines)
    ↓
Phase 2: gt refinery process-next command                   (~80 lines)
    ↓
Phase 3: Integration branch handling in HandleMRInfoSuccess (~15 lines)
    ↓
Phase 4: Update formula to use new command                  (~30 lines, -200 lines)
    ↓
Phase 5: E2E tests                                          (~300 lines)
```

Total new code: ~500 lines
Total removed formula prose: ~200 lines
Net: ~300 lines added

Each phase is independently committable and testable. Phase 2 is the keystone —
everything before it is preparation, everything after it builds on it.

---

## Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| Merge strategy mismatch (squash vs rebase-ff) changes commit history | Default to rebase-ff (matching formula). Squash is opt-in config. |
| `ListReadyMRs` uses `ReadyWithType("merge-request")` — same Type vs Label issue? | Verify `ReadyWithType` goes through the compat shim. If not, fix to use Label. |
| Engineer's `doMerge` target defaults to config `TargetBranch` (removed by xexr) | Use `mr.Target` from MR metadata. Fall back to `rig.DefaultBranch()`. |
| Existing formula-based refineries need migration | Phase 4 is optional — old formula still works. New command is additive. |
| `HandleMRInfoSuccess` closes source issue unconditionally | Phase 3 adds integration branch check — only close when merging to default branch. |
| Mail router not initialized in CLI context | Check if `NewEngineer` sets up `e.router`. If nil, initialize from rig config. |

---

## What Changes for Users

**For formula-driven refineries** (current):
- Nothing breaks. The formula continues to work as-is.
- When Phase 4 is deployed, the formula calls `gt refinery process-next` instead of raw git.
- Claude's role shifts from "execute merge" to "decide when to merge."

**For e2e tests** (new):
- Full lifecycle testable without Claude, tmux, or daemon.
- `gt refinery process-next` is a subprocess call, same as `gt mq integration create/land`.

**For operators**:
- `gt refinery process-next` can be run manually to process a stuck MR.
- Conflict resolution tasks have structured metadata and block the MR.
- MERGE_FAILED notifications work for conflicts, not just test failures.

---

## Success Criteria

- [ ] `gt refinery process-next` processes an MR and merges to main (exit 0)
- [ ] `gt refinery process-next` processes an MR and merges to integration branch (exit 0)
- [ ] Conflict creates task with `## Metadata` format, blocks MR, acquires slot (exit 1)
- [ ] Test failure rejects MR, notifies witness, reopens issue (exit 2)
- [ ] Empty queue returns exit 3
- [ ] E2E test covers full lifecycle: create → work → process-next → land
- [ ] E2E test covers conflict: process → conflict → resolve → process again
- [ ] Formula updated to call command instead of raw git (optional, can defer)
- [ ] All existing tests pass (`go test ./...`)
