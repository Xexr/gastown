# E2E Investigation: Issue Tracking

> Issues identified during the [e2e integration branch investigation](./initial-e2e-investigation.md), sorted by severity.
> See investigation doc Sections 2-4 for full analysis and evidence.
>
> **Cross-referenced against PR #1226** (xexr, `feat/integration-branch-enhancement`) on 2026-02-13.
> Julian's manual testing found 4 bugs; xexr's 28/28 test run confirmed all fixed.

---

## PR #1226 Resolution Status

Of the 17 issues identified, **4 were already fixed by xexr** in the current PR, **3 were improved**, and **10 are pre-existing architectural gaps** not caused by or specific to this PR.

### Fixed by xexr in PR #1226

| # | Issue | Fix | Evidence |
|---|-------|-----|----------|
| 3 | `gt mq integration status` reports 0 MRs | `Label: "gt:merge-request"` replaces `Type: "merge-request"` in both `findOpenMRsForIntegration` and `runMqIntegrationStatus` | Julian's Bug 1; confirmed fixed in 28/28 test run |
| 11 | `auto_land` FORBIDDEN enforcement LLM-only | Pre-push hook added with deterministic integration branch ancestry detection. Blocks pushes to default branch containing integration content unless `GT_INTEGRATION_LAND=1`. Formula also has FORBIDDEN directives. | Julian's Bug 3; `.githooks/pre-push` +39 lines |
| 16 | `makeTestMR` creates unrealistic beads | `makeTestMR` now creates `Type: "task"` with `Labels: ["gt:merge-request"]`. Mock `List` has label filtering. Regression tests `TestMakeTestMR_RealisticFields` and `TestMockBeadsList_LabelFilter` added. | `mq_testutil_test.go`, `mq_integration_test.go` (+911 lines) |
| 17 | FORBIDDEN directives untestable in Go | `.githooks/pre-push_test.sh` (221 lines) tests the deterministic pre-push guardrail. The LLM-level FORBIDDEN is defense-in-depth; the hook is the enforceable layer, and it's now tested. | `.githooks/pre-push_test.sh` (new file) |

Also fixed by xexr (Julian's bugs not in our original list):
- **Bug 2**: Epic auto-closed before land → `runMqIntegrationLand` now handles `epicAlreadyClosed` gracefully
- **Bug 4**: Duplicate `gt mq integration create` → checks epic metadata for existing branch, `resolveUniqueBranchName` disambiguates collisions with numeric suffix

### Improved by xexr (not fully resolved)

| # | Issue | Improvement | Remaining gap |
|---|-------|-------------|---------------|
| 7 | LLM-dependent merge-push sequence | Formulas parameterized with `{{target_branch}}` — explicit variable instead of hardcoded `main` | Claude still must execute multi-step sequence correctly |
| 8 | LLM-dependent branch substitution | `{{target_branch}}` and `{{base_branch}}` variables reduce ambiguity | Still prose-driven, not deterministic code |
| 12 | LLM-dependent inbox parsing | Same parameterization improvements | Claude still must remember values across steps |

### Pre-existing — not caused by or specific to PR #1226

These issues exist independently of the integration branch feature. They affect the entire merge queue and conflict resolution pipeline. They should be tracked as backlog items, not blockers for PR #1226.

| # | Severity | Issue | Why pre-existing |
|---|----------|-------|-----------------|
| 1 | **P0** | Infinite retry loop / duplicate conflict tasks | Formula conflict handling predates this PR. Not changed by xexr. |
| 2 | **P0** | Format incompatibility (creation vs consumption) | Both formulas predate this PR. Conflict-resolve updated for `{{base_branch}}` but format gap remains. |
| 4 | **P1** | No agent auto-dispatches conflict tasks | Architectural gap in all patrol formulas. No dispatcher role exists. |
| 5 | **P1** | MERGE_FAILED protocol silent on conflicts | Pre-existing protocol — handle-failures step only covers test failures. |
| 6 | **P1** | Merge slot setup only in dead code | Engineer's `createConflictResolutionTaskForMR` was dead code before this PR and remains so. |
| 9 | **P2** | `gt sling` auto-applies wrong formula | Pre-existing auto-apply logic (Issue #288). Sling gained `--base-branch` flag but routing unchanged. |
| 10 | **P2** | LLM-dependent test failure diagnosis | General refinery formula concern, not integration-specific. |
| 13 | **P2** | Conflict resolution pipeline never exercised | Still true — zero conflict tasks in beads DB. Not a bug this PR can fix. |
| 14 | **P2** | Merge strategy divergence (formula vs Engineer) | Engineer simplified by removing `TargetBranch`/`IntegrationBranches` config, but squash vs rebase+ff-only remains. |
| 15 | **P3** | Engineer merge methods are dead code | ~400 lines with zero callers. Pre-existing. Addresses Issues 1, 2, 5, 6 if wired in. |

---

## Full Issue Table (All 17)

| # | Severity | Status | Issue | Impact | Affected Code |
|---|----------|--------|-------|--------|---------------|
| 1 | **P0** | Pre-existing | **Infinite retry loop / duplicate conflict tasks** — Formula doesn't block MR on conflict task. Each patrol cycle (30s) retries the same branch, creates another duplicate task. | Unbounded bead pollution, wasted compute | `mol-refinery-patrol.formula.toml` (process-branch) |
| 2 | **P0** | Pre-existing | **Format incompatibility between conflict task creation and consumption** — Refinery formula creates prose metadata. Conflict-resolve formula expects structured `## Metadata`. | Conflict resolution polecat fails at load-task | `mol-refinery-patrol.formula.toml` ↔ `mol-polecat-conflict-resolve.formula.toml` |
| 3 | ~~P0~~ | **Fixed** | ~~`gt mq integration status` reports 0 MRs~~ — Fixed: queries by Label instead of Type. | ~~Non-functional~~ | `mq_integration.go` |
| 4 | **P1** | Pre-existing | **No agent auto-dispatches conflict tasks** — Task sits in `bd ready` indefinitely. Manual `gt sling` with explicit formula required. | Conflict resolution requires human intervention | All patrol formulas |
| 5 | **P1** | Pre-existing | **MERGE_FAILED protocol silent on conflicts** — No mail notification for conflicts. Only test failures send MERGE_FAILED. | No agent aware of conflicts | `mol-refinery-patrol.formula.toml` (handle-failures) |
| 6 | **P1** | Pre-existing | **Merge slot setup only in dead code** — Slot creation only in dead `createConflictResolutionTaskForMR()`. | Conflict-resolve polecat may error | `engineer.go:702-724` |
| 7 | **P1** | Improved | **LLM-dependent merge-push sequence** — Multi-step sequence Claude must execute correctly. Now parameterized with `{{target_branch}}`. | Silent lifecycle breakage if steps dropped | `mol-refinery-patrol.formula.toml` (merge-push) |
| 8 | **P1** | Improved | **LLM-dependent branch substitution** — Now uses `{{target_branch}}` / `{{base_branch}}` variables. Reduces but doesn't eliminate LLM dependency. | Wrong code merged if substitution fails | `mol-refinery-patrol.formula.toml` (process-branch) |
| 9 | **P2** | Pre-existing | **`gt sling` auto-applies wrong formula for conflict tasks** — Auto-applies `mol-polecat-work` for bare beads. Matters when automated dispatch is added. | Wrong workflow for conflict tasks | `sling.go:488-494` |
| 10 | **P2** | Pre-existing | **LLM-dependent test failure diagnosis** — Claude decides if failure is branch regression vs pre-existing. | Merges broken code or rejects good code | `mol-refinery-patrol.formula.toml` (handle-failures) |
| 11 | ~~P2~~ | **Fixed** | ~~`auto_land` FORBIDDEN enforcement LLM-only~~ — Fixed: pre-push hook provides deterministic enforcement with ancestry detection. | ~~Bypasses human review gate~~ | `.githooks/pre-push` |
| 12 | **P2** | Improved | **LLM-dependent inbox parsing** — Claude must remember values across steps. Parameterization helps but doesn't eliminate dependency. | Polecat worktrees accumulate | `mol-refinery-patrol.formula.toml` (inbox-check) |
| 13 | **P2** | Pre-existing | **Conflict resolution pipeline never exercised** — Zero conflict tasks in beads DB. All gaps latent. | Issues undiscoverable until first conflict | Entire conflict pipeline |
| 14 | **P2** | Pre-existing | **Merge strategy divergence** — Formula: rebase+ff-only. Engineer: squash. Different histories. | Unexpected changes if switching paths | `engineer.go:doMerge()` vs formula |
| 15 | **P3** | Pre-existing | **Engineer merge methods are dead code** — ~400 lines, zero callers. Addresses Issues 1, 2, 5, 6 if wired in. | Maintenance burden | `engineer.go:264-820` |
| 16 | ~~P3~~ | **Fixed** | ~~`makeTestMR` creates unrealistic beads~~ — Fixed: uses `Type: "task"` with labels. Regression tests added. | ~~False test confidence~~ | `mq_testutil_test.go` |
| 17 | ~~P3~~ | **Fixed** | ~~FORBIDDEN directives untestable~~ — Fixed: pre-push hook tested via `.githooks/pre-push_test.sh`. | ~~No regression protection~~ | `.githooks/pre-push_test.sh` |

---

## Summary

| Category | Count | Issues |
|----------|-------|--------|
| **Fixed by xexr** | 4 | #3, #11, #16, #17 |
| **Improved by xexr** | 3 | #7, #8, #12 |
| **Pre-existing (backlog)** | 10 | #1, #2, #4, #5, #6, #9, #10, #13, #14, #15 |

**PR #1226 is not blocked by the pre-existing issues.** The integration branch feature works end-to-end for the happy path (create → work → merge → land). The pre-existing issues affect the conflict resolution pipeline which exists independently and has never been exercised in production.

---

## Backlog: Conflict Resolution Pipeline

The 10 pre-existing issues cluster into two groups:

### Conflict resolution pipeline (Issues 1, 2, 4, 5, 6, 13)

Six compounding gaps that prevent end-to-end conflict resolution. Even fixing any single gap, the others prevent success:

```
Issue 1 (duplicate tasks) ← needs Issue 4 (dispatch) to be fixed, but
Issue 4 (dispatch)        ← needs Issue 2 (format) to be fixed, but
Issue 2 (format)          ← needs Issue 6 (merge slot) to be fixed
Issue 5 (no notification) ← independent, but same pipeline
Issue 13 (never tested)   ← all of the above are latent because of this
```

**Highest-leverage fix**: Wire the Engineer's dead code (Issue 15) into a production command (`gt refinery process-next`). This closes Issues 1, 2, 5, and 6 simultaneously and converts LLM-dependent merge mechanics (Issues 7, 8) into deterministic code.

### LLM reliability (Issues 7, 8, 9, 10, 12, 14)

Formula steps where behavior is model-dependent. Improved by xexr's parameterization but not eliminated. Long-term fix is the same: move merge mechanics from LLM prose to deterministic Go code.
