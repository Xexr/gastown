# Implementation Review: Integration Branch Enhancement (PR #1226)

**Date:** 2026-02-12
**Reviewers:** Opus 4.6, GPT 5.3 Codex (Gemini 3 Pro: failed — rate limit)
**Branch:** `feat/integration-branch-enhancement`
**Commit:** `96a970b2`

## Review Configuration
- **Spec Reviewed:** PR #1226 description, `docs/concepts/integration-branches.md`
- **Implementation Reviewed:** 50+ files across `internal/beads/`, `internal/cmd/`, `internal/config/`, `internal/git/`, `internal/rig/`, `internal/polecat/`, `internal/refinery/`, `internal/doctor/`, `.beads/formulas/`, `docs/`
- **Categories Checked:** All (completeness, quality, scope, standards)

## Completion Matrix

| # | Requirement | Opus | GPT | Consensus | Location |
|---|------------|------|-----|-----------|----------|
| 1 | Polecat integration branch awareness | DONE | DONE | DONE | `polecat_spawn.go`, `polecat/manager.go` |
| 2 | Sling `--base-branch` flag | DONE | DONE | DONE | `sling.go:117,139,271` |
| 3 | Land respects base branch + worktree + flock | DONE | DONE | DONE | `mq_integration.go:105-148,419-422` |
| 4 | Detection improvements (metadata-first, remote-first) | DONE | DONE | DONE | `beads/integration.go:171-214` |
| 5 | `{title}` template + sanitization + collision | DONE | DONE | DONE | `beads/integration.go:34-87`, `mq_integration.go:176` |
| 6 | Configurable branch naming precedence | DONE | DONE | DONE | `mq_integration.go:217-233` |
| 7 | Metadata-first resolution (mq list/submit --epic) | DONE | DONE | DONE | `mq_integration.go:199-213`, `mq_list.go`, `mq_submit.go` |
| 8 | Refinery patrol config + auto-land step | DONE | DONE | DONE | `prime_molecule.go`, `mol-refinery-patrol.formula.toml` |
| 9 | Rig-level config (4 fields, pointer semantics) | DONE | DEVIATED | DONE | `config/types.go` (template is `string` not `*string` — outcome works) |
| 10 | Formula updates (`{{base_branch}}`) | DONE | DONE | DONE | `mol-polecat-work.formula.toml`, `mol-polecat-conflict-resolve.formula.toml` |
| 11 | Per-rig build pipeline (5 commands) | DONE | DONE | DONE | `sling_helpers.go`, `prime_molecule.go`, formulas |
| 12 | `target_branch` -> `default_branch` consolidation | DONE | DONE | DONE | `config/loader.go:294`, `prime_molecule.go:325` |
| 13 | `default_branch` validation at spawn | DONE | DONE | DONE | `polecat/manager.go:617-627` |
| 14 | `default_branch` validation at `gt rig add` | DONE | DONE | DONE | `rig/manager.go:370-376` |
| 15 | `gt doctor` check for `default_branch` | DONE | DONE | DONE | `doctor/rig_check.go:1177-1274` |
| 16 | `target_branch` injected without MQ settings | DONE | DONE | DONE | `prime_molecule.go:325-340` |
| 17 | MQ integration commands respect `default_branch` | DONE | PARTIAL | **REVIEW** | `create`/`land` respect it; `status` hardcodes `"main"` |
| 18 | Land race hardening (flock) | DONE | DONE | DONE | `mq_integration.go:105-148` |
| 19 | Land crash recovery (idempotency) | DONE | DONE | DONE | `mq_integration.go:492-501` |
| 20 | Deprecated field migration warning | DONE | DONE | DONE | `config/loader.go:286-309` |
| 21 | Vestigial Engineer flags removed | DONE | DONE | DONE | `refinery/engineer.go:27-31` |
| 22 | Dead `resetHard` removed | DONE | DONE | DONE | Verified by search |

## Summary Stats
- **Total Requirements:** 22
- **Complete (both agree):** 20 (91%)
- **Needs Review:** 1 (status hardcodes "main")
- **Informational Deviations:** 1 (template field is `string` not `*string`)
- **Missing:** 0
- **Scope Creep Items:** 0

## Issues (Risk-Weighted)

### HIGH

#### 1. `mq integration status` uses hardcoded `"main"` instead of base branch
- **Found by:** GPT (Opus missed)
- **Verified:** Yes — `mq_integration.go:724` calls `CommitsAhead("main", ref)`
- **Impact:** For rigs with `default_branch != "main"`, status shows incorrect ahead count and ready-to-land
- **Root cause:** `runMqIntegrationStatus` doesn't read `base_branch` from epic metadata or rig `default_branch`
- **Fix pattern:** Same as `land` at lines 419-422: `beads.GetBaseBranchField(epic.Description)` with `r.DefaultBranch()` fallback
- **Also affects:** `IntegrationStatusOutput.AheadOfMain` field name (JSON: `ahead_of_main`) and print message "Ahead of main:"

#### 2. Non-`main` default branch lost when injecting `base_branch` into polecat formulas
- **Found by:** GPT (Opus missed)
- **Verified:** Yes — traced full code path
- **Flow:**
  1. `polecat/manager.go:610-614` correctly creates worktree from `origin/<default_branch>`
  2. `polecat_spawn.go:221-222` falls back to `"main"` when `baseBranch` is empty (should use rig `default_branch`)
  3. `sling.go:272` skips injection when `BaseBranch == "main"` (assumes formula default handles it)
  4. Formula gets `{{base_branch}}` = `"main"` (default) — **wrong** for non-main rigs
- **Impact:** Polecat worktree is created from correct branch, but formula instructions (rebase, fetch) reference `main`
- **Fix:** In `polecat_spawn.go:221-222`, replace `effectiveBranch = "main"` with `effectiveBranch = r.DefaultBranch()`

### MEDIUM

#### 3. `RemoteBranchExists` calls `ls-remote` twice
- **Found by:** Opus
- **Verified:** Yes — `git.go:703-713` runs identical `ls-remote` command twice, first result discarded
- **Fix:** Remove lines 704-707, use single call

#### 4. Critical land/create paths lightly tested
- **Found by:** Both agents
- **Impact:** No direct unit tests for `runMqIntegrationCreate`, `runMqIntegrationLand`, lock behavior, or idempotency flow
- **Mitigation:** Core detection logic in `beads/` has excellent coverage. E2E integration tests cover the full path.
- **Recommendation:** Future-scoped. These functions involve significant I/O (git, beads DB).

### LOW / INFORMATIONAL

#### 5. `detectIntegrationBranch` wrapper in `mq_submit.go` unused
- **Found by:** Opus
- **Location:** `mq_submit.go:237-240` — wrapper annotated "for backward compatibility" but `runMqSubmit` calls `beads.DetectIntegrationBranch` directly at line 150
- **Recommendation:** Remove dead code

#### 6. Config/docs semantics drift around template field
- **Found by:** GPT
- **Detail:** `integration_branch_template` is `string` (not `*string`), though spec says "pointer semantics" for all 4 fields
- **Impact:** None — empty string correctly means "use default"
- **Recommendation:** Informational only

#### 7. Formula version discrepancy
- **Found by:** Opus (informational)
- **Detail:** `mol-polecat-conflict-resolve.formula.toml` is v2 vs v5 for work formula
- **Impact:** None — expected, different evolution rates

## Agent Comparison

| # | Issue | Opus | GPT | Agree? |
|---|-------|------|-----|--------|
| 1 | `status` hardcodes "main" | Missed | HIGH | **GPT only** |
| 2 | BaseBranch lost in spawn info | Missed | HIGH | **GPT only** |
| 3 | Double ls-remote call | MEDIUM | Missed | **Opus only** |
| 4 | Light cmd/ test coverage | LOW | MEDIUM | **Both** |
| 5 | Unused wrapper in mq_submit | LOW | Missed | **Opus only** |
| 6 | Template field string vs ptr | Not flagged | LOW | **GPT only** |
| 7 | Formula version difference | LOW | Not flagged | **Opus only** |

## Reasoning

**Issue #1:** GPT correctly identified that `status` hardcodes "main" while `create` and `land` both properly read base branch from metadata. This is a genuine gap — the command would show incorrect counts for non-main default branches. Opus marked requirement H1 as DONE ("default_branch used everywhere") without catching this specific function.

**Issue #2:** GPT traced a cross-function data flow that Opus missed. The worktree creation (in `manager.go`) correctly uses rig `default_branch`, but the return value (in `polecat_spawn.go`) falls back to "main", and the formula var injection (in `sling.go`) relies on that return value. This breaks the formula for non-main rigs even though the worktree itself is correct.

**Issue #3:** Minor efficiency concern Opus caught. Valid but low impact.

## Test Coverage Assessment

| Area | Coverage | Notes |
|------|----------|-------|
| `beads/integration.go` | Excellent | 12 detection, 13 sanitize, 7 naming, metadata ops |
| `cmd/mq_integration_test.go` | Good | `filterMRsByTarget`, MR field parsing, template resolution |
| `cmd/patrol_helpers_test.go` | Good | Refinery patrol vars including `DefaultBranchWithoutMQ` |
| `git/git_test.go` | Good | RefExists tests (valid, invalid, origin refs) |
| `doctor/rig_check_test.go` | Good | 5 tests for DefaultBranchExistsCheck |
| `cmd/` create/land/status | Light | No direct unit tests; covered by E2E integration tests |

## Architecture Notes

1. **Detection in `beads/` package (pure logic):** Uses `IssueShower` and `BranchChecker` interfaces to avoid circular deps
2. **Engineer is target-agnostic by design:** Merges whatever target the MR specifies; gating at MR creation time
3. **Two formula injection paths:** Polecats via `sling.go`/`sling_helpers.go`, Refinery via `prime_molecule.go`
4. **`default_branch` from rig config.json:** `Rig.DefaultBranch()` with "main" fallback; all consumers use this method
