# PR #1226 Review Tracking

**PR**: [feat: make integration branches end-to-end across the pipeline](https://github.com/steveyegge/gastown/pull/1226)
**Author**: Xexr
**Upstream**: steveyegge/gastown
**Created**: Single source of truth for all review findings, fork PRs, and remaining work.

---

## Current Status

| Item | Value |
|------|-------|
| **Upstream sync** | Complete as of 2026-02-13 |
| **upstream/main HEAD** | `ed084c08` |
| **PR branch HEAD** | `e635931f` (15 commits on upstream/main: 1 original + 5 cherry-picked fork PRs + 9 fixes) |
| **Main cherry-pick** | `27961dfd` (cherry-pick of original commit) |
| **origin/main HEAD** | `27961dfd` (upstream + 1 cherry-pick) |
| **CI** | All checks passing (lint, test, integration, Windows CI, embedded formulas, coverage) |
| **Build** | `make build` clean, `go test ./...` all pass on both branches |
| **Formula sync** | #1372 wisp hooking preserved (was accidentally reverted in earlier versions, fixed by rebase) |
| **PR state** | OPEN, not yet approved. Two `request_changes` reviews from @julianknutsen's automated pipeline. |
| **Fork PRs folded** | #3, #4, #5, #6, #7 cherry-picked onto PR branch. #8 deferred (Draft). All closed with review comments. |
| **Pending before squash** | R4-7 (`.land-worktree/` doctor check), Q2 answer, MT-3 (blocked on infra) |

---

## Master Finding Tracker

### Round 1: Single-model automated review (Opus 4.6) -- from @julianknutsen

| # | Finding | Severity | Status | Notes |
|---|---------|----------|--------|-------|
| R1-1 | Land worktree race on fixed path | Blocker | **Fixed** | Added `gofrs/flock` to `createLandWorktree`, matching existing `lockPolecat()` pattern |
| R1-2 | Formula TOML defaults `"none"` instead of `""` | Major | **Disagree** | Changing to `""` breaks `bd mol wisp create` -- beads treats `default=""` as "no default" -> required -> error. `"none"` is the correct workaround; proper fix belongs in beads codebase |
| R1-3 | Removed config fields with no migration warning | Major | **Fixed** | Added `warnDeprecatedMergeQueueKeys()` in `LoadRigSettings`. Warns on stderr if raw JSON contains `target_branch` or `integration_branches` |
| R1-4 | `IntegrationBranch*Enabled` not checked in Engineer | Major | **Fixed** | Removed vestigial fields from Engineer's struct -- gating is at MR creation time, not merge time. Added architectural comment |
| R1-5 | Land crash leaves inconsistent state | Major | **Fixed** | Added idempotency check: if integration branch HEAD is ancestor of target, skip to cleanup |
| R1-6 | Dead `resetHard` function | Minor | **Fixed** | Removed |
| R1-7 | PR #1299 dependency | Minor | **Resolved** | PR #1299 now merged upstream |

### Round 2: Multi-model review (Opus 4.6 + GPT 5.3 Codex) -- our own review

| # | Finding | Severity | Found by | Status | Notes |
|---|---------|----------|----------|--------|-------|
| R2-1 | `mq integration status` hardcodes `"main"` | HIGH | GPT | **Fixed** | Added `baseBranch` resolution. Renamed `AheadOfMain` -> `AheadOfBase`. Print messages parameterized. |
| R2-2 | Non-`main` default branch lost in polecat spawn | HIGH | GPT | **Fixed** | `polecat_spawn.go:222` -- changed `effectiveBranch = "main"` to `effectiveBranch = r.DefaultBranch()` |
| R2-3 | `RemoteBranchExists` calls `ls-remote` twice | MEDIUM | Opus | **Fixed** | Removed duplicate first call |
| R2-4 | Light `cmd/` test coverage for create/land/status | MEDIUM | Both | **Accepted** | E2E integration tests cover full paths. Future-scoped. |
| R2-5 | Dead `detectIntegrationBranch` wrapper | LOW | Opus | **Fixed** | Removed unused wrapper in `mq_submit.go` |
| R2-6 | Template field `string` vs `*string` | LOW | GPT | **Accepted** | Empty string unambiguously means "use default" -- correct Go convention |
| R2-7 | Formula version discrepancy (v2 vs v5) | LOW | Opus | **Accepted** | Different formulas evolve at different rates. Versions independent. |

### Round 3: Dual-model automated review (Claude + Codex) -- from @julianknutsen, request_changes

| # | Finding | Severity | Status | Notes |
|---|---------|----------|--------|-------|
| R3-1 | Default template `{epic}` -> `{title}` breaks existing branches | Major | **Fixed** | Added `LegacyIntegrationBranchTemplate` as secondary fallback in `DetectIntegrationBranch`. New test added. |
| R3-2 | Partial `merge_queue` config silently disables refinery tests | Major | **Fixed** | Changed `RunTests` and `DeleteMergedBranches` from `bool` to `*bool` with nil-safe accessors defaulting `true`. Regression test added (fork PR #6, `gt-c2h`). |
| R3-3 | Empty epic title produces invalid branch `integration/` | Major | **Fixed** | `BuildIntegrationBranchName` falls back to epic ID when sanitized title is empty. Regression test added (fork PR #5, `gt-mq8`). |
| R3-4 | `cleanupIntegrationBranch` swallows errors silently | Minor | **Fixed** | Changed return type to `[]string` (warnings). Both callers check and report partial cleanup. |

### Julian's Manual Testing (28-test plan, 22 executed)

| # | Finding | Severity | Status | Fork PR | Notes |
|---|---------|----------|--------|---------|-------|
| MT-1 | `gt mq integration status` always shows 0 MRs -- queries `Type: "merge-request"` but MR beads have `Type: "task"` with label `gt:merge-request` | Major | **Folded** (code hygiene only) | [Xexr/gastown#3](https://github.com/Xexr/gastown/pull/3) (closed) | Cherry-picked as code hygiene. Our review found the fix is a **behavioral no-op** -- `beads.List` already translates Type to `--label`. If 0-MRs symptom was real, root cause is elsewhere. See `gt-6ck`. |
| MT-2 | Epic auto-closed before `gt mq integration land` -- last child MR close triggers epic auto-close | Minor | **Open** | -- | Lifecycle timing issue. Design decision needed: delay auto-close, re-open during land, or decouple. `gt-2rz`. |
| MT-3 | Refinery AI bypasses `auto_land=false` guard -- merges integration branch via raw git commands | Major | **Open** (Draft PR reviewed) | [Xexr/gastown#8](https://github.com/Xexr/gastown/pull/8) | Three-layer enforcement architecture reviewed and validated. Blocked on core.hooksPath gap in WorktreeAddExisting() — Layer 2 ineffective on fresh rigs. Need infrastructure fix before folding. `gt-58j`, `gt-627`. |
| MT-4 | Duplicate `gt mq integration create` succeeds silently -- overwrites metadata, orphans old branch | Minor | **Open** | -- | Need to add existence check + `--force` flag in runMqIntegrationCreate. Straightforward implementation. `gt-dgt`. |
| MT-5 | PR reverts wisp hooking fix from #1372 in refinery + witness formulas | Minor | **Resolved** | -- | Fixed by rebasing onto upstream/main which includes #1372. |

### Round 4: Dual-model automated review (Claude + Codex) -- from @julianknutsen, request_changes

| # | Finding | Severity | Status | Fork PR | Notes |
|---|---------|----------|--------|---------|-------|
| R4-1 | Silent config loss for `target_branch` in existing rigs -- refinery's `LoadConfig()` never calls `warnDeprecatedMergeQueueKeys` | Blocker | **Folded** | [Xexr/gastown#7](https://github.com/Xexr/gastown/pull/7) (closed) | l0g1x determined `Engineer.LoadConfig()` is dead code. Doctor check added instead. Follow-ups complete: duplicate var fixed (`gt-bvx`, `d1e36649`), multi-rig test added (`gt-e7w`, `543ecd23`). |
| R4-2 | Legacy epics resolve to wrong integration branch in land/status -- `land`, `status`, `resolveIntegrationBranchName` don't use `DetectIntegrationBranch`'s two-step fallback | Major | **Folded** | [Xexr/gastown#4](https://github.com/Xexr/gastown/pull/4) (closed) | New `resolveEpicBranch` function covers all 3 affected paths. 6 test scenarios. Note: land doesn't fetch before resolution (pre-existing, not a regression). |
| R4-3 | `land` accepts local-only branch then fails on `origin/` refs downstream | Major | **Fixed** | -- | Early validation rejects local-only branch with "push it first" message. Commit `ddef9eb2`. |
| R4-4 | `RefExists` masks infrastructure failures as "ref missing" -- `GitError` -> `false, nil` for all errors | Major | **Fixed** | -- | Narrowed to "Needed a single revision" pattern only. Commit `77ccdaa2`. |
| R4-5 | Inconsistent error handling in `DetectIntegrationBranch` -- metadata path hard-errors, legacy path swallows | Minor | **Fixed** | -- | Both paths now swallow consistently (best-effort). Commit `e635931f`. |
| R4-6 | Missing compile-time interface assertion `var _ BranchChecker = (*git.Git)(nil)` | Minor | **Fixed** | -- | Added in `git/interface_test.go`. Commit `1e7cc81f`. |
| R4-7 | `.land-worktree/` not in `.gitignore` for existing rigs -- only added during rig init | Minor | **Open** | -- | Needs a `gt doctor` check. `gt-58n`. |
| R4-8 | Validation error message omits newly rejected `?`, `*`, `[` characters | Nit | **Fixed** | -- | Error message now includes `?`, `*`, `[`. Commit `1e7cc81f`. |

### Our Review Findings (from fork PR code review, 2026-02-13)

| # | Finding | Source | Severity | Status | Bead | Notes |
|---|---------|--------|----------|--------|------|-------|
| F-1 | MT-1 root cause unresolved -- PR #3 fix is behavioral no-op, 0-MRs symptom may persist | PR #3 review | Minor | **Open** | `gt-6ck` | If symptom was real, root cause is elsewhere (DB routing, bd version, env setup) |
| F-2 | Incomplete Type→Label migration -- 4 query-side callsites still use deprecated pattern | PR #3 review | Minor | **Fixed** | `gt-4sk` | mq_list.go:31, mq_next.go:63, status.go:1180, refinery/manager.go:224. Commit `c1ee17ec`. |
| F-3 | Mock beads.List filters Type and Label independently, real impl treats as mutually exclusive | PR #3 review | Low | **Open** | `gt-p9m` | No current caller passes both fields. Does not block squash. |
| F-4 | Duplicate `deprecatedMergeQueueKeys` variable in config/loader.go and doctor check | PR #7 review | Minor | **Fixed** | `gt-bvx` | Exported from config package. Commit `d1e36649`. |
| F-5 | No multi-rig test for DeprecatedMergeQueueKeysCheck | PR #7 review | Minor | **Fixed** | `gt-e7w` | Multi-rig test added (clean + dirty rig). Commit `543ecd23`. |
| F-6 | `removeDeprecatedKeys` hardcodes 0o644 file permissions | PR #7 review | Low | **Open** | -- | Matches SaveRigSettings behavior. Not filed as bead. |
| F-7 | Land function doesn't fetch before `resolveEpicBranch` (stale refs) | PR #4 review | Low | **Noted** | -- | Pre-existing condition, not a regression from PR #4. |

---

## Fork PRs (by l0g1x, on Xexr/gastown)

| Fork PR | Title | Addresses | State | Cherry-pick | Notes |
|---------|-------|-----------|-------|-------------|-------|
| [#3](https://github.com/Xexr/gastown/pull/3) | fix: use Label instead of Type to query MR beads | MT-1 | **Closed** | `b30e4b65` → `19c197ee` | Behavioral no-op (code hygiene). 3 caveats filed as beads. |
| [#4](https://github.com/Xexr/gastown/pull/4) | fix: add legacy {epic} template fallback to land/status | R4-2 | **Closed** | `6dd5a3a9` → `2f1ab666` | Covers all 3 affected paths. Conflict resolved (additive). |
| [#5](https://github.com/Xexr/gastown/pull/5) | test: regression test for empty/invalid epic titles | R3-3 | **Closed** | `2c642569` → `4782384d` | Clean cherry-pick. Excellent regression test. |
| [#6](https://github.com/Xexr/gastown/pull/6) | test: regression test for partial merge_queue config *bool defaults | R3-2 | **Closed** | `55316d22` → `fef09f64` | Conflict resolved (additive). Thorough regression test. |
| [#7](https://github.com/Xexr/gastown/pull/7) | fix: add gt doctor check for deprecated merge_queue config keys | R4-1 | **Closed** | `1d63e0c8` → `27cb2df9` | Clean cherry-pick. 2 follow-up fixes block squash (`gt-bvx`, `gt-e7w`). |
| [#8](https://github.com/Xexr/gastown/pull/8) | fix: add refinery formula guardrails for integration branch auto_land | MT-3 | **Open (Draft)** | -- | Deferred. core.hooksPath gap unfixed. |

---

## Open Questions from Reviewers

| # | Question | From | Status | Notes |
|---|----------|------|--------|-------|
| Q1 | Polecat spawn base always uses `origin/main`, not integration branch -- child #2 won't see child #1's changes. Intentional? | @julianknutsen | **Answered** | Answered in PR comment -- spawn logic DOES use integration branch via `polecat_spawn.go:111-130` auto-detection. The `manager.go` code Julian referenced is the fallback path. Julian thumbs-up'd the response. |
| Q2 | `mol-polecat-work.formula.toml` changes recommended command from `bd ready` to `gt hook` -- intentional workflow change? | @julianknutsen | **Needs answer** | Need to respond on PR. Was this intentional or swept up in the base_branch pass? `gt-p7i`. |

---

## Remaining Work

### Blocks final squash (`gt-3tm`)

1. ~~**`gt-bvx`** -- Fix duplicate `deprecatedMergeQueueKeys` variable~~ **DONE** (`d1e36649`)
2. ~~**`gt-e7w`** -- Add multi-rig test for DeprecatedMergeQueueKeysCheck~~ **DONE** (`543ecd23`)
3. ~~**`gt-x1z`** (R4-3) -- `land` local-only branch validation~~ **DONE** (`ddef9eb2`)
4. ~~**`gt-61l`** (R4-4) -- `RefExists` error masking~~ **DONE** (`77ccdaa2`)
5. ~~**`gt-03t`** (R4-5) -- Consistent error handling in `DetectIntegrationBranch`~~ **DONE** (`e635931f`)
6. ~~**`gt-52j`** (R4-6) -- Compile-time interface assertion~~ **DONE** (`1e7cc81f`)
7. **`gt-58n`** (R4-7) -- `.land-worktree/` doctor check for existing rigs
8. ~~**`gt-w0h`** (R4-8) -- Validation error message update~~ **DONE** (`1e7cc81f`)
9. **`gt-p7i`** (Q2) -- Answer formula workflow change question
10. ~~**`gt-4fd`** -- Check formula version increments (+1 verified, refinery-patrol bumped 5→6)~~ **DONE** (`1e7cc81f`)

### Does not block squash

11. ~~**`gt-4sk`** (F-2) -- Complete Type→Label migration across 4 remaining callsites~~ **DONE** (`c1ee17ec`)
12. **`gt-6ck`** (F-1) -- Investigate 0-MRs root cause (may be transient)
13. **`gt-p9m`** (F-3) -- Fix mock beads.List mutual-exclusivity inconsistency

### Open — needs design decision or infrastructure work

14. **`gt-2rz`** (MT-2) -- Epic auto-closed before integration land. Design decision: delay auto-close, re-open during land, or decouple lifecycle. Likely simplest: land re-opens if auto-closed.
15. **`gt-58j`** / **`gt-627`** (MT-3) -- Refinery formula guardrails for auto_land. Fork PR #8 reviewed, architecture validated. Blocked on core.hooksPath not set in WorktreeAddExisting(). Need infra fix before folding.
16. **`gt-dgt`** (MT-4) -- Duplicate `mq integration create` guard. Straightforward: check existing metadata, refuse without `--force`.

### Out of scope for this PR

17. **`gt-efd`** (R2-4) -- Additional cmd/ unit test coverage (future-scoped)

---

## Upstream Sync Status

| Item | Value |
|------|-------|
| **Last sync** | 2026-02-13 |
| **upstream/main HEAD** | `ed084c08` |
| **origin/main HEAD** | `27961dfd` (upstream + 1 cherry-pick) |
| **PR branch HEAD** | `e635931f` (15 commits: original + 5 fork PR cherry-picks + 9 fixes) |
| **Absorbed** | 25 commits (18 non-merge) from 11 contributors |
| **All clones aligned** | crew/furiosa, mayor/rig, refinery/rig all at `27961dfd` |
| **Binary rebuilt** | `gt version v0.5.0-831-g27961dfd` |
| **Formulas synced** | `gt doctor --fix` ran, 32 formulas up-to-date |

**Key upstream changes absorbed**: Mayor daemon supervision, agent startup race fix, startup normalization, GT_ROOT config fix, refinery rejection reopen, nuke closes MRs, beads path fix, rig layout detection, deacon patrol restore, session lifecycle unification, stale polecat branch cleanup
