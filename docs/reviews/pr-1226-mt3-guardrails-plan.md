# MT-3 Fix: Integration Branch Landing Guardrails

## Context

During Julian's manual testing of PR #1226, the refinery AI (Sonnet 4.5) bypassed the `auto_land=false` setting and landed an integration branch to main using raw `git merge` + `git push`. This is MT-3 — a formula guardrail gap, not a code bug. The AI agent ignored the formula's "nothing to do" instruction and acted autonomously.

This plan implements a three-layer defense-in-depth approach:
- **Layer 1 (Formula + Role)**: Explicit FORBIDDEN directives in both the formula and role template
- **Layer 2 (Git Hook)**: Pre-push hook that blocks integration branch content landing on the default branch
- **Layer 3 (Code)**: `PushWithEnv()` method that only the legitimate `gt mq integration land` code path can use to bypass the hook

Additionally: infrastructure fix to ensure hooks are active on the refinery worktree at creation time (not just after `gt doctor --fix`), documentation updates, and doctor check verification.

**Scope limitation (accepted)**: The pre-push hook detects integration branches by matching `refs/remotes/origin/integration/*`. Custom templates that produce branches outside the `integration/` prefix are not covered by the hook. Cherry-picks (which produce new SHAs) are also not detected. Layer 1 (formula language) is the guardrail for those cases. This is documented.

---

## Commit Strategy

Split into two commits to cleanly isolate the layers:

**Commit 1: Layer 1 — Formula and role template defenses**
- Section 1: Formula FORBIDDEN directives + step language + version bump (both copies)
- Section 2: Role template CARDINAL RULE + check-integration-branches expansion
- This commit is purely instructional — no code changes, no hooks, no runtime behavior changes

**Commit 2: Layers 2+3 — Git hook guardrails and infrastructure**
- Section 3: Enhanced pre-push hook (ancestry-based detection, dynamic default branch)
- Section 4: `runWithEnv()`, `PushWithEnv()`, land command update
- Section 5: `ConfigureHooksPath()` + manager.go infrastructure fix
- Section 7: Documentation (Safety Guardrails section)
- Section 8: All tests (hook tests, PushWithEnv unit test, land integration test)

This separation allows the formula/role changes (soft enforcement) to be reviewed independently from the git hook machinery (hard enforcement).

---

## Changes

### 1. Layer 1a — Formula guardrails (`mol-refinery-patrol.formula.toml`)

**File**: `.beads/formulas/mol-refinery-patrol.formula.toml` + `internal/formula/formulas/mol-refinery-patrol.formula.toml` (both copies)

**A) Top-level FORBIDDEN in formula description** (insert after the variables table, before "Step Execution Order", around line 60):

```
## FORBIDDEN Actions

- FORBIDDEN: Landing integration branches to the default branch via raw git commands (`git merge`, `git push`).
  Integration branches may ONLY be landed via `gt mq integration land <epic-id>`.
  This applies regardless of `auto_land` configuration. The pre-push hook enforces this.
```

**B) Strengthen the `check-integration-branches` step** (line 544-560):

Current wording says: `If auto_land = "false": Say "Auto-land disabled, nothing to do." Close step.`

Add after that line:
```
FORBIDDEN: If auto_land is false, you MUST NOT land integration branches yourself using
raw git commands. Do not merge integration branches to the default/target branch. Do not push
integration branch merges. The auto_land=false setting means landing requires a human
to run `gt mq integration land` manually. Respect this boundary unconditionally.
```

**C) Bump formula version** from 6 to 7.

### 2. Layer 1b — Role template guardrails (`refinery.md.tmpl`)

**File**: `internal/templates/roles/refinery.md.tmpl`

**A) Add to CARDINAL RULE section** (after line 86, the existing FORBIDDEN):

```
- FORBIDDEN: Landing integration branches to the default branch (or any base branch) via raw git commands.
  Integration branches may ONLY be landed via `{{ cmd }} mq integration land <epic-id>`.
  Even if auto_land is configured, the landing MUST go through the CLI command, not raw git.
```

**B) Expand the `check-integration-branches` inline** (line 319):

Replace the current one-liner:
```
**check-integration-branches**: Check if integration branches are ready to land.
```

With:
```
**check-integration-branches**: Check if integration branches are ready to land.
If `auto_land` is false, say "Auto-land disabled" and move on. **FORBIDDEN**: Landing
integration branches yourself via raw git commands. Only `{{ cmd }} mq integration land`
is authorized.
```

### 3. Layer 2 — Enhanced pre-push hook

**File**: `.githooks/pre-push`

Replace the current hook with an enhanced version that:

1. **Detects the default branch dynamically** via `refs/remotes/origin/HEAD` (fallback: `main`)
2. Updates the allowlist to use `${default_branch}` instead of hardcoded `main`
3. **Adds** `integration/*` to the allowlist (so agents CAN push to integration branches)
4. **Adds ancestry-based detection**: When pushing to the default branch, checks whether any `origin/integration/*` tip has become newly reachable
5. **Blocks** unless `GT_INTEGRATION_LAND=1` env var is set
6. Preserves existing `upstream` remote bypass for fork workflows

The detection logic uses **ancestry checking** (not merge-commit scanning), which catches all merge styles — `--no-ff`, `--ff-only`, default merge, and rebase:
```bash
# Detect the rig's default branch dynamically
default_branch=$(git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's#refs/remotes/origin/##')
: "${default_branch:=main}"

# When pushing to default branch, check for integration branch ancestry
if [[ "$branch" == "$default_branch" ]] && [[ "$GT_INTEGRATION_LAND" != "1" ]]; then
  # Skip for deletions/initial pushes
  if [[ "$remote_sha" == "0000000000000000000000000000000000000000" ]] || \
     [[ "$local_sha" == "0000000000000000000000000000000000000000" ]]; then
    continue
  fi
  # Check if any integration branch tip is newly reachable from this push
  for ref in $(git for-each-ref --format='%(objectname)' refs/remotes/origin/integration/); do
    if git merge-base --is-ancestor "$ref" "$local_sha" && \
       ! git merge-base --is-ancestor "$ref" "$remote_sha"; then
      echo "BLOCKED: Push to $default_branch introduces integration branch content."
      echo "Integration branches must be landed via: gt mq integration land <epic-id>"
      exit 1
    fi
  done
fi
```

**Why ancestry-based detection is superior to merge-commit scanning:**
- Catches `git merge --no-ff` (merge commit)
- Catches `git merge --ff-only` (fast-forward, no merge commit)
- Catches `git merge` (default, which may ff or create merge)
- Catches `git rebase` that pulls in integration branch content
- Only misses cherry-picks (different SHAs — acceptable, as cherry-picking is deliberate)

**Why the default branch is detected dynamically:**
- Users may have `master`, `develop`, or another branch as their default
- `refs/remotes/origin/HEAD` is set automatically by `git clone`
- Can be refreshed with `git remote set-head origin --auto`
- Fallback to `main` if symref is not set

### 4. Layer 3 — Code: `PushWithEnv` method

**File**: `internal/git/git.go`

**A) Add `runWithEnv()` private method** (alongside existing `run()` at line 87):

```go
func (g *Git) runWithEnv(args []string, extraEnv []string) (string, error) {
    if g.gitDir != "" {
        args = append([]string{"--git-dir=" + g.gitDir}, args...)
    }
    cmd := exec.Command("git", args...)
    if g.workDir != "" {
        cmd.Dir = g.workDir
    }
    if len(extraEnv) > 0 {
        cmd.Env = append(os.Environ(), extraEnv...)
    }
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    err := cmd.Run()
    if err != nil {
        return "", g.wrapError(err, stdout.String(), stderr.String(), args)
    }
    return strings.TrimSpace(stdout.String()), nil
}
```

**B) Add `PushWithEnv()` public method** (alongside existing `Push()` at line 363):

```go
// PushWithEnv pushes with additional environment variables.
// Used by gt mq integration land to set GT_INTEGRATION_LAND=1, which the
// pre-push hook checks to allow integration branch → main merges.
func (g *Git) PushWithEnv(remote, branch string, force bool, env []string) error {
    args := []string{"push", remote, branch}
    if force {
        args = append(args, "--force")
    }
    _, err := g.runWithEnv(args, env)
    return err
}
```

**C) Update land command to use PushWithEnv**

**File**: `internal/cmd/mq_integration.go` line 655

Change:
```go
if err := landGit.Push("origin", targetBranch, false); err != nil {
```
To:
```go
if err := landGit.PushWithEnv("origin", targetBranch, false, []string{"GT_INTEGRATION_LAND=1"}); err != nil {
```

### 5. Infrastructure — Hook provisioning at worktree creation

**File**: `internal/rig/manager.go` (after line 506)

The refinery is created as a worktree from `.repo.git` via `WorktreeAddExisting()`. Currently no `configureHooksPath()` call follows. Add one.

`configureHooksPath` is unexported (lowercase) in `internal/git/git.go`. Add an exported method on `Git`:

```go
// ConfigureHooksPath sets core.hooksPath for the repo/worktree if .githooks exists.
func (g *Git) ConfigureHooksPath() error {
    return configureHooksPath(g.workDir)
}
```

Then in `manager.go` after worktree creation:
```go
refineryGit := git.NewGit(refineryRigPath)
if err := refineryGit.ConfigureHooksPath(); err != nil {
    return nil, fmt.Errorf("configuring hooks for refinery: %w", err)
}
```

**Why this is safe**:
- `configureHooksPath` checks for `.githooks/` existence first (no-op if missing)
- Idempotent (setting twice is harmless)
- Setting on worktree propagates to `.repo.git` shared config → all worktrees benefit (polecats, land worktree)
- Matches what `gt doctor --fix` already does as remediation
- The land worktree (`.land-worktree`) is also created from `.repo.git`, so it inherits hooks automatically

### 6. Doctor check verification

**File**: `internal/doctor/rig_check.go`

The existing `HooksPathConfiguredCheck` (line 241-360) already:
- Scans `refinery/rig` (line 277) — the affected worktree
- Handles worktrees correctly (`.git` as file passes `os.Stat`)
- Fixes by running `git config core.hooksPath .githooks`

**No changes needed to the doctor check.** It already covers the refinery. The infrastructure fix (step 5) ensures hooks are set at creation time; doctor remains the safety net for existing rigs.

Note: the doctor check scans `polecats/<name>` (one level) but polecat worktrees are at `polecats/<name>/<rigname>/` (two levels). So doctor doesn't actually check polecat worktrees. This is pre-existing and out of scope — polecats don't need hooks for MT-3 purposes, and the shared config propagation from step 5 covers them anyway.

### 7. Documentation

**File**: `docs/concepts/integration-branches.md`

Add a new section after "Auto-Landing" (after line 447) and before "Build Pipeline Configuration":

```markdown
## Safety Guardrails

Integration branch landing is protected by a three-layer defense:

### Layer 1: Formula and Role Instructions

The refinery formula and role template explicitly forbid landing integration
branches via raw git commands. Only `gt mq integration land` is authorized.

### Layer 2: Pre-Push Hook

The `.githooks/pre-push` hook detects when a push to the default branch
introduces integration branch content. It uses ancestry-based detection:
if any `origin/integration/*` branch tip becomes newly reachable from the
pushed commits, the push is blocked unless `GT_INTEGRATION_LAND=1` is set.

The default branch is detected dynamically via `refs/remotes/origin/HEAD`
(fallback: `main`), so this works regardless of the rig's branch naming.

This catches all merge styles: `--no-ff`, `--ff-only`, default merge, and
rebase. Only cherry-picks (which produce new SHAs) are not detected.

**Scope**: This check matches branches under the `integration/` prefix (the
default template). Custom templates that produce branches outside `integration/`
are not covered by the hook — Layer 1 (formula language) is the guardrail for
those cases.

**Requires**: `core.hooksPath` must be configured for the hook to be active.
New rigs get this automatically. Existing rigs: run `gt doctor --fix`.

### Layer 3: Authorized Code Path

The `gt mq integration land` command uses `PushWithEnv()` to set
`GT_INTEGRATION_LAND=1`, allowing the push through the hook. Raw `git push`
from any agent or user does not set this variable and will be blocked.
Manually setting the env var is possible but is not part of the supported
workflow — the variable is a policy-based trust boundary, not a
capability-based security mechanism.

### Why Three Layers?

| Layer | Type | Strength | Limitation |
|-------|------|----------|------------|
| Formula/Role | Soft | Covers all branch patterns | AI agents can ignore instructions |
| Pre-push hook | Hard | Blocks all merge styles at git boundary | Only matches `integration/*` prefix; env var is policy-based |
| Code path | Hard | Land command sets bypass env var | Requires hook to be active |

The layers complement each other. The formula covers custom templates; the hook
provides hard enforcement for default templates (catching merges, fast-forwards,
and rebases via ancestry detection); the code path ensures the CLI command can
bypass the hook.
```

### 8. Tests

**A) Pre-push hook tests** — new file `.githooks/pre-push_test.sh`

Test scenarios (matching PR #8's 9-case plan):
1. Normal push to default branch (no integration content) → allowed
2. Push to `polecat/*` → allowed
3. Push to `integration/*` → allowed
4. Push to `feature/*` without upstream remote → blocked
5. Push to `feature/*` with upstream remote → allowed
6. Push to default branch with integration merge (no env var) → **blocked**
7. Push to default branch with integration merge + `GT_INTEGRATION_LAND=1` → **allowed**
8. Push to default branch with non-integration merge → allowed
9. Tag push → allowed
10. Push to default branch with fast-forward integration merge (no merge commit) → **blocked** (ancestry-based detection catches this)

**B) Unit test for `PushWithEnv`** — add to `internal/git/git_test.go`

Test that `PushWithEnv` passes environment variables to the git subprocess.

**C) Integration test for land with hooks** — add to `internal/cmd/mq_integration_test.go`

Test that `gt mq integration land` succeeds when hooks are active (verifies the `GT_INTEGRATION_LAND=1` bypass works end-to-end).

---

## File Summary

| File | Change |
|------|--------|
| `.beads/formulas/mol-refinery-patrol.formula.toml` | FORBIDDEN directive + step language + version bump |
| `internal/formula/formulas/mol-refinery-patrol.formula.toml` | Same (embedded copy) |
| `internal/templates/roles/refinery.md.tmpl` | CARDINAL RULE addition + check-integration-branches expansion |
| `.githooks/pre-push` | Dynamic default branch, ancestry-based integration detection, GT_INTEGRATION_LAND bypass |
| `.githooks/pre-push_test.sh` | New: 10-scenario test suite for pre-push hook |
| `internal/git/git.go` | New: `runWithEnv()`, `PushWithEnv()`, `ConfigureHooksPath()` |
| `internal/git/git_test.go` | New: test for PushWithEnv |
| `internal/cmd/mq_integration.go` | Line 655: `Push()` → `PushWithEnv()` with env var |
| `internal/cmd/mq_integration_test.go` | New: land-with-hooks integration test |
| `internal/rig/manager.go` | After line 506: `ConfigureHooksPath()` call |
| `docs/concepts/integration-branches.md` | New "Safety Guardrails" section |

---

## Upgrade Path

The pre-push hook is a **git hook** (`.githooks/pre-push`), not a Claude Code hook.
It is distributed via the git repo, not via `gt hooks sync`.

**`gt hooks sync` does not apply here.** That command manages Claude Code hooks
(`settings.local.json` files). The two hook systems are completely separate.

### New rigs (created after upgrade)

No action required. Section 5's `ConfigureHooksPath()` call sets `core.hooksPath`
automatically at worktree creation time. The enhanced pre-push hook is active
immediately.

### Existing rigs (upgrading)

After pulling the new code (`git pull`), users must run:

```bash
gt doctor --fix
```

This sets `core.hooksPath = .githooks` on all clones and worktrees that don't
already have it configured. The updated `.githooks/pre-push` file is already
present from the pull — `gt doctor --fix` just tells git to use it.

### How to verify hooks are active

```bash
gt doctor
```

The `HooksPathConfiguredCheck` reports whether `core.hooksPath` is configured on
each clone (mayor/rig, refinery/rig, crew members, polecats). Green = hooks active.

Users can also test directly:

```bash
# From any clone with hooks active:
git config --get core.hooksPath
# Should output: .githooks
```

---

## Verification

1. **Build**: `SKIP_UPDATE_CHECK=1 make build` — must pass clean
2. **Tests**: `go test ./...` — all existing + new tests pass
3. **Hook manual test**: Create a mock integration branch merge and try `git push` (should block), then try with `GT_INTEGRATION_LAND=1` (should pass)
4. **Formula sync**: Run `go generate` and check `git status` — embedded formula copy must match `.beads/` copy
5. **Doctor check**: Run `gt doctor` on existing rig — should report hooks as configured (or fix them)
