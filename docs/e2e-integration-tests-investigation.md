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
Engineer.ProcessMRInfo()             → refinery merges polecat branch → integration branch
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

## 2. Key Architectural Insight: The Engineer is Pure Go

The `refinery.Engineer` (`internal/refinery/engineer.go`) is a regular Go struct:

```go
e := refinery.NewEngineer(r *rig.Rig)
result := e.ProcessMRInfo(ctx, &MRInfo{
    Branch:      "polecat/nux",
    Target:      "integration/gt-abc",
    SourceIssue: "gt-task123",
})
e.HandleMRInfoSuccess(mr, result)
```

It does **NOT** need tmux, daemon, or Claude sessions. It just needs:
- A `rig.Rig` with a `Path` (directory tree: `refinery/rig/` or `mayor/rig/` as git working dir)
- A `beads.Beads` instance (wraps `bd` CLI calls)
- A `git.Git` instance (wraps git operations in a working directory)

This means **we can call the merge queue processor directly from Go test code** without any of the agent orchestration infrastructure. This is the critical insight for automating Part C tests.

---

## 3. Existing Test Infrastructure

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

## 4. Proposed Architecture

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

## 5. Challenges & Mitigations

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
- `auto_land=false` (default) → refinery formula blocks autonomous landing
- `auto_land=true` → refinery is allowed to land when all epic children closed
- Interplay between `auto_land` config, pre-push hook `GT_INTEGRATION_LAND` env var, and formula FORBIDDEN directives

### Challenge 7: Local vs CI runability

**Both should work.** The test scaffolding uses `t.TempDir()`, mock binaries, and local git repos. No external services needed. CI-specific concerns:
- Build tags (`-tags=e2e`) need to be in a workflow
- Timeout budget: Part C lifecycle tests need ~2 min total
- `bd` binary needs to be on PATH (mock or real)

---

## 6. Test Coverage Mapping

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
  1. SetupTestTown(t)               → town with rig, bare git remote
  2. gt mq integration create <epic> → creates integration branch
  3. Simulate polecat work           → git worktree, commit, push branch
  4. Create MR bead (target: integration/...)
  5. Engineer.ProcessMRInfo()        → merges polecat → integration branch
  6. Verify: integration branch has the commit
  7. gt mq integration land          → merges integration → main
  8. Verify: main has the commit, integration branch deleted
```

---

## 7. Open Questions

1. **Mock bd vs real bd?** Mock is simpler and faster. Real bd tests more but needs either flat-file backend or dolt. Recommend starting with mock.

2. **Subprocess vs direct Go calls for Part C?** Recommend **hybrid**: use subprocess for `gt mq integration create/land` (tests the CLI path), use direct Go calls for `Engineer.ProcessMRInfo` (avoids needing a running refinery agent).

3. **New `e2e.yml` workflow vs existing `ci.yml`?** New workflow with path filters — e2e tests only run when integration-related code changes.

4. **Should `testutil` fixtures live in the e2e package or be shared?** Start in `internal/e2e/testutil/`. If other packages need them later, promote to `internal/testutil/`.

---

## 8. References

- Julian's manual test plan: https://gist.github.com/julianknutsen/04217bd547d382ce5c6e37f44d3700bf
- Xexr's adapted test plan: https://gist.github.com/Xexr/f6c6902fe818aad7ccdf645d21e08a49
- Engineer source: `internal/refinery/engineer.go`
- Existing integration tests: `internal/cmd/rig_integration_test.go`
- CI workflow: `.github/workflows/ci.yml`
- Pre-push hook: `.githooks/pre-push`
