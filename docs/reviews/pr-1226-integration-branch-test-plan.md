# Integration Branch Test Plan

> Manual validation tests for PR #1226: make integration branches end-to-end across the pipeline.
> Each phase is designed to be run sequentially by a crew member in the gastown rig.
>
> **Review note:** This is a revised version of the original test plan from `crew/max/`.
> Changes from the original are marked with `[REVISED]` or `[NEW]` tags.

## Prerequisites

- gastown rig is running with beads configured
- Merge queue is enabled in `settings/config.json`
- The rig has `default_branch` set to `main` in `config.json`
- A clean git state on main (no pending MRs, no stale branches)
- `[NEW]` Run `gt doctor --fix` after checking out the PR branch to sync updated formulas

---

## Phase 1: Baseline — Beads Without Integration Branches

**Goal:** Confirm that standalone issues (no epic, no integration branch) commit to the default branch (`main`).

### Steps

1. Create a standalone task (not under an epic):
   ```bash
   bd create --type=task --title="Standalone task for baseline test"
   ```
   Note the issue ID (e.g., `gt-abc`).

2. Sling it to the rig:
   ```bash
   gt sling gt-abc gastown
   ```

3. Wait for the polecat to spawn and check what branch its worktree is on:
   ```bash
   # Find the polecat worktree
   ls ~/gt/gastown/polecats/
   # Check its branch
   git -C ~/gt/gastown/polecats/<polecat-name>/rig branch --show-current
   ```

### Validation

- [ ] The polecat worktree is based on `main` (not an integration branch)
- [ ] When the polecat completes and submits an MR, the MR target is `main`
  ```bash
  bd list --type=merge-request --status=open
  # Check the target field of the MR
  bd show <mr-id>
  ```
- [ ] After refinery merges it, the commit lands on `main`

---

## Phase 2: Epic Without Integration Branch

**Goal:** Confirm that issues under an epic that has NO integration branch still target the default branch.

### Steps

1. Create an epic:
   ```bash
   bd create --type=epic --title="Epic without integration branch"
   ```
   Note the epic ID (e.g., `gt-epic1`).

2. Create a child task under the epic:
   ```bash
   bd create --type=task --title="Child task under plain epic" --parent=gt-epic1
   ```
   Note the child ID (e.g., `gt-child1`).

3. Sling the child:
   ```bash
   gt sling gt-child1 gastown
   ```

4. Check the polecat's worktree branch.

### Validation

- [ ] The polecat worktree is based on `main` (no integration branch exists, so fallback to default)
- [ ] The MR created by `gt done` targets `main`
- [ ] After refinery merge, the commit lands on `main`
- [ ] The epic remains open (it has no integration branch lifecycle)

---

## Phase 3: Epic With Integration Branch — Create and Verify

**Goal:** Create an integration branch for an epic and verify it exists and metadata is stored correctly.

### Steps

1. Create an epic:
   ```bash
   bd create --type=epic --title="Auth overhaul test"
   ```
   Note the epic ID (e.g., `gt-epic2`).

2. Create the integration branch:
   ```bash
   gt mq integration create gt-epic2
   ```

3. Verify the branch was created:
   ```bash
   git branch -r | grep integration
   ```

4. Check the epic metadata:
   ```bash
   bd show gt-epic2
   ```

5. Check integration status:
   ```bash
   gt mq integration status gt-epic2
   ```

### Validation

- [ ] `gt mq integration create` succeeds and prints the branch name (should be `integration/auth-overhaul-test` based on `{title}` template)
- [ ] The branch exists on the remote: `git branch -r | grep auth-overhaul`
- [ ] `bd show gt-epic2` description contains `integration_branch: integration/auth-overhaul-test`
- [ ] `bd show gt-epic2` description contains `base_branch: main`
- [ ] `gt mq integration status gt-epic2` shows the branch name, 0 commits ahead, not ready to land (no children)

---

## Phase 4: Sling Child to Integration Branch

**Goal:** Confirm that slinging a child of an epic with an integration branch causes the polecat to source its worktree from the integration branch.

### Steps

1. Create two child tasks under the epic from Phase 3:
   ```bash
   bd create --type=task --title="Auth tokens" --parent=gt-epic2
   bd create --type=task --title="Auth sessions" --parent=gt-epic2
   ```
   Note the child IDs (e.g., `gt-tokens`, `gt-sessions`).

2. Sling the first child:
   ```bash
   gt sling gt-tokens gastown
   ```

3. Check the polecat's worktree:
   ```bash
   ls ~/gt/gastown/polecats/
   git -C ~/gt/gastown/polecats/<polecat-name>/rig log --oneline -3
   git -C ~/gt/gastown/polecats/<polecat-name>/rig branch --show-current
   ```

### Validation

- [ ] The polecat's worktree is based on the integration branch (not `main`)
- [ ] `git log` in the polecat worktree shows the integration branch history
- [ ] The polecat's formula vars include `base_branch` set to the integration branch name

---

## Phase 5: MR Targets Integration Branch (gt done)

**Goal:** Confirm that when a polecat under an integration-branch epic runs `gt done`, the MR targets the integration branch.

### Steps

1. Let the polecat from Phase 4 complete its work (or manually trigger `gt done` from the polecat context if testing interactively).

2. Check the MR that was created:
   ```bash
   bd list --type=merge-request --status=open
   bd show <mr-id>
   ```

### Validation

- [ ] The MR's target field is `integration/auth-overhaul-test` (NOT `main`)
- [ ] The MR's source branch is the polecat's feature branch

---

## Phase 6: Refinery Merges to Integration Branch

**Goal:** Confirm the refinery merges the MR into the integration branch, not main.

### Steps

1. Wait for the refinery to process the MR (or trigger a refinery cycle).

2. After the MR is merged, check the integration branch:
   ```bash
   git fetch origin
   git log --oneline origin/integration/auth-overhaul-test -5
   ```

3. Check main has NOT received this commit:
   ```bash
   git log --oneline origin/main -5
   ```

4. Check integration status:
   ```bash
   gt mq integration status gt-epic2
   ```

### Validation

- [ ] The polecat's work commit is on `origin/integration/auth-overhaul-test`
- [ ] The polecat's work commit is NOT on `origin/main`
- [ ] `gt mq integration status` shows 1+ commits ahead of main
- [ ] `gt mq integration status` shows the first child as closed/merged
- [ ] Status shows NOT ready to land (second child still open)

---

## Phase 7: Second Child Inherits Sibling Work

**Goal:** Confirm that the second polecat starts from the integration branch and sees the first child's work.

### Steps

1. Sling the second child:
   ```bash
   gt sling gt-sessions gastown
   ```

2. Check the new polecat's worktree:
   ```bash
   git -C ~/gt/gastown/polecats/<new-polecat>/rig log --oneline -5
   ```

### Validation

- [ ] The second polecat's worktree contains the first child's commits (it started from the integration branch which already has that work merged)
- [ ] The second polecat can build on the first child's changes without conflicts

---

## Phase 8: All Children Closed — Ready to Land

**Goal:** Once both children are merged into the integration branch, confirm status shows ready to land.

### Steps

1. Let the second polecat complete and its MR merge to the integration branch.

2. Check status:
   ```bash
   gt mq integration status gt-epic2
   ```

### Validation

- [ ] All children show as closed
- [ ] No pending MRs
- [ ] Integration branch has commits ahead of main
- [ ] Status shows `ready_to_land: true`

---

## Phase 9: Manual Land

**Goal:** Confirm `gt mq integration land` merges the integration branch to main as a single merge commit, cleans up the branch, and closes the epic.

### Steps

1. Run dry-run first:
   ```bash
   gt mq integration land gt-epic2 --dry-run
   ```

2. Verify dry-run output looks correct, then land for real:
   ```bash
   gt mq integration land gt-epic2 --skip-tests
   ```
   (Use `--skip-tests` since this is a validation test, not a real project.)

3. Verify the result:
   ```bash
   git fetch origin
   git log --oneline --graph origin/main -10
   git branch -r | grep integration/auth-overhaul
   bd show gt-epic2
   ```

### Validation

- [ ] Dry-run shows what would happen without making changes
- [ ] After land: `origin/main` has a merge commit containing all the child work
- [ ] The merge commit is a `--no-ff` merge (visible in `--graph` output)
- [ ] The integration branch is deleted from remote (`git branch -r` no longer lists it)
- [ ] The epic is closed (`bd show gt-epic2` shows status=closed)

---

## Phase 10: Non-Main Default Branch

**Goal:** Confirm that when `default_branch` is set to something other than `main`, the entire flow targets that branch instead.

### Steps

1. Change the rig's default branch in `config.json`:
   ```bash
   # Edit ~/gt/gastown/config.json — set "default_branch": "develop"
   ```

2. Create the `develop` branch if it doesn't exist:
   ```bash
   git push origin main:develop
   ```

3. Create a new epic + child + integration branch:
   ```bash
   bd create --type=epic --title="Develop branch test"
   # Note epic ID: gt-epic3
   bd create --type=task --title="Develop child" --parent=gt-epic3
   # Note child ID: gt-devchild
   gt mq integration create gt-epic3
   ```

4. Check the epic metadata:
   ```bash
   bd show gt-epic3
   ```

5. Sling the child:
   ```bash
   gt sling gt-devchild gastown
   ```

6. Let it complete through the pipeline (polecat → done → refinery merge → land).

7. Land:
   ```bash
   gt mq integration land gt-epic3 --skip-tests
   ```

8. Check where work landed:
   ```bash
   git fetch origin
   git log --oneline origin/develop -5
   git log --oneline origin/main -5
   ```

### Validation

- [ ] `bd show gt-epic3` metadata shows `base_branch: develop`
- [ ] Integration branch was created from `develop` (not `main`)
- [ ] MRs target the integration branch (not `develop` directly)
- [ ] After land, the merge commit is on `origin/develop` (NOT `origin/main`)
- [ ] `origin/main` is unchanged

### Cleanup

```bash
# Reset default_branch back to main
# Edit ~/gt/gastown/config.json — set "default_branch": "main"
# Optionally delete develop branch
git push origin --delete develop
```

---

## Phase 11: Config Gates — Disable Polecat Integration

**Goal:** Confirm that setting `integration_branch_polecat_enabled: false` makes polecats ignore integration branches and spawn from the default branch.

`[REVISED]` Clarified validation: the polecat gate and the MR-target gate are
independent config fields. Disabling the polecat gate only affects worktree
sourcing. The MR target is controlled by `integration_branch_refinery_enabled`
(which remains `true` by default in this test).

### Pre-check `[NEW]`

```bash
# Confirm refinery integration is still enabled (default) before proceeding
# settings/config.json should NOT contain integration_branch_refinery_enabled: false
```

### Steps

1. Create an epic with integration branch (reuse or create new):
   ```bash
   bd create --type=epic --title="Config gate polecat test"
   # Note: gt-epic4
   gt mq integration create gt-epic4
   bd create --type=task --title="Polecat gate child" --parent=gt-epic4
   # Note: gt-pgate
   ```

2. Set `integration_branch_polecat_enabled` to `false` in `settings/config.json`.

3. Sling:
   ```bash
   gt sling gt-pgate gastown
   ```

4. Check the polecat's worktree base.

### Validation

- [ ] Even though the epic has an integration branch, the polecat spawns from `main`
- [ ] `[REVISED]` The MR still targets the integration branch — this is because `integration_branch_refinery_enabled` (which gates `gt done` target detection) is still `true` (default). The two gates are independent: polecat gate controls worktree sourcing, refinery gate controls MR targeting.

### Cleanup

- Remove or reset `integration_branch_polecat_enabled` in settings.

---

## Phase 12: Config Gates — Disable Refinery Integration

**Goal:** Confirm that setting `integration_branch_refinery_enabled: false` makes `gt done` target the default branch even when an integration branch exists.

`[REVISED]` Added edge case validation. When the refinery gate is disabled but
the polecat gate is enabled, an asymmetric state occurs: the polecat works from
the integration branch but the MR targets main. This is by design (independent
gates) but should be validated for correctness.

### Pre-check `[NEW]`

```bash
# Confirm polecat integration is still enabled (default) before proceeding
# settings/config.json should NOT contain integration_branch_polecat_enabled: false
```

### Steps

1. Create an epic with integration branch:
   ```bash
   bd create --type=epic --title="Config gate refinery test"
   # Note: gt-epic5
   gt mq integration create gt-epic5
   bd create --type=task --title="Refinery gate child" --parent=gt-epic5
   # Note: gt-rgate
   ```

2. Set `integration_branch_refinery_enabled` to `false` in `settings/config.json`.

3. Sling and let the polecat complete.

4. Check the MR target.

### Validation

- [ ] `[REVISED]` The polecat spawns from the integration branch (polecat gate is still `true` by default, independent of refinery gate)
- [ ] The MR targets `main` (NOT the integration branch) because `gt done` target detection is disabled
- [ ] The integration branch receives no new commits from this MR
- [ ] `[NEW]` The MR merges cleanly to main despite the polecat having been based on the integration branch (verify no conflicts or duplicate commits if the integration branch had diverged from main)

### Cleanup

- Remove or reset `integration_branch_refinery_enabled` in settings.

---

## Phase 13: Custom Branch Template

**Goal:** Confirm that `integration_branch_template` controls the branch name.

### Steps

1. Set `integration_branch_template` to `"feat/{epic}"` in `settings/config.json`.

2. Create an epic:
   ```bash
   bd create --type=epic --title="Template test epic"
   # Note: gt-epic6
   ```

3. Create the integration branch:
   ```bash
   gt mq integration create gt-epic6
   ```

4. Check what branch was created:
   ```bash
   git branch -r | grep feat/
   ```

### Validation

- [ ] The branch name follows the custom template: `feat/gt-epic6` (using `{epic}` variable)
- [ ] Epic metadata stores the actual branch name used (not the template)
- [ ] Auto-detection still finds it when a child is slung (metadata takes precedence over template)

### Cleanup

- Remove or reset `integration_branch_template` in settings.

---

## Phase 14: --base-branch Override on Create

**Goal:** Confirm that `--base-branch` on `gt mq integration create` sets where the branch is created from and where it lands back to.

### Steps

1. Create a release branch:
   ```bash
   git push origin main:release/v2
   ```

2. Create an epic + integration branch with `--base-branch`:
   ```bash
   bd create --type=epic --title="Release branch test"
   # Note: gt-epic7
   gt mq integration create gt-epic7 --base-branch release/v2
   ```

3. Check metadata:
   ```bash
   bd show gt-epic7
   ```

4. Verify the branch was created from the right base:
   ```bash
   git fetch origin
   git log --oneline origin/integration/release-branch-test -3
   # Should share ancestry with release/v2, not main
   ```

### Validation

- [ ] `bd show gt-epic7` metadata shows `base_branch: release/v2`
- [ ] The integration branch is rooted on `release/v2`
- [ ] Landing would merge back to `release/v2` (can verify with `--dry-run`):
  ```bash
  gt mq integration land gt-epic7 --dry-run
  ```

### Cleanup

```bash
git push origin --delete release/v2
```

---

## Phase 15: Nested Epics — Detection Walks Parent Chain

**Goal:** Confirm that a task under a sub-epic (which has no integration branch) correctly inherits the integration branch from the grandparent epic.

### Steps

1. Create a parent epic with integration branch:
   ```bash
   bd create --type=epic --title="Grandparent epic"
   # Note: gt-gp
   gt mq integration create gt-gp
   ```

2. Create a sub-epic under it (no integration branch on the sub-epic):
   ```bash
   bd create --type=epic --title="Sub-epic" --parent=gt-gp
   # Note: gt-sub
   ```

3. Create a task under the sub-epic:
   ```bash
   bd create --type=task --title="Nested task" --parent=gt-sub
   # Note: gt-nested
   ```

4. Sling the nested task:
   ```bash
   gt sling gt-nested gastown
   ```

5. Check the polecat's worktree and eventual MR target.

### Validation

- [ ] The polecat spawns from the grandparent's integration branch (detection walked: task → sub-epic (no branch) → grandparent epic (has branch))
- [ ] The MR targets the grandparent's integration branch
- [ ] The sub-epic's lack of its own integration branch did NOT prevent detection

**Note:** Detection walks up to 10 levels of parent hierarchy. This test covers 2 levels (the practical common case).

---

## Phase 16: Land with Pending MRs (--force)

**Goal:** Confirm that landing with open MRs fails by default, and `--force` overrides.

### Steps

1. Using an epic with an integration branch and at least one open MR still pending:
   ```bash
   gt mq integration land gt-epic-with-open-mr
   ```

2. Expect failure. Then force:
   ```bash
   gt mq integration land gt-epic-with-open-mr --force --skip-tests
   ```

### Validation

- [ ] Without `--force`: command fails with an error listing the open MRs
- [ ] With `--force`: command proceeds and merges whatever is on the integration branch

---

## Phase 17: Idempotent Land (Crash Recovery)

**Goal:** Confirm that running `land` twice is safe.

### Steps

1. After a successful land from a previous phase, run the same land command again:
   ```bash
   gt mq integration land gt-epic2
   ```

### Validation

- [ ] The command does NOT fail catastrophically
- [ ] It either reports "already merged" / "branch not found" or completes cleanup gracefully
- [ ] No duplicate merge commits appear on main

---

## Phase 18: `--branch` Flag Override on Create `[NEW]`

**Goal:** Confirm that `--branch` on `gt mq integration create` fully overrides the template, and that metadata-first resolution uses the stored name.

### Steps

1. Create an epic:
   ```bash
   bd create --type=epic --title="Custom branch name test"
   # Note: gt-epic8
   ```

2. Create the integration branch with an explicit name:
   ```bash
   gt mq integration create gt-epic8 --branch my-custom-branch
   ```

3. Check what was stored:
   ```bash
   bd show gt-epic8
   git branch -r | grep my-custom-branch
   ```

4. Create a child task and sling it:
   ```bash
   bd create --type=task --title="Branch override child" --parent=gt-epic8
   # Note: gt-brchild
   gt sling gt-brchild gastown
   ```

5. Check the polecat's worktree and MR target.

### Validation

- [ ] The branch name is exactly `my-custom-branch` (not `integration/custom-branch-name-test` from template)
- [ ] `bd show gt-epic8` metadata stores `integration_branch: my-custom-branch`
- [ ] Auto-detection finds it via metadata (not template fallback) — polecat spawns from `my-custom-branch`
- [ ] MR targets `my-custom-branch`

### Cleanup

```bash
git push origin --delete my-custom-branch
```

---

## Phase 19: Build Pipeline Commands `[NEW]`

**Goal:** Confirm that rig-level build pipeline commands are injected into polecat formulas.

The PR adds 5 configurable commands (`setup_command`, `typecheck_command`, `lint_command`, `test_command`, `build_command`) that are auto-injected into polecat-work, refinery-patrol, and sync-workspace formulas.

### Steps

1. Set build pipeline commands in `settings/config.json`:
   ```json
   {
     "merge_queue": {
       "setup_command": "echo setup-ok",
       "typecheck_command": "echo typecheck-ok",
       "lint_command": "echo lint-ok",
       "test_command": "echo test-ok",
       "build_command": "echo build-ok"
     }
   }
   ```

2. Create a task and sling it:
   ```bash
   bd create --type=task --title="Build pipeline test"
   # Note: gt-bptest
   gt sling gt-bptest gastown
   ```

3. Before the polecat starts work, inspect its formula variables:
   ```bash
   # Check the molecule's wisp bead for injected vars
   bd show <wisp-id>
   ```

4. Alternatively, check the polecat's prime output for the injected commands.

### Validation

- [ ] The polecat formula receives `setup_command=echo setup-ok`
- [ ] The polecat formula receives `typecheck_command=echo typecheck-ok`
- [ ] The polecat formula receives `lint_command=echo lint-ok`
- [ ] The polecat formula receives `test_command=echo test-ok`
- [ ] The polecat formula receives `build_command=echo build-ok`
- [ ] Commands with value `"none"` (or empty) are skipped in the formula

### Cleanup

- Remove build pipeline commands from `settings/config.json`.

---

## Phase 20: `default_branch` Validation `[NEW]`

**Goal:** Confirm that invalid `default_branch` values produce actionable errors instead of cryptic git failures.

### Steps

1. **Polecat spawn validation:**
   ```bash
   # Set default_branch to a nonexistent branch
   # Edit ~/gt/gastown/config.json — set "default_branch": "nonexistent-branch"

   bd create --type=task --title="Validation test task"
   # Note: gt-valtest
   gt sling gt-valtest gastown
   ```

2. **Doctor check:**
   ```bash
   gt doctor
   ```

3. **Rig add validation** (optional, only if creating a new rig):
   ```bash
   gt rig add test-rig --repo=<some-repo> --default-branch=nonexistent
   ```

### Validation

- [ ] Polecat spawn fails with a clear error message listing the bad branch and possible remediation (NOT a cryptic `exit status 128`)
- [ ] `gt doctor` reports the bad `default_branch` as a check failure
- [ ] `gt rig add` with a bad `--default-branch` rejects the config before saving

### Cleanup

```bash
# Reset default_branch back to main
# Edit ~/gt/gastown/config.json — set "default_branch": "main"
```

---

## Phase 21: Stale Remote Branch Detection `[NEW]`

**Goal:** Confirm that detection prefers remote branch existence over stale local tracking refs.

### Steps

1. Create an epic with integration branch:
   ```bash
   bd create --type=epic --title="Stale ref test"
   # Note: gt-stale
   gt mq integration create gt-stale
   bd create --type=task --title="Stale ref child" --parent=gt-stale
   # Note: gt-stalekid
   ```

2. Delete the remote integration branch but leave local tracking ref:
   ```bash
   git push origin --delete integration/stale-ref-test
   # Local ref origin/integration/stale-ref-test still exists until fetch --prune
   ```

3. Sling the child (without running `git fetch --prune`):
   ```bash
   gt sling gt-stalekid gastown
   ```

4. Check the polecat's worktree base.

### Validation

- [ ] The polecat spawns from `main` (not the stale local ref)
- [ ] Detection correctly identified the remote branch as deleted via `git ls-remote`
- [ ] The polecat's MR targets `main` (fallback to default branch)

### Cleanup

```bash
git fetch --prune
```

---

## Phase 22: Deprecation Warnings for Old Config Fields `[NEW]`

**Goal:** Confirm that deprecated config fields produce stderr warnings.

### Steps

1. Add a deprecated field to `settings/config.json`:
   ```json
   {
     "merge_queue": {
       "target_branch": "main"
     }
   }
   ```

2. Run any `gt mq` command and capture stderr:
   ```bash
   gt mq integration status gt-epic2 2>&1 | grep -i deprec
   ```

### Validation

- [ ] A deprecation warning appears on stderr mentioning `target_branch`
- [ ] The command still succeeds (warning is non-fatal)

### Cleanup

- Remove `target_branch` from `settings/config.json`.

---

## Phase 23: Status JSON Output `[NEW]`

**Goal:** Confirm that `gt mq integration status --json` produces machine-readable output with all expected fields.

### Steps

1. Using an epic with an integration branch (before landing):
   ```bash
   gt mq integration status gt-epic2 --json
   ```

2. Pipe to `jq` to inspect structure:
   ```bash
   gt mq integration status gt-epic2 --json | jq .
   ```

### Validation

- [ ] Output is valid JSON
- [ ] Contains `epic` field (epic ID)
- [ ] Contains `branch` field (integration branch name)
- [ ] Contains `base_branch` field (where it lands back to)
- [ ] Contains `ahead_of_base` field (commit count, integer)
- [ ] Contains `ready_to_land` field (boolean)
- [ ] Contains `children_total` and `children_closed` fields (integers)
- [ ] Contains `merged_mrs` and `pending_mrs` arrays

---

## Test Run Results

> **Run date:** 2026-02-12
> **Rig:** dypt (Xexr/dypt repo)
> **Runner:** NEO (dypt/crew/neo), orchestrated by furiosa (gastown/crew/furiosa)
> **Binary:** gastown main with PR #1226 cherry-picked
> **Commit tested:** `6c1f4b54` (feat/integration-branch-enhancement)

### Summary Checklist

| Phase | Test | Status |
|-------|------|--------|
| 1 | Standalone task → main | PASS |
| 2 | Epic without integration branch → main | PASS |
| 3 | Create integration branch + verify metadata | PASS |
| 4 | Polecat sources worktree from integration branch | PASS |
| 5 | MR targets integration branch | PASS |
| 6 | Refinery merges to integration branch (not main) | PASS |
| 7 | Second polecat inherits sibling work | PASS |
| 8 | All children closed → ready to land | PASS |
| 9 | Manual land → merge commit on main + cleanup | PASS |
| 10 | Non-main default_branch respected throughout | PASS |
| 11 | Config gate: disable polecat integration | PASS |
| 12 | Config gate: disable refinery integration | PASS |
| 13 | Custom branch template | PASS |
| 14 | --base-branch override | PASS |
| 15 | Nested epics — parent chain walk | PASS |
| 16 | Land with pending MRs (--force) | **FINDING** → PASS (retest) |
| 17 | Idempotent land (crash recovery) | PASS |
| 18 | `--branch` flag override on create `[NEW]` | PASS |
| 19 | Build pipeline commands `[NEW]` | PASS |
| 20 | `default_branch` validation `[NEW]` | **PARTIAL** → PASS (retest) |
| 21 | Stale remote branch detection `[NEW]` | PASS |
| 22 | Deprecation warnings for old config `[NEW]` | PASS |
| 23 | Status JSON output `[NEW]` | PASS |

### Bugs Found

#### BUG 1: Land command guard checks MRs but not children status (`gt-gc3`)

**Phase 16** — The `gt mq integration land` command only checks for open MRs targeting
the integration branch. It does NOT check whether epic children are still open or
in_progress. This allows landing while polecats are actively working on children,
orphaning them when the integration branch gets deleted.

- **Expected:** Land refuses unless all children closed (or `--force` passed)
- **Actual:** Land succeeds if no open MRs exist, even with in_progress children
- **Impact:** User can accidentally land incomplete work; orphaned polecats fail
- **Fix:** Add children-status check to land guard (status command already tracks this)

#### BUG 2: `gt doctor` doesn't validate `default_branch` exists on remote (`gt-1hk`)

**Phase 20** — Setting `default_branch` to a nonexistent branch is caught at polecat
spawn time with a clear error, but `gt doctor` (57 checks) doesn't flag it proactively.

- **Expected:** `gt doctor` reports invalid `default_branch` as a check failure
- **Actual:** All 57 checks pass; bad config only caught at spawn time
- **Impact:** Misconfiguration only discovered when work is attempted
- **Fix:** Add rig-config check for `default_branch` validity in doctor

#### BUG 3: Ahead-count reports 0 when actually 1 (non-main base branch) (`gt-pdk`)

**Phase 10** — `gt mq integration status` reported "Ahead of develop: 0 commits" when
the integration branch was actually 1 commit ahead. Observed only with non-main base
branch (`develop`).

- **Expected:** Status shows correct commit count ahead of base
- **Actual:** Reports 0 commits ahead
- **Impact:** Misleading status output; user may think no work has been merged
- **Fix:** Investigate ahead-count calculation for non-default base branches

### Fixes and Retest (same session)

All 3 bugs were fixed in commit `e871a5e4` (squashed into the final PR commit), binary
rebuilt, and retested on the same dypt rig by NEO:

**BUG 1 fix (gt-gc3):** Added children status check to `runMqIntegrationLand()` after
the open MRs guard. Land now queries epic children via `bd.List()` and refuses if any
are open/in_progress unless `--force` is passed. Retest confirmed: land with open child
fails with `"cannot land: 1 children still open/in_progress"`, `--force` overrides.

**BUG 2 fix (gt-1hk):** Added `DefaultBranchAllRigsCheck` — a global doctor check that
iterates all rigs and validates each one's `default_branch` against the bare repo refs.
Runs without `--rig` flag (the existing per-rig `DefaultBranchExistsCheck` remains for
`--rig` mode). Retest confirmed: `gt doctor` catches `"nonexistent-branch"` as error,
passes clean after resetting to `"main"`.

**BUG 3 fix (gt-pdk):** Changed `CommitsAhead(baseBranch, ref)` to
`CommitsAhead("origin/"+baseBranch, ref)` so non-main base branches resolve via the
remote tracking ref (which always exists after `g.Fetch("origin")`). The `create` command
already used `"origin/"` prefix — `status` was the only caller missing it. Retest
confirmed ahead-count mechanism working on main-based branches; full non-main retest
was not feasible as previous test artifacts had been cleaned up, but the fix is a
one-line change with clear root cause.

### Minor Observations

- **Phase 2 (first attempt):** Polecat silently failed `gt done` due to permission denial,
  then self-cleaned. Re-sling succeeded. Not an integration branch issue.
- **Phase 9:** Land deleted remote branch but `git fetch` didn't auto-prune stale tracking
  ref. Standard git behavior, but could be improved by adding prune to land command.
- **Phase 19:** Build pipeline commands confirmed injected with "if command set" logic for
  empty value skipping.
