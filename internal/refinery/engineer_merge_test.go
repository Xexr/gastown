package refinery

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
)

// testGitRepo sets up a bare "origin" repo and a working clone for Engineer tests.
// Returns (workDir, cleanup). The working clone has an initial commit on "main".
func testGitRepo(t *testing.T) (string, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "engineer-merge-test-*")
	if err != nil {
		t.Fatal(err)
	}

	bareDir := filepath.Join(tmpDir, "origin.git")
	workDir := filepath.Join(tmpDir, "work")

	// Create bare repo with main as default branch
	run(t, tmpDir, "git", "init", "--bare", "--initial-branch=main", bareDir)

	// Clone it as a working copy
	run(t, tmpDir, "git", "clone", bareDir, workDir)

	// Configure git user in working copy
	run(t, workDir, "git", "config", "user.email", "test@test.com")
	run(t, workDir, "git", "config", "user.name", "Test")

	// Create initial commit on main
	writeFile(t, filepath.Join(workDir, "README.md"), "# Test repo\n")
	run(t, workDir, "git", "add", "README.md")
	run(t, workDir, "git", "commit", "-m", "initial commit")
	run(t, workDir, "git", "push", "origin", "main")

	cleanup := func() { os.RemoveAll(tmpDir) }
	return workDir, cleanup
}

// createBranch creates a branch with a commit in the working repo.
func createBranch(t *testing.T, workDir, branch, filename, content, message string) {
	t.Helper()
	run(t, workDir, "git", "checkout", "-b", branch)
	writeFile(t, filepath.Join(workDir, filename), content)
	run(t, workDir, "git", "add", filename)
	run(t, workDir, "git", "commit", "-m", message)
	run(t, workDir, "git", "push", "origin", branch)
	run(t, workDir, "git", "checkout", "main")
}

// createConflictingCommitOnMain creates a commit on main that conflicts with the
// given branch's file.
func createConflictingCommitOnMain(t *testing.T, workDir, filename, content, message string) {
	t.Helper()
	run(t, workDir, "git", "checkout", "main")
	writeFile(t, filepath.Join(workDir, filename), content)
	run(t, workDir, "git", "add", filename)
	run(t, workDir, "git", "commit", "-m", message)
	run(t, workDir, "git", "push", "origin", "main")
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("command %s %v failed in %s: %v\noutput: %s", name, args, dir, err, out.String())
	}
	return strings.TrimSpace(out.String())
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// newTestEngineer creates an Engineer with real git and test config.
// Sets rig to a temp-dir-backed Rig so DefaultBranch() returns "main".
func newTestEngineer(t *testing.T, workDir string, strategy string) *Engineer {
	t.Helper()
	g := git.NewGit(workDir)
	var buf bytes.Buffer
	r := &rig.Rig{Name: "test-rig", Path: workDir}
	return &Engineer{
		git:    g,
		rig:    r,
		config: &MergeQueueConfig{MergeStrategy: strategy, DeleteMergedBranches: true},
		output: &buf,
	}
}

// engineerOutput returns the captured output from a test engineer.
func engineerOutput(eng *Engineer) string {
	if buf, ok := eng.output.(*bytes.Buffer); ok {
		return buf.String()
	}
	return ""
}

// --- Rebase-FF strategy tests ---

func TestDoMerge_RebaseFF_HappyPath(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	createBranch(t, workDir, "feature/test", "feature.go", "package main\n", "feat: add feature")

	eng := newTestEngineer(t, workDir, "rebase-ff")
	result := eng.doMerge(context.Background(), "feature/test", "main", "issue-1")

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.MergeCommit == "" {
		t.Fatal("expected merge commit SHA")
	}
	if result.Conflict {
		t.Fatal("unexpected conflict flag")
	}

	// Verify the commit is on main
	g := git.NewGit(workDir)
	if err := g.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "feature.go")); os.IsNotExist(err) {
		t.Fatal("feature.go not on main after merge")
	}
}

func TestDoMerge_RebaseFF_Conflict(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	// Create branch with a file
	createBranch(t, workDir, "feature/conflict", "conflict.txt", "branch version\n", "feat: branch change")

	// Create conflicting commit on main
	createConflictingCommitOnMain(t, workDir, "conflict.txt", "main version\n", "feat: main change")

	eng := newTestEngineer(t, workDir, "rebase-ff")
	result := eng.doMerge(context.Background(), "feature/conflict", "main", "issue-1")

	if result.Success {
		t.Fatal("expected failure, got success")
	}
	if !result.Conflict {
		t.Fatalf("expected conflict flag, got error: %s", result.Error)
	}
}

func TestDoMerge_RebaseFF_TestFailure(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	createBranch(t, workDir, "feature/failing", "failing.go", "package main\n", "feat: failing")

	eng := newTestEngineer(t, workDir, "rebase-ff")
	eng.config.RunTests = true
	eng.config.TestCommand = "false" // always fails

	result := eng.doMerge(context.Background(), "feature/failing", "main", "issue-1")

	if result.Success {
		t.Fatal("expected failure, got success")
	}
	if !result.TestsFailed {
		t.Fatalf("expected TestsFailed flag, got: conflict=%v error=%s", result.Conflict, result.Error)
	}
}

func TestDoMerge_RebaseFF_IntegrationBranch(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	// Create integration branch on origin
	run(t, workDir, "git", "checkout", "-b", "integration/l0g1x")
	run(t, workDir, "git", "push", "origin", "integration/l0g1x")
	run(t, workDir, "git", "checkout", "main")

	// Create feature branch targeting integration
	createBranch(t, workDir, "feature/int-test", "int-feature.go", "package main\n", "feat: integration feature")

	eng := newTestEngineer(t, workDir, "rebase-ff")
	result := eng.doMerge(context.Background(), "feature/int-test", "integration/l0g1x", "issue-1")

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	// Verify commit is on integration branch, not main
	g := git.NewGit(workDir)
	if err := g.Checkout("integration/l0g1x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "int-feature.go")); os.IsNotExist(err) {
		t.Fatal("int-feature.go not on integration/l0g1x after merge")
	}

	// Verify main does NOT have the file
	if err := g.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "int-feature.go")); !os.IsNotExist(err) {
		t.Fatal("int-feature.go should NOT be on main")
	}
}

// --- Squash strategy tests ---

func TestDoMerge_Squash_HappyPath(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	createBranch(t, workDir, "feature/squash", "squash.go", "package main\n", "feat: squashable")

	eng := newTestEngineer(t, workDir, "squash")
	result := eng.doMerge(context.Background(), "feature/squash", "main", "issue-1")

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.MergeCommit == "" {
		t.Fatal("expected merge commit SHA")
	}

	// Verify the file is on main
	g := git.NewGit(workDir)
	if err := g.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "squash.go")); os.IsNotExist(err) {
		t.Fatal("squash.go not on main after merge")
	}
}

func TestDoMerge_Squash_Conflict(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	createBranch(t, workDir, "feature/sq-conflict", "sq-conflict.txt", "branch version\n", "feat: branch")
	createConflictingCommitOnMain(t, workDir, "sq-conflict.txt", "main version\n", "feat: main")

	eng := newTestEngineer(t, workDir, "squash")
	result := eng.doMerge(context.Background(), "feature/sq-conflict", "main", "issue-1")

	if result.Success {
		t.Fatal("expected failure, got success")
	}
	if !result.Conflict {
		t.Fatalf("expected conflict flag, got error: %s", result.Error)
	}
}

// --- Branch deletion tests ---

func TestDoMerge_RebaseFF_BranchNotDeleted(t *testing.T) {
	// doMerge itself does NOT delete branches — that's HandleMRInfoSuccess's job.
	// Verify the branch still exists after doMerge succeeds.
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	createBranch(t, workDir, "feature/keep", "keep.go", "package main\n", "feat: keep branch")

	eng := newTestEngineer(t, workDir, "rebase-ff")
	result := eng.doMerge(context.Background(), "feature/keep", "main", "issue-1")

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	// Branch should still exist locally (doMerge doesn't delete)
	g := git.NewGit(workDir)
	exists, err := g.BranchExists("feature/keep")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("feature/keep should still exist after doMerge (deletion is HandleMRInfoSuccess's job)")
	}
}

// --- Default strategy test ---

func TestDoMerge_DefaultStrategy_IsRebaseFF(t *testing.T) {
	cfg := DefaultMergeQueueConfig()
	if cfg.MergeStrategy != "rebase-ff" {
		t.Errorf("expected default MergeStrategy to be 'rebase-ff', got %q", cfg.MergeStrategy)
	}
}

func TestDoMerge_BranchNotFound(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	eng := newTestEngineer(t, workDir, "rebase-ff")
	result := eng.doMerge(context.Background(), "nonexistent/branch", "main", "issue-1")

	if result.Success {
		t.Fatal("expected failure for nonexistent branch")
	}
	if result.Conflict {
		t.Fatal("should not be a conflict — branch doesn't exist")
	}
}

// --- Multi-commit branch test ---

func TestDoMerge_RebaseFF_MultiCommitBranch(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	// Create branch with multiple commits
	run(t, workDir, "git", "checkout", "-b", "feature/multi")
	writeFile(t, filepath.Join(workDir, "file1.go"), "package main // file1\n")
	run(t, workDir, "git", "add", "file1.go")
	run(t, workDir, "git", "commit", "-m", "feat: add file1")
	writeFile(t, filepath.Join(workDir, "file2.go"), "package main // file2\n")
	run(t, workDir, "git", "add", "file2.go")
	run(t, workDir, "git", "commit", "-m", "feat: add file2")
	writeFile(t, filepath.Join(workDir, "file3.go"), "package main // file3\n")
	run(t, workDir, "git", "add", "file3.go")
	run(t, workDir, "git", "commit", "-m", "feat: add file3")
	run(t, workDir, "git", "push", "origin", "feature/multi")
	run(t, workDir, "git", "checkout", "main")

	eng := newTestEngineer(t, workDir, "rebase-ff")
	result := eng.doMerge(context.Background(), "feature/multi", "main", "issue-1")

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	// Verify all 3 files are on main
	g := git.NewGit(workDir)
	if err := g.Checkout("main"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"file1.go", "file2.go", "file3.go"} {
		if _, err := os.Stat(filepath.Join(workDir, f)); os.IsNotExist(err) {
			t.Fatalf("%s not on main after merge", f)
		}
	}
}

func TestDoMerge_RebaseFF_MultiCommitConflictMidRebase(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	// Create branch with 2 commits, second one conflicts
	run(t, workDir, "git", "checkout", "-b", "feature/multi-conflict")
	writeFile(t, filepath.Join(workDir, "safe.go"), "package main // safe\n")
	run(t, workDir, "git", "add", "safe.go")
	run(t, workDir, "git", "commit", "-m", "feat: safe file")
	writeFile(t, filepath.Join(workDir, "clash.txt"), "branch version\n")
	run(t, workDir, "git", "add", "clash.txt")
	run(t, workDir, "git", "commit", "-m", "feat: clashing file")
	run(t, workDir, "git", "push", "origin", "feature/multi-conflict")
	run(t, workDir, "git", "checkout", "main")

	// Create conflict on main
	createConflictingCommitOnMain(t, workDir, "clash.txt", "main version\n", "feat: main clash")

	eng := newTestEngineer(t, workDir, "rebase-ff")
	result := eng.doMerge(context.Background(), "feature/multi-conflict", "main", "issue-1")

	if result.Success {
		t.Fatal("expected failure")
	}
	if !result.Conflict {
		t.Fatalf("expected conflict flag, got error: %s", result.Error)
	}
}

// --- Working directory state tests ---

func TestDoMerge_RebaseFF_ConflictRestoresTargetBranch(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	createBranch(t, workDir, "feature/wd-test", "wd.txt", "branch\n", "feat: branch")
	createConflictingCommitOnMain(t, workDir, "wd.txt", "main\n", "feat: main")

	eng := newTestEngineer(t, workDir, "rebase-ff")
	result := eng.doMerge(context.Background(), "feature/wd-test", "main", "issue-1")

	if !result.Conflict {
		t.Fatal("expected conflict")
	}

	// Verify we're back on main, not stuck on feature branch
	g := git.NewGit(workDir)
	currentBranch, err := g.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if currentBranch != "main" {
		t.Fatalf("expected working dir on 'main' after conflict, got %q", currentBranch)
	}
}

// --- Unknown strategy test ---

func TestDoMerge_UnknownStrategy_ReturnsError(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	createBranch(t, workDir, "feature/unknown", "x.go", "package main\n", "feat: x")

	eng := newTestEngineer(t, workDir, "cherry-pick")
	result := eng.doMerge(context.Background(), "feature/unknown", "main", "issue-1")

	if result.Success {
		t.Fatal("expected failure for unknown strategy")
	}
	if !strings.Contains(result.Error, "unknown merge strategy") {
		t.Fatalf("expected 'unknown merge strategy' error, got: %s", result.Error)
	}
}

// --- Config parsing test ---

func TestLoadConfig_MergeStrategy(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engineer-config-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	config := `{
		"type": "rig",
		"version": 1,
		"name": "test-rig",
		"merge_queue": {
			"enabled": true,
			"merge_strategy": "squash"
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	r := &rig.Rig{Name: "test-rig", Path: tmpDir}
	e := NewEngineer(r)
	if err := e.LoadConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if e.config.MergeStrategy != "squash" {
		t.Errorf("expected MergeStrategy 'squash', got %q", e.config.MergeStrategy)
	}
}

func TestLoadConfig_MergeStrategy_DefaultsToRebaseFF(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "engineer-config-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Config without merge_strategy — should use default
	config := `{
		"type": "rig",
		"version": 1,
		"merge_queue": {
			"enabled": true
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	r := &rig.Rig{Name: "test-rig", Path: tmpDir}
	e := NewEngineer(r)
	if err := e.LoadConfig(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if e.config.MergeStrategy != "rebase-ff" {
		t.Errorf("expected default MergeStrategy 'rebase-ff', got %q", e.config.MergeStrategy)
	}
}

// --- Push failure test ---

func TestDoMerge_RebaseFF_PushFailure(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	createBranch(t, workDir, "feature/push-fail", "pf.go", "package main\n", "feat: push-fail")

	// Install a pre-receive hook on the bare repo that rejects all pushes.
	// This allows fetch to work but push will fail.
	bareDir := filepath.Join(filepath.Dir(workDir), "origin.git")
	hookDir := filepath.Join(bareDir, "hooks")
	os.MkdirAll(hookDir, 0755)
	hookPath := filepath.Join(hookDir, "pre-receive")
	os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0755)

	eng := newTestEngineer(t, workDir, "rebase-ff")
	result := eng.doMerge(context.Background(), "feature/push-fail", "main", "issue-1")

	// Rebase and ff-merge succeed locally, but push is rejected by hook
	if result.Success {
		t.Fatal("expected failure from push")
	}
	if result.Conflict {
		t.Fatal("should not be a conflict")
	}
	if result.TestsFailed {
		t.Fatal("should not be a test failure")
	}
	if !strings.Contains(result.Error, "failed to push") {
		t.Fatalf("expected push error, got: %s", result.Error)
	}
}

// --- HandleMRInfoSuccess integration branch logic ---

func TestHandleMRInfoSuccess_IntegrationBranch_OutputMessage(t *testing.T) {
	// Test the branch comparison logic without needing full beads.
	// We verify the output message contains the expected integration branch text.
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	eng := newTestEngineer(t, workDir, "rebase-ff")
	// rig.DefaultBranch() returns "main" (no config file)

	// Simulate: MR targeting integration branch
	mr := &MRInfo{
		ID:          "gt-test",
		Branch:      "feature/int",
		Target:      "integration/l0g1x", // NOT "main"
		SourceIssue: "gt-issue",
	}
	result := ProcessResult{Success: true, MergeCommit: "abc12345"}

	// HandleMRInfoSuccess will fail on beads calls (no real beads),
	// but the source-issue logic runs after beads — check output for
	// the integration branch message.
	eng.HandleMRInfoSuccess(mr, result)

	output := engineerOutput(eng)
	if !strings.Contains(output, "left open (merged to integration branch integration/l0g1x)") {
		t.Fatalf("expected integration branch message in output, got:\n%s", output)
	}
	if strings.Contains(output, "Closed source issue") {
		t.Fatal("source issue should NOT be closed for integration branch merge")
	}
}

func TestHandleMRInfoSuccess_DefaultBranch_DoesNotLeaveOpen(t *testing.T) {
	workDir, cleanup := testGitRepo(t)
	defer cleanup()

	eng := newTestEngineer(t, workDir, "rebase-ff")

	// MR targeting default branch
	mr := &MRInfo{
		ID:          "gt-test",
		Branch:      "feature/default",
		Target:      "main", // matches DefaultBranch()
		SourceIssue: "gt-issue",
	}
	result := ProcessResult{Success: true, MergeCommit: "abc12345"}

	eng.HandleMRInfoSuccess(mr, result)

	output := engineerOutput(eng)
	// With nil beads, the close is skipped (no beads to call), but crucially
	// it must NOT take the "left open" integration branch path.
	if strings.Contains(output, "left open") {
		t.Fatal("source issue should NOT be 'left open' for default branch merge")
	}
}
