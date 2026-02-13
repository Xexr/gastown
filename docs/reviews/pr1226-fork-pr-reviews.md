# PR #1226 Fork PR Reviews

**Date:** 2026-02-13
**Reviewer:** furiosa (parallel code-reviewer agents, Opus 4.6)
**Methodology:** Each fork PR reviewed independently in parallel. Three questions per PR:
1. Is the original review finding legitimate?
2. Does the fork PR fully address it?
3. Does the fix introduce regressions or new issues?

---

## PR #3 — fix: use Label instead of Type to query MR beads (MT-1) | `gt-c0k`

### Finding Validation

**Partially valid.** The observation (0 MRs in `gt mq integration status`) may have been real, but the root cause diagnosis is **incorrect**.

`beads.List` at `internal/beads/beads.go:306-312` already translates `Type: "merge-request"` to `--label=gt:merge-request`. Both `Type` and `Label` paths produce **identical `bd` CLI commands**. The code change is a behavioral no-op.

If Julian observed 0 MRs during manual testing, the root cause is likely elsewhere — beads DB routing, bd CLI version, or test environment issue.

### Fix Evaluation

**Code hygiene improvement, not a bug fix.**

Changes:
- `mq_integration.go` lines 662, 797: `Type: "merge-request"` → `Label: "gt:merge-request"`
- `mq_testutil_test.go`: mock now filters by Label; `makeTestMR` creates beads with `Type: "task"` + `Labels: ["gt:merge-request"]` (matches production reality)
- `mq_integration_test.go`: two new tests (`TestMakeTestMR_RealisticFields`, `TestMockBeadsList_LabelFilter`)

### Regressions

None. Behavioral no-op.

### Issues

1. **Does not fix the reported symptom** — if 0-MRs was real, it persists. Root cause needs separate investigation.
2. **Incomplete migration** — 6+ other callsites still use deprecated `Type: "merge-request"` pattern: `mq_list.go:31`, `mq_next.go:63`, `status.go:1180`, `done.go:511`, `mq_submit.go:196`, `refinery/manager.go:224`
3. **Mock filtering inconsistency** — mock checks `Type` and `Label` independently, but real `beads.List` treats them as mutually exclusive (Label takes priority)

### Verdict

**Accept with caveats.** Good code hygiene. Consider completing the migration across all callsites in a single pass. File separate bead for 0-MRs root cause investigation.

---

## PR #4 — fix: add legacy {epic} template fallback to land/status (R4-2) | `gt-80a`

### Finding Validation

**Confirmed valid. Major severity appropriate.**

Four branch resolution paths exist:

| Path | Has Legacy Fallback? | Affected? |
|------|---------------------|-----------|
| `DetectIntegrationBranch` (beads/integration.go:184-245) | Yes (lines 218-234) | No |
| `resolveIntegrationBranchName` (mq_integration.go:199-214) | Only on error path | **Yes — happy path missing** |
| `runMqIntegrationLand` (mq_integration.go:414-417) | No | **Yes** |
| `runMqIntegrationStatus` (mq_integration.go:743-745) | No | **Yes** |

Legacy epics (created with `{epic}` template, no metadata, title != epic ID) will resolve to wrong branch name. User sees:
```
integration branch 'integration/add-user-authentication' does not exist
```
...when actual branch is `integration/gt-abc`.

### Fix Evaluation

**Reviewer could not access the diff directly.** Architecture analysis indicates the fix needs to:
- Add branch-existence check + legacy `{epic}` fallback to `land` and `status` (they have git access)
- Address `resolveIntegrationBranchName` (needs `BranchChecker` param or refactoring)
- Cover `mq_submit --epic` and `mq_list --epic` paths (both use `resolveIntegrationBranchName`)

**Must verify the actual diff covers all affected paths.**

### Regressions

No risk to new-style branches (they have metadata). If both `{title}` and `{epic}` branches exist for same epic (unlikely), `{title}` is preferred (correct).

### Verdict

**Finding confirmed. Need to review actual diff** to verify it covers all 3 affected code paths.

---

## PR #5 — test: regression test for empty/invalid epic titles (R3-3) | `gt-mq8`

### Finding Validation

**Legitimate.** `BuildIntegrationBranchName` with `{title}` template produced `integration/` for empty titles. Fix (already in main PR at `beads/integration.go:143-146`) falls back to epic ID.

### Fix Evaluation

**Excellent regression test.** `TestBuildIntegrationBranchName_NeverProducesInvalidRef` added to `cmd/mq_integration_test.go`.

Table-driven, 5 cases:

| Case | Template | Title | Purpose |
|------|----------|-------|---------|
| empty + default | `""` | `""` | Core bug |
| empty + explicit {title} | `"integration/{title}"` | `""` | Same, explicit |
| special-chars-only | `"integration/{title}"` | `"!@#$%^&*()"` | Sanitization |
| whitespace-only | `"integration/{title}"` | `" "` | Edge case |
| empty + {epic} | `"integration/{epic}"` | `""` | Control |

Three-layer assertion: `validateBranchName`, no trailing `/`, non-empty final segment. Tests real code (not mocks).

### Regressions

None. Purely additive (70 lines). Intentional partial overlap with `beads/integration_test.go` — tests at different layer (cmd wrapper + validator composition).

### Verdict

**Ready to merge.** Well-crafted regression test.

---

## PR #6 — test: regression test for partial merge_queue config *bool defaults (R3-2) | `gt-c2h`

### Finding Validation

**Legitimate.** Plain `bool` fields defaulted to `false` on partial JSON deserialization, silently disabling `RunTests` and `DeleteMergedBranches`. Fix (already in main PR) changed to `*bool` with nil-safe accessors defaulting `true`.

### Fix Evaluation

**Excellent regression test.** Two test functions:

1. `TestMergeQueueConfig_PartialJSON_BoolDefaults` — 3 subtests:
   - Minimal config (all `*bool` omitted) — verifies correct defaults
   - Explicit `false` — verifies not overridden
   - Explicit `true` — verifies passthrough

   Covers all 5 `*bool` fields: `RunTests`, `DeleteMergedBranches`, `PolecatIntegration`, `RefineryIntegration`, `AutoLand`

2. `TestMergeQueueConfig_PartialJSON_NilPointers` — verifies omitted fields deserialize to `nil` (not `*false`)

### Regressions

None. Purely additive (121 lines). Not a duplicate — existing `TestDefaultMergeQueueConfig` tests factory path, this tests deserialization path.

### Notes

- `Engineer.LoadConfig()` in `engineer.go` has separate `MergeQueueConfig` (plain `bool`) but is safe — uses `*bool` intermediate for JSON. Also confirmed dead code (R4-1).
- Test targets `internal/config/loader_test.go` — ensure propagated to both `refinery/rig` and `mayor/rig` copies when folding.

### Verdict

**Ready to merge.** Thorough regression test.

---

## PR #7 — fix: add gt doctor check for deprecated merge_queue config keys (R4-1) | `gt-2mu`

### Finding Validation

**Legitimate.** `target_branch` and `integration_branches` removed from config struct with no migration. `Engineer.LoadConfig()` confirmed dead code — zero production call sites (only `engineer_test.go`). Production uses `config.LoadRigSettings()` which reads `settings/config.json`. Doctor check is the right approach.

The deprecated keys are silently ignored by `json.Unmarshal` — not "lost" but having no effect. Risk is user confusion (setting `target_branch: "develop"` thinking it controls merge targets).

### Fix Evaluation

**Clean implementation following established patterns.** `DeprecatedMergeQueueKeysCheck` embeds `FixableCheck`, uses `findAllRigs()` helper, reads raw JSON to detect deprecated keys.

7 unit tests covering: clean config, single deprecated key (×2), both keys, no merge_queue section, no settings file, fix preserves valid keys.

### Issues

1. **Duplicate `deprecatedMergeQueueKeys` variable** — same `[]string{"target_branch", "integration_branches"}` in both `config/loader.go:296` and new `doctor/deprecated_config_check.go`. Should export from `config` package or cross-reference.
2. **`removeDeprecatedKeys` doesn't preserve key order** — `json.MarshalIndent` on `map[string]json.RawMessage` reorders keys. Creates git noise. Acceptable precedent in codebase (`SessionHookCheck.fixSettingsFile` has same pattern).
3. **No multi-rig test** — all tests use single rig. Should add test with two rigs (one clean, one deprecated).
4. **Doctor help text not updated** — `cmd/doctor.go` Long help string should list the new check.

### Verdict

**Accept with minor fixes** (duplicate variable, multi-rig test). Architecture is sound.

---

## PR #8 — fix: add refinery formula guardrails for integration branch auto_land (MT-3) | `gt-58j`

### Finding Validation

**Confirmed valid.** The refinery AI can bypass `auto_land=false` using raw git commands. The `check-integration-branches` formula step is a soft guardrail only.

Three infrastructure gaps identified:
1. Pre-push hook doesn't include `integration/*` in allowed patterns
2. Refinery worktree missing `core.hooksPath` (created via `WorktreeAddExisting()` which skips `configureHooksPath()`)
3. Even with hooks active, `integration/*` pushes blocked for legitimate operations

### Fix Evaluation

**Three-layer enforcement approach:**

| Layer | Mechanism | Strength |
|-------|-----------|----------|
| 1. Formula | FORBIDDEN directives | Soft (AI can ignore) |
| 2. Git hook | Enhanced pre-push blocks integration→main unless `GT_INTEGRATION_LAND=1` | Hard (requires `core.hooksPath`) |
| 3. Code | `PushWithEnv()` sets env var only through validated `gt mq integration land` path | Hard |

Architecture is sound. Includes `pre-push_test.sh` with 9 test cases.

### Key Gap

**`core.hooksPath` not set during initial refinery worktree creation** (`rig/manager.go:506`). Without `gt doctor --fix`, Layer 2 (the strongest guardrail) is completely ineffective. This is acknowledged but not fixed in the PR.

### Regressions

No impact on normal refinery operations. `integration/*` addition to allowed push patterns is necessary for the feature.

### Verdict

**Not ready to merge (Draft status correct).** Architecture is sound but depends on unfixed `core.hooksPath` infrastructure gap. Should either include the `core.hooksPath` provisioning fix or document it as a prerequisite.

---

## Summary Decision Table

| PR | Finding | Fix Quality | Verdict | Bead |
|----|---------|-------------|---------|------|
| #3 (MT-1) | Partially valid | Code hygiene, not bug fix | Accept with caveats | `gt-c0k` |
| #4 (R4-2) | Confirmed Major | Need to verify diff | Review diff | `gt-80a` |
| #5 (R3-3) | Confirmed | Excellent test | **Ready** | `gt-mq8` |
| #6 (R3-2) | Confirmed | Excellent test | **Ready** | `gt-c2h` |
| #7 (R4-1) | Confirmed | Clean, minor fixes needed | Accept with fixes | `gt-2mu` |
| #8 (MT-3) | Confirmed Major | Good architecture, key gap | **Defer (Draft)** | `gt-58j` |

## Recommended Execution Order

1. **Phase 1 (easy wins):** PRs #5, #6 — clean regression tests, ready to commit
2. **Phase 2 (need diff review):** PRs #3, #4, #7 — findings validated, need to inspect actual diffs
3. **Phase 3 (defer):** PR #8 — Draft, infrastructure gap unfixed
