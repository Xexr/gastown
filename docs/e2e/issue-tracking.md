# E2E Investigation: Issue Tracking

> Issues identified during the [e2e integration branch investigation](./initial-e2e-investigation.md), sorted by severity.
> See investigation doc Sections 2-4 for full analysis and evidence.

---

## Issue Table

| # | Severity | Issue | Impact | Affected Code |
|---|----------|-------|--------|---------------|
| 1 | **P0** | **Infinite retry loop / duplicate conflict tasks** — Formula doesn't block MR on conflict task. Each patrol cycle (30s) retries the same branch, creates another duplicate task. After N cycles: N identical tasks, MR still stuck. | Unbounded bead pollution, wasted compute, confusing `bd ready` output | `mol-refinery-patrol.formula.toml` (process-branch step) |
| 2 | **P0** | **Format incompatibility between conflict task creation and consumption** — Refinery formula creates prose metadata (`## Conflict Resolution Required`, no list markers). Conflict-resolve formula expects structured `## Metadata` with `- Field: value` syntax. Polecat can't extract metadata it needs. | Conflict resolution polecat fails at step 1 (load-task), entire resolution fails | `mol-refinery-patrol.formula.toml` ↔ `mol-polecat-conflict-resolve.formula.toml` |
| 3 | **P0** | **`gt mq integration status` reports 0 MRs** — Queries with `Type: "merge-request"` but MR beads have `Type: "task"` with label `gt:merge-request`. Julian confirmed 0 MRs in manual testing. | Integration status command is non-functional | `mq_integration.go:662, 797` |
| 4 | **P1** | **No agent auto-dispatches conflict tasks** — Conflict task sits in `bd ready` indefinitely. No witness, deacon, boot, or refinery agent scans for unassigned conflict work. Manual `gt sling` with explicit formula required. | Conflict resolution never starts without human intervention — breaks automated pipeline | All patrol formulas (witness, deacon, boot) |
| 5 | **P1** | **MERGE_FAILED protocol silent on conflicts** — Formula sends MERGE_FAILED for test failures only. Conflicts create a task silently — no mail notification to any agent. Even if witness had a conflict-dispatch handler, it would never fire. | No agent is aware a conflict occurred | `mol-refinery-patrol.formula.toml` (handle-failures step) |
| 6 | **P1** | **Merge slot setup only in dead code** — `mol-polecat-conflict-resolve` expects merge slot (`bd merge-slot acquire --wait`). Slot creation (`MergeSlotEnsureExists`) only exists in Engineer's dead `createConflictResolutionTaskForMR()`. Unknown if `bd merge-slot acquire` auto-creates. | Conflict-resolve polecat may error on slot acquisition, or skip serialization | `engineer.go:702-724` (dead), `mol-polecat-conflict-resolve.formula.toml` (step 2) |
| 7 | **P1** | **LLM-dependent merge-push sequence** — Claude must remember polecat name, verify SHA, send MERGED mail, close MR bead, archive message — all in sequence. Any dropped step causes silent lifecycle breakage. | Orphaned worktrees, orphaned beads, inbox bloat — all silent failures | `mol-refinery-patrol.formula.toml` (merge-push step) |
| 8 | **P1** | **LLM-dependent branch substitution** — Claude must substitute correct branch names into git commands from prose instructions. Wrong branch → merges wrong code or loses work. | Wrong code merged to main or integration branch | `mol-refinery-patrol.formula.toml` (process-branch step) |
| 9 | **P2** | **`gt sling` auto-applies wrong formula for conflict tasks** — Lines 488-494 auto-apply `mol-polecat-work` for any bare bead slung to polecats. No detection of conflict tasks by title prefix. Matters when/if automated dispatch is added. | Conflict task treated as generic work — wrong workflow, no merge-slot, wrong merge path | `sling.go:488-494` |
| 10 | **P2** | **LLM-dependent test failure diagnosis** — Claude must determine if test failure is a branch regression vs pre-existing on target. Wrong diagnosis → merges broken code OR rejects good code. | Broken code lands on main, or valid work blocked indefinitely | `mol-refinery-patrol.formula.toml` (handle-failures step) |
| 11 | **P2** | **LLM-dependent `auto_land` FORBIDDEN enforcement** — Formula relies on Claude respecting FORBIDDEN directive against landing integration branches when `auto_land=false`. No code enforcement except pre-push hook. | Integration branch landed autonomously when it shouldn't be — bypasses human review gate | `mol-refinery-patrol.formula.toml` (check-integration-branches step) |
| 12 | **P2** | **LLM-dependent inbox parsing** — Claude must parse MERGE_READY mail and remember branch, issue, polecat name, MR bead ID across multiple later steps. Forgetting polecat name → MERGED notification fails. | Polecat worktrees accumulate indefinitely (no MERGED → witness never nukes) | `mol-refinery-patrol.formula.toml` (inbox-check step) |
| 13 | **P2** | **Conflict resolution pipeline never exercised** — Zero conflict tasks, zero MR beads, empty merge queue in beads DB. Entire conflict pathway is theoretical. All gaps above are latent. | All conflict-related issues undiscoverable until first real conflict | Entire conflict pipeline |
| 14 | **P2** | **Merge strategy divergence** — Formula uses rebase + `merge --ff-only` (linear history). Engineer uses `merge --squash` (single commit). Different commit histories on same inputs. If Engineer is ever wired in, behavior changes. | Unexpected history changes if switching from formula to Engineer path | `engineer.go:doMerge()` vs formula process-branch step |
| 15 | **P3** | **Engineer merge methods are dead code** — `ProcessMR()`, `ProcessMRInfo()`, `doMerge()`, `handleSuccess()`, `HandleMRInfoSuccess()`, `HandleMRInfoFailure()` have zero callers. ~400 lines of untested, unused code. | Code maintenance burden; misleading to contributors who think it's used | `engineer.go:264-820` |
| 16 | **P3** | **Test mock `makeTestMR` creates unrealistic beads** — Uses `Type: "merge-request"` instead of `Type: "task"` with `Labels: ["gt:merge-request"]`. Mock `List` filters on `issue.Type` without compat shim. Tests pass against wrong data model. | Tests give false confidence — pass with mock but fail in production | `mq_testutil_test.go` |
| 17 | **P3** | **Formula FORBIDDEN directives untestable in Go** — Claude-level guardrails can't be validated by automated tests. Only the pre-push hook is a code-enforceable guardrail. | No automated regression protection for landing restrictions | `mol-refinery-patrol.formula.toml` |

---

## Summary by Severity

| Severity | Count | Theme |
|----------|-------|-------|
| **P0** | 3 | Active bugs: duplicate tasks, format mismatch, broken status command |
| **P1** | 5 | Missing automation: no dispatch, no notification, no slot setup, LLM-dependent critical path |
| **P2** | 5 | Latent risks: wrong formula routing, LLM-dependent decisions, untested pipeline, strategy divergence |
| **P3** | 3 | Technical debt: dead code, unrealistic test data, untestable guardrails |

---

## Relationship Between Issues

Issues 1-6 and 13 are all part of the conflict resolution pipeline and compound on each other. Even fixing any single gap, the others prevent end-to-end success:

```
Issue 1 (duplicate tasks) ← needs Issue 4 (dispatch) to be fixed, but
Issue 4 (dispatch)        ← needs Issue 2 (format) to be fixed, but
Issue 2 (format)          ← needs Issue 6 (merge slot) to be fixed
Issue 5 (no notification) ← independent, but same pipeline
Issue 13 (never tested)   ← all of the above are latent because of this
```

**Highest-leverage fix**: The Engineer's dead code (Issue 15) addresses Issues 1, 2, 5, and 6 simultaneously. Wiring it into a production command (`gt refinery process-next`) closes 4 of 6 conflict pipeline gaps and converts LLM-dependent merge mechanics (Issues 7, 8) into deterministic code.

**Independent fix**: Issue 3 (`gt mq integration status` reports 0 MRs) has a straightforward fix — query by Label instead of Type. See [plan](../../.claude/plans/crispy-petting-flute.md).
