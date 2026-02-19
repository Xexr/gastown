//go:build integration

// Package cmd contains integration tests for the work queue subsystem.
// These tests exercise queue CLI operations (enqueue, list, status, dispatch
// dry-run, circuit breaker) against a Dolt-server-backed beads DB. No Claude
// credentials, no agent sessions.
//
// Requires a Dolt server on port 3307 (managed by requireDoltServer/cleanupDoltServer).
//
// Run with:
//
//	go test -tags=integration -run 'TestQueue' -timeout 5m -count=1 -v ./internal/cmd/
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
)

// queueTestCounter generates unique prefixes for each test to isolate Dolt
// databases on the shared server. Without this, beads from earlier tests
// leak into later tests (all using the same database).
var queueTestCounter atomic.Int32

// initBeadsDBForServer initializes a beads DB that can operate against the
// shared Dolt test server on port 3307. Uses local init (bd init --prefix)
// which reliably creates the schema, then bd auto-detects the running server
// at runtime for SQL operations.
//
// Note: bd init --server (fresh, not migration) fails to create tables in CI.
// Local init works reliably and bd auto-detects the server for runtime ops.
func initBeadsDBForServer(t *testing.T, dir, prefix string) {
	t.Helper()

	cmd := exec.Command("bd", "init", "--prefix", prefix)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	t.Logf("bd init --prefix %s in %s: exit=%v\n%s", prefix, dir, err, out)
	if err != nil {
		t.Fatalf("bd init failed in %s: %v\n%s", dir, err, out)
	}

	// Create empty issues.jsonl to prevent bd auto-export from corrupting
	// routes.jsonl (same as initBeadsDBWithPrefix does).
	issuesPath := filepath.Join(dir, ".beads", "issues.jsonl")
	if err := os.WriteFile(issuesPath, []byte(""), 0644); err != nil {
		t.Fatalf("create issues.jsonl in %s: %v", dir, err)
	}
}

// setupQueueIntegrationTown creates a minimal town filesystem for queue tests.
// Uses the shared Dolt test server on port 3307 (managed by requireDoltServer)
// for beads databases. No gt install, no Claude credentials, no agent sessions.
//
// Returns (hqPath, rigPath, gtBinary, env).
func setupQueueIntegrationTown(t *testing.T) (hqPath, rigPath, gtBinary string, env []string) {
	t.Helper()

	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed, skipping queue integration test")
	}

	requireDoltServer(t)
	cleanStaleBeadsDatabases(t)
	gtBinary = buildGT(t)

	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	// Configure git/dolt identity in isolated HOME (needed by bd init --server
	// which initializes a git repo inside .beads/).
	configureTestGitIdentity(t, tmpDir)

	// Generate unique prefixes per test to avoid cross-test data leakage on
	// the shared Dolt server. Each test gets its own databases (e.g., beads_h3, beads_r3).
	n := queueTestCounter.Add(1)
	hqPrefix := fmt.Sprintf("h%d", n)
	rigPrefix := fmt.Sprintf("r%d", n)

	hqPath = filepath.Join(tmpDir, "test-hq")
	rigPath = filepath.Join(hqPath, "testrig", "mayor", "rig")

	// --- mayor/ ---
	mayorDir := filepath.Join(hqPath, "mayor")
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		t.Fatalf("mkdir mayor: %v", err)
	}
	writeJSONFile(t, filepath.Join(mayorDir, "town.json"), &config.TownConfig{
		Type:    "town",
		Name:    "test",
		Version: config.CurrentTownVersion,
	})

	rigsConfig := &config.RigsConfig{
		Version: config.CurrentRigsVersion,
		Rigs: map[string]config.RigEntry{
			"testrig": {
				GitURL: "file:///dev/null",
				BeadsConfig: &config.BeadsConfig{
					Prefix: rigPrefix,
				},
			},
		},
	}
	if err := config.SaveRigsConfig(filepath.Join(mayorDir, "rigs.json"), rigsConfig); err != nil {
		t.Fatalf("save rigs.json: %v", err)
	}

	// --- settings/ (written later by configureQueue, create dir now) ---
	if err := os.MkdirAll(filepath.Join(hqPath, "settings"), 0755); err != nil {
		t.Fatalf("mkdir settings: %v", err)
	}

	// --- town-level .beads/ ---
	townBeadsDir := filepath.Join(hqPath, ".beads")
	if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
		t.Fatalf("mkdir town .beads: %v", err)
	}
	routes := []beads.Route{
		{Prefix: hqPrefix + "-", Path: "."},
		{Prefix: rigPrefix + "-", Path: "testrig/mayor/rig"},
	}
	if err := beads.WriteRoutes(townBeadsDir, routes); err != nil {
		t.Fatalf("write routes: %v", err)
	}
	initBeadsDBForServer(t, hqPath, hqPrefix)

	// --- testrig directory (loadRig checks os.Stat on townRoot/<rigName>) ---
	rigDir := filepath.Join(hqPath, "testrig")
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		t.Fatalf("mkdir rigPath: %v", err)
	}
	initBeadsDBForServer(t, rigPath, rigPrefix)

	// Redirect: testrig/.beads/ → mayor/rig/.beads
	// beadsSearchDirs scans townRoot/<dir>/.beads — the redirect lets bd commands
	// from testrig/ resolve to the actual rig beads DB.
	rigBeadsRedirect := filepath.Join(rigDir, ".beads")
	if err := os.MkdirAll(rigBeadsRedirect, 0755); err != nil {
		t.Fatalf("mkdir rig .beads redirect: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rigBeadsRedirect, "redirect"), []byte("mayor/rig/.beads"), 0644); err != nil {
		t.Fatalf("write rig redirect: %v", err)
	}

	// --- Environment ---
	env = cleanQueueTestEnv(tmpDir)

	// Configure queue with defaults
	configureQueue(t, hqPath, true, 10, 3)

	return hqPath, rigPath, gtBinary, env
}

// --------------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------------

// TestQueueCircuitBreakerExclusion verifies that a bead with dispatch_failures
// >= maxDispatchFailures is excluded from queue list and dry-run dispatch.
func TestQueueCircuitBreakerExclusion(t *testing.T) {
	hqPath, rigPath, gtBinary, env := setupQueueIntegrationTown(t)

	// Create a bead and manually set it up as circuit-broken.
	beadID := createTestBead(t, rigPath, "Circuit breaker test")

	// Add gt:queued label
	addBeadLabel(t, beadID, LabelQueued, rigPath)

	// Write queue metadata with dispatch_failures=3 (circuit-broken)
	meta := NewQueueMetadata("testrig")
	meta.DispatchFailures = maxDispatchFailures // 3
	meta.LastFailure = "simulated failure"
	desc := FormatQueueMetadata(meta)
	updateBeadDescription(t, beadID, desc, rigPath)

	// Verify: queue list should exclude this bead
	listed := getQueueList(t, gtBinary, hqPath, env)
	for _, item := range listed {
		if item["id"] == beadID {
			t.Errorf("circuit-broken bead %s should be excluded from queue list", beadID)
		}
	}

	// Verify: queue status should not count this bead
	status := getQueueStatus(t, gtBinary, hqPath, env)
	total := int(status["queued_total"].(float64))
	if total != 0 {
		t.Errorf("queued_total = %d, want 0 (circuit-broken bead excluded)", total)
	}

	// Verify: dry-run dispatch should not pick this bead
	out := runGTCmdOutput(t, gtBinary, hqPath, env, "queue", "run", "--dry-run")
	if strings.Contains(out, beadID) {
		t.Errorf("dry-run dispatch should not mention circuit-broken bead %s", beadID)
	}
}

// TestQueueMissingMetadataQuarantine verifies that a bead with gt:queued
// but no queue metadata gets gt:dispatch-failed on dispatch attempt.
func TestQueueMissingMetadataQuarantine(t *testing.T) {
	hqPath, rigPath, gtBinary, env := setupQueueIntegrationTown(t)

	// Create a bead with gt:queued but NO queue metadata (simulating a
	// manually-labeled bead that bypassed gt sling --queue).
	beadID := createTestBead(t, rigPath, "Missing metadata test")
	addBeadLabel(t, beadID, LabelQueued, rigPath)

	// Run dispatch (non-dry-run). The bead has no metadata so dispatch
	// should quarantine it with gt:dispatch-failed.
	runGTCmdMayFail(t, gtBinary, hqPath, env, "queue", "run", "--batch", "1")

	// Verify: bead should now have gt:dispatch-failed label
	if !beadHasLabel(t, beadID, "gt:dispatch-failed", rigPath) {
		t.Errorf("bead %s should have gt:dispatch-failed label after metadata-less dispatch", beadID)
	}
}

// TestQueueAutoConvoyCreation verifies that gt sling --queue creates an
// auto-convoy, stores the convoy ID in queue metadata, and the convoy is
// resolvable via bd show.
func TestQueueAutoConvoyCreation(t *testing.T) {
	hqPath, rigPath, gtBinary, env := setupQueueIntegrationTown(t)

	beadID := createTestBead(t, rigPath, "Auto convoy test")

	// Enqueue via gt sling --queue
	slingToQueue(t, gtBinary, hqPath, env, beadID, "testrig")

	// Verify: bead should have gt:queued label
	if !beadHasLabel(t, beadID, LabelQueued, rigPath) {
		t.Errorf("bead %s should have gt:queued label after sling --queue", beadID)
	}

	// Verify: description should contain queue metadata with convoy ID
	desc := getBeadDescription(t, beadID, rigPath)
	meta := ParseQueueMetadata(desc)
	if meta == nil {
		t.Fatalf("bead %s has no queue metadata after sling --queue", beadID)
	}
	if meta.TargetRig != "testrig" {
		t.Errorf("target_rig = %q, want %q", meta.TargetRig, "testrig")
	}
	if meta.Convoy == "" {
		t.Fatalf("convoy ID not stored in queue metadata")
	}

	// Verify: convoy is resolvable via bd show from hq
	cmd := exec.Command("bd", "show", meta.Convoy, "--json", "--allow-stale")
	cmd.Dir = hqPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bd show convoy %s failed: %v", meta.Convoy, err)
	}
	var convoys []struct {
		ID        string `json:"id"`
		IssueType string `json:"issue_type"`
	}
	if err := json.Unmarshal(out, &convoys); err != nil {
		t.Fatalf("parse convoy show: %v", err)
	}
	if len(convoys) == 0 {
		t.Fatalf("convoy %s not found via bd show", meta.Convoy)
	}
	if convoys[0].IssueType != "convoy" {
		t.Errorf("convoy issue_type = %q, want %q", convoys[0].IssueType, "convoy")
	}

	// Verify: convoy has a "tracks" dependency pointing to the rig bead.
	// This is the core cross-rig link: convoy lives in HQ DB, bead in rig DB.
	depCmd := exec.Command("bd", "dep", "list", meta.Convoy, "--direction=down", "--type=tracks", "--json")
	depCmd.Dir = hqPath
	depOut, err := depCmd.Output()
	if err != nil {
		t.Fatalf("bd dep list %s --type=tracks failed: %v", meta.Convoy, err)
	}
	var deps []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(depOut, &deps); err != nil {
		t.Fatalf("parse dep list: %v\nraw: %s", err, depOut)
	}
	foundTracked := false
	for _, dep := range deps {
		if dep.ID == beadID {
			foundTracked = true
			break
		}
	}
	if !foundTracked {
		t.Errorf("convoy %s should track bead %s via tracks dep, got deps: %s", meta.Convoy, beadID, depOut)
	}
}

// TestQueueBlockedStatusReporting verifies that queue list correctly reports
// blocked:true/false and queue status reports correct queued_ready count.
func TestQueueBlockedStatusReporting(t *testing.T) {
	hqPath, rigPath, gtBinary, env := setupQueueIntegrationTown(t)

	// Create three beads: one to be ready, one to be blocked, one blocker
	readyID := createTestBead(t, rigPath, "Ready bead")
	blockedID := createTestBead(t, rigPath, "Blocked bead")
	blockerID := createTestBead(t, rigPath, "Blocker bead")

	// Enqueue ready and blocked beads via gt sling --queue
	slingToQueue(t, gtBinary, hqPath, env, readyID, "testrig")
	slingToQueue(t, gtBinary, hqPath, env, blockedID, "testrig")

	// Add blocking dependency: blockerID blocks blockedID
	addBeadDependency(t, blockedID, blockerID, rigPath)

	// Verify: queue list should show both, with correct blocked status
	listed := getQueueList(t, gtBinary, hqPath, env)
	if len(listed) < 2 {
		t.Fatalf("queue list returned %d items, want >= 2", len(listed))
	}

	foundReady := false
	foundBlocked := false
	for _, item := range listed {
		id, _ := item["id"].(string)
		blocked, _ := item["blocked"].(bool)
		switch id {
		case readyID:
			foundReady = true
			if blocked {
				t.Errorf("bead %s should NOT be blocked", readyID)
			}
		case blockedID:
			foundBlocked = true
			if !blocked {
				t.Errorf("bead %s SHOULD be blocked", blockedID)
			}
		}
	}
	if !foundReady {
		t.Errorf("ready bead %s not found in queue list", readyID)
	}
	if !foundBlocked {
		t.Errorf("blocked bead %s not found in queue list", blockedID)
	}

	// Verify: queue status should report correct counts
	status := getQueueStatus(t, gtBinary, hqPath, env)
	total := int(status["queued_total"].(float64))
	ready := int(status["queued_ready"].(float64))
	if total != 2 {
		t.Errorf("queued_total = %d, want 2", total)
	}
	if ready != 1 {
		t.Errorf("queued_ready = %d, want 1", ready)
	}
}

// TestQueueSlingDryRun verifies that gt sling --queue --dry-run has no side
// effects: no label added, no metadata written, no convoy created.
func TestQueueSlingDryRun(t *testing.T) {
	hqPath, rigPath, gtBinary, env := setupQueueIntegrationTown(t)

	beadID := createTestBead(t, rigPath, "Dry run test")

	// Capture description before dry-run
	descBefore := getBeadDescription(t, beadID, rigPath)

	// Run sling --queue --dry-run
	slingToQueue(t, gtBinary, hqPath, env, beadID, "testrig", "--dry-run")

	// Verify: no gt:queued label
	if beadHasLabel(t, beadID, LabelQueued, rigPath) {
		t.Errorf("dry-run should NOT add gt:queued label")
	}

	// Verify: description unchanged
	descAfter := getBeadDescription(t, beadID, rigPath)
	if descAfter != descBefore {
		t.Errorf("dry-run should NOT modify description\nbefore: %q\nafter:  %q", descBefore, descAfter)
	}

	// Verify: no convoy created (HQ beads DB should have no convoy issues)
	cmd := exec.Command("bd", "list", "--type=convoy", "--json")
	cmd.Dir = hqPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bd list convoys failed: %v", err)
	}
	var convoys []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &convoys); err != nil {
		t.Fatalf("parse convoy list: %v", err)
	}
	if len(convoys) != 0 {
		t.Errorf("dry-run should NOT create convoys, found %d", len(convoys))
	}
}

// --------------------------------------------------------------------------
// Multi-rig tests
// --------------------------------------------------------------------------

// setupMultiRigQueueTown creates a town with TWO rigs for cross-rig tests.
// Returns (hqPath, rig1Path, rig2Path, gtBinary, env).
func setupMultiRigQueueTown(t *testing.T) (hqPath, rig1Path, rig2Path, gtBinary string, env []string) {
	t.Helper()

	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed, skipping queue integration test")
	}

	requireDoltServer(t)
	cleanStaleBeadsDatabases(t)
	gtBinary = buildGT(t)

	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	configureTestGitIdentity(t, tmpDir)

	// Unique prefixes: hN for HQ, rN for rig1, sN for rig2.
	n := queueTestCounter.Add(1)
	hqPrefix := fmt.Sprintf("h%d", n)
	rig1Prefix := fmt.Sprintf("r%d", n)
	rig2Prefix := fmt.Sprintf("s%d", n)

	hqPath = filepath.Join(tmpDir, "test-hq")
	rig1Path = filepath.Join(hqPath, "rig1", "mayor", "rig")
	rig2Path = filepath.Join(hqPath, "rig2", "mayor", "rig")

	// --- mayor/ ---
	mayorDir := filepath.Join(hqPath, "mayor")
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		t.Fatalf("mkdir mayor: %v", err)
	}
	writeJSONFile(t, filepath.Join(mayorDir, "town.json"), &config.TownConfig{
		Type:    "town",
		Name:    "test",
		Version: config.CurrentTownVersion,
	})

	rigsConfig := &config.RigsConfig{
		Version: config.CurrentRigsVersion,
		Rigs: map[string]config.RigEntry{
			"rig1": {
				GitURL: "file:///dev/null",
				BeadsConfig: &config.BeadsConfig{
					Prefix: rig1Prefix,
				},
			},
			"rig2": {
				GitURL: "file:///dev/null",
				BeadsConfig: &config.BeadsConfig{
					Prefix: rig2Prefix,
				},
			},
		},
	}
	if err := config.SaveRigsConfig(filepath.Join(mayorDir, "rigs.json"), rigsConfig); err != nil {
		t.Fatalf("save rigs.json: %v", err)
	}

	// --- settings/ ---
	if err := os.MkdirAll(filepath.Join(hqPath, "settings"), 0755); err != nil {
		t.Fatalf("mkdir settings: %v", err)
	}

	// --- town-level .beads/ with routes for all three DBs ---
	townBeadsDir := filepath.Join(hqPath, ".beads")
	if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
		t.Fatalf("mkdir town .beads: %v", err)
	}
	routes := []beads.Route{
		{Prefix: hqPrefix + "-", Path: "."},
		{Prefix: rig1Prefix + "-", Path: "rig1/mayor/rig"},
		{Prefix: rig2Prefix + "-", Path: "rig2/mayor/rig"},
	}
	if err := beads.WriteRoutes(townBeadsDir, routes); err != nil {
		t.Fatalf("write routes: %v", err)
	}
	initBeadsDBForServer(t, hqPath, hqPrefix)

	// --- rig1 ---
	if err := os.MkdirAll(rig1Path, 0755); err != nil {
		t.Fatalf("mkdir rig1Path: %v", err)
	}
	initBeadsDBForServer(t, rig1Path, rig1Prefix)
	// Write routes to rig1's .beads/ so bd can resolve cross-rig IDs (needed for
	// cross-rig dep creation via external refs).
	if err := beads.WriteRoutes(filepath.Join(rig1Path, ".beads"), routes); err != nil {
		t.Fatalf("write rig1 routes: %v", err)
	}
	rig1Redirect := filepath.Join(hqPath, "rig1", ".beads")
	if err := os.MkdirAll(rig1Redirect, 0755); err != nil {
		t.Fatalf("mkdir rig1 redirect: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rig1Redirect, "redirect"), []byte("mayor/rig/.beads"), 0644); err != nil {
		t.Fatalf("write rig1 redirect: %v", err)
	}

	// --- rig2 ---
	if err := os.MkdirAll(rig2Path, 0755); err != nil {
		t.Fatalf("mkdir rig2Path: %v", err)
	}
	initBeadsDBForServer(t, rig2Path, rig2Prefix)
	if err := beads.WriteRoutes(filepath.Join(rig2Path, ".beads"), routes); err != nil {
		t.Fatalf("write rig2 routes: %v", err)
	}
	rig2Redirect := filepath.Join(hqPath, "rig2", ".beads")
	if err := os.MkdirAll(rig2Redirect, 0755); err != nil {
		t.Fatalf("mkdir rig2 redirect: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rig2Redirect, "redirect"), []byte("mayor/rig/.beads"), 0644); err != nil {
		t.Fatalf("write rig2 redirect: %v", err)
	}

	// --- Environment ---
	env = cleanQueueTestEnv(tmpDir)

	// Configure queue with defaults
	configureQueue(t, hqPath, true, 10, 3)

	return hqPath, rig1Path, rig2Path, gtBinary, env
}

// TestQueueMultiRigDispatch verifies that queue list and status correctly
// discover queued beads across multiple rigs. beadsSearchDirs scans all
// rig directories under the town root.
func TestQueueMultiRigDispatch(t *testing.T) {
	hqPath, rig1Path, rig2Path, gtBinary, env := setupMultiRigQueueTown(t)

	// Create one bead in each rig.
	bead1 := createTestBead(t, rig1Path, "Rig1 bead")
	bead2 := createTestBead(t, rig2Path, "Rig2 bead")

	// Enqueue both to their respective rigs.
	slingToQueue(t, gtBinary, hqPath, env, bead1, "rig1")
	slingToQueue(t, gtBinary, hqPath, env, bead2, "rig2")

	// Verify: queue list should find both beads.
	listed := getQueueList(t, gtBinary, hqPath, env)
	found := map[string]bool{}
	for _, item := range listed {
		if id, ok := item["id"].(string); ok {
			found[id] = true
		}
	}
	if !found[bead1] {
		t.Errorf("bead %s (rig1) not found in queue list", bead1)
	}
	if !found[bead2] {
		t.Errorf("bead %s (rig2) not found in queue list", bead2)
	}

	// Verify: queue status should report total=2, ready=2.
	status := getQueueStatus(t, gtBinary, hqPath, env)
	total := int(status["queued_total"].(float64))
	ready := int(status["queued_ready"].(float64))
	if total != 2 {
		t.Errorf("queued_total = %d, want 2", total)
	}
	if ready != 2 {
		t.Errorf("queued_ready = %d, want 2", ready)
	}

	// Verify: dry-run dispatch should mention both beads.
	out := runGTCmdOutput(t, gtBinary, hqPath, env, "queue", "run", "--dry-run")
	if !strings.Contains(out, bead1) {
		t.Errorf("dry-run should mention rig1 bead %s", bead1)
	}
	if !strings.Contains(out, bead2) {
		t.Errorf("dry-run should mention rig2 bead %s", bead2)
	}
}

// --------------------------------------------------------------------------
// Cross-rig container tests
//
// These tests verify that gt queue epic and gt convoy queue correctly
// auto-resolve each child's target rig from its bead ID prefix, enabling
// multi-rig epics and convoys.
// --------------------------------------------------------------------------

// TestQueueMultiRigEpicAutoResolve verifies that gt queue epic auto-resolves
// each child's target rig from its prefix. An epic in rig1 with children in
// rig1 and rig2 should queue each child to its respective rig.
func TestQueueMultiRigEpicAutoResolve(t *testing.T) {
	hqPath, rig1Path, rig2Path, gtBinary, env := setupMultiRigQueueTown(t)

	// Create an epic in rig1.
	epicID := createTestBeadOfType(t, rig1Path, "Multi-rig epic", "epic")

	// Create children in different rigs.
	child1 := createTestBead(t, rig1Path, "Rig1 child")
	child2 := createTestBead(t, rig2Path, "Rig2 child")

	// Link children to epic via depends_on (epic → child).
	// child1 is local to rig1 — resolves directly.
	addBeadDependencyOfType(t, epicID, child1, "depends_on", rig1Path)
	// child2 is in rig2 — resolved via routes.jsonl as an external ref.
	addBeadDependencyOfType(t, epicID, child2, "depends_on", rig1Path)

	// Dry-run: verify auto-rig-resolution routes each child correctly.
	// Uses --dry-run to avoid needing formula infrastructure (mol-polecat-work).
	out := runGTCmdOutput(t, gtBinary, hqPath, env, "queue", "epic", epicID, "--dry-run")

	// Verify: child1 should be routed to rig1
	expected1 := fmt.Sprintf("%s → rig1", child1)
	if !strings.Contains(out, expected1) {
		t.Errorf("epic dry-run should route %s → rig1\noutput: %s", child1, out)
	}

	// Verify: child2 should be routed to rig2
	expected2 := fmt.Sprintf("%s → rig2", child2)
	if !strings.Contains(out, expected2) {
		t.Errorf("epic dry-run should route %s → rig2\noutput: %s", child2, out)
	}

	// Non-dry-run: actually enqueue each child to its auto-resolved rig.
	// Use gt sling per-child (with --hook-raw-bead to skip formula) to verify
	// end-to-end queuing works for beads from different rigs.
	slingToQueue(t, gtBinary, hqPath, env, child1, "rig1")
	slingToQueue(t, gtBinary, hqPath, env, child2, "rig2")

	// Verify: both children should have gt:queued label
	if !beadHasLabel(t, child1, LabelQueued, rig1Path) {
		t.Errorf("child1 %s should have gt:queued label", child1)
	}
	if !beadHasLabel(t, child2, LabelQueued, rig2Path) {
		t.Errorf("child2 %s should have gt:queued label", child2)
	}

	// Verify: queue metadata should show correct target rigs
	desc1 := getBeadDescription(t, child1, rig1Path)
	meta1 := ParseQueueMetadata(desc1)
	if meta1 == nil || meta1.TargetRig != "rig1" {
		t.Errorf("child1 target_rig = %v, want rig1", meta1)
	}
	desc2 := getBeadDescription(t, child2, rig2Path)
	meta2 := ParseQueueMetadata(desc2)
	if meta2 == nil || meta2.TargetRig != "rig2" {
		t.Errorf("child2 target_rig = %v, want rig2", meta2)
	}

	// Verify: queue status should find both children
	status := getQueueStatus(t, gtBinary, hqPath, env)
	total := int(status["queued_total"].(float64))
	if total != 2 {
		t.Errorf("queued_total = %d, want 2", total)
	}
}

// TestQueueMultiRigConvoyAutoResolve verifies that gt convoy queue auto-resolves
// each tracked issue's target rig from its prefix. A convoy in HQ tracking beads
// in rig1 and rig2 should queue each bead to its respective rig.
func TestQueueMultiRigConvoyAutoResolve(t *testing.T) {
	hqPath, rig1Path, rig2Path, gtBinary, env := setupMultiRigQueueTown(t)

	// Create a convoy in HQ (the typical location for convoys).
	convoyID := createTestBeadOfType(t, hqPath, "Multi-rig convoy", "convoy")

	// Create beads in different rigs.
	bead1 := createTestBead(t, rig1Path, "Rig1 tracked bead")
	bead2 := createTestBead(t, rig2Path, "Rig2 tracked bead")

	// Add tracks deps from convoy (HQ) to beads in each rig.
	// bead1 and bead2 are in different DBs — stored as external refs in HQ.
	addBeadDependencyOfType(t, convoyID, bead1, "tracks", hqPath)
	addBeadDependencyOfType(t, convoyID, bead2, "tracks", hqPath)

	// Dry-run: verify auto-rig-resolution routes each bead correctly.
	out := runGTCmdOutput(t, gtBinary, hqPath, env, "convoy", "queue", convoyID, "--dry-run")

	// Verify: bead1 should be routed to rig1
	expected1 := fmt.Sprintf("%s → rig1", bead1)
	if !strings.Contains(out, expected1) {
		t.Errorf("convoy dry-run should route %s → rig1\noutput: %s", bead1, out)
	}

	// Verify: bead2 should be routed to rig2
	expected2 := fmt.Sprintf("%s → rig2", bead2)
	if !strings.Contains(out, expected2) {
		t.Errorf("convoy dry-run should route %s → rig2\noutput: %s", bead2, out)
	}

	// Non-dry-run: actually enqueue each bead to its auto-resolved rig.
	slingToQueue(t, gtBinary, hqPath, env, bead1, "rig1")
	slingToQueue(t, gtBinary, hqPath, env, bead2, "rig2")

	// Verify: both beads should have gt:queued label
	if !beadHasLabel(t, bead1, LabelQueued, rig1Path) {
		t.Errorf("bead1 %s should have gt:queued label", bead1)
	}
	if !beadHasLabel(t, bead2, LabelQueued, rig2Path) {
		t.Errorf("bead2 %s should have gt:queued label", bead2)
	}

	// Verify: queue metadata should show correct target rigs
	desc1 := getBeadDescription(t, bead1, rig1Path)
	meta1 := ParseQueueMetadata(desc1)
	if meta1 == nil || meta1.TargetRig != "rig1" {
		t.Errorf("bead1 target_rig = %v, want rig1", meta1)
	}
	desc2 := getBeadDescription(t, bead2, rig2Path)
	meta2 := ParseQueueMetadata(desc2)
	if meta2 == nil || meta2.TargetRig != "rig2" {
		t.Errorf("bead2 target_rig = %v, want rig2", meta2)
	}

	// Verify: queue status should find both beads
	status := getQueueStatus(t, gtBinary, hqPath, env)
	total := int(status["queued_total"].(float64))
	if total != 2 {
		t.Errorf("queued_total = %d, want 2", total)
	}
}
