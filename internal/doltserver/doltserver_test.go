package doltserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestFindMigratableDatabases_FollowsRedirect(t *testing.T) {
	// Setup: simulate a town with a rig that uses a redirect
	townRoot := t.TempDir()

	// Create rig directory with .beads/redirect -> mayor/rig/.beads
	rigName := "nexus"
	rigDir := filepath.Join(townRoot, rigName)
	rigBeadsDir := filepath.Join(rigDir, ".beads")
	if err := os.MkdirAll(rigBeadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write redirect file
	redirectPath := filepath.Join(rigBeadsDir, "redirect")
	if err := os.WriteFile(redirectPath, []byte("mayor/rig/.beads\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create the actual Dolt database at the redirected location
	actualDoltDir := filepath.Join(rigDir, "mayor", "rig", ".beads", "dolt", "beads", ".dolt")
	if err := os.MkdirAll(actualDoltDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create .dolt-data directory (required by DefaultConfig)
	doltDataDir := filepath.Join(townRoot, ".dolt-data")
	if err := os.MkdirAll(doltDataDir, 0755); err != nil {
		t.Fatal(err)
	}

	migrations := FindMigratableDatabases(townRoot)

	// Should find the rig database via redirect
	found := false
	for _, m := range migrations {
		if m.RigName == rigName {
			found = true
			expectedSource := filepath.Join(rigDir, "mayor", "rig", ".beads", "dolt", "beads")
			if m.SourcePath != expectedSource {
				t.Errorf("SourcePath = %q, want %q", m.SourcePath, expectedSource)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected to find migration for rig %q via redirect, got migrations: %v", rigName, migrations)
	}
}

func TestFindMigratableDatabases_NoRedirect(t *testing.T) {
	// Setup: rig with direct .beads/dolt/beads (no redirect)
	townRoot := t.TempDir()

	rigName := "simple"
	doltDir := filepath.Join(townRoot, rigName, ".beads", "dolt", "beads", ".dolt")
	if err := os.MkdirAll(doltDir, 0755); err != nil {
		t.Fatal(err)
	}

	doltDataDir := filepath.Join(townRoot, ".dolt-data")
	if err := os.MkdirAll(doltDataDir, 0755); err != nil {
		t.Fatal(err)
	}

	migrations := FindMigratableDatabases(townRoot)

	found := false
	for _, m := range migrations {
		if m.RigName == rigName {
			found = true
			expectedSource := filepath.Join(townRoot, rigName, ".beads", "dolt", "beads")
			if m.SourcePath != expectedSource {
				t.Errorf("SourcePath = %q, want %q", m.SourcePath, expectedSource)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected to find migration for rig %q, got migrations: %v", rigName, migrations)
	}
}

func TestEnsureMetadata_HQ(t *testing.T) {
	townRoot := t.TempDir()

	// Create .beads directory
	beadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write existing metadata without dolt config
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"),
		[]byte(`{"database": "beads.db", "custom_field": "preserved"}`), 0600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureMetadata(townRoot, "hq"); err != nil {
		t.Fatalf("EnsureMetadata failed: %v", err)
	}

	// Read and verify
	data, err := os.ReadFile(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		t.Fatalf("reading metadata: %v", err)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("parsing metadata: %v", err)
	}

	if metadata["backend"] != "dolt" {
		t.Errorf("backend = %v, want dolt", metadata["backend"])
	}
	if metadata["dolt_mode"] != "server" {
		t.Errorf("dolt_mode = %v, want server", metadata["dolt_mode"])
	}
	if metadata["dolt_database"] != "hq" {
		t.Errorf("dolt_database = %v, want hq", metadata["dolt_database"])
	}
	if metadata["custom_field"] != "preserved" {
		t.Errorf("custom_field was not preserved: %v", metadata["custom_field"])
	}
}

func TestEnsureMetadata_Rig(t *testing.T) {
	townRoot := t.TempDir()

	// Create rig with mayor/rig/.beads
	beadsDir := filepath.Join(townRoot, "myrig", "mayor", "rig", ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureMetadata(townRoot, "myrig"); err != nil {
		t.Fatalf("EnsureMetadata failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		t.Fatalf("reading metadata: %v", err)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("parsing metadata: %v", err)
	}

	if metadata["backend"] != "dolt" {
		t.Errorf("backend = %v, want dolt", metadata["backend"])
	}
	if metadata["dolt_database"] != "myrig" {
		t.Errorf("dolt_database = %v, want myrig", metadata["dolt_database"])
	}
	if metadata["jsonl_export"] != "issues.jsonl" {
		t.Errorf("jsonl_export = %v, want issues.jsonl", metadata["jsonl_export"])
	}
}

func TestEnsureMetadata_Idempotent(t *testing.T) {
	townRoot := t.TempDir()

	beadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Run twice
	if err := EnsureMetadata(townRoot, "hq"); err != nil {
		t.Fatalf("first EnsureMetadata failed: %v", err)
	}
	if err := EnsureMetadata(townRoot, "hq"); err != nil {
		t.Fatalf("second EnsureMetadata failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		t.Fatalf("reading metadata: %v", err)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("parsing metadata: %v", err)
	}

	if metadata["dolt_database"] != "hq" {
		t.Errorf("dolt_database = %v, want hq", metadata["dolt_database"])
	}
}

func TestEnsureAllMetadata(t *testing.T) {
	townRoot := t.TempDir()

	// Create two databases in .dolt-data
	for _, name := range []string{"hq", "myrig"} {
		doltDir := filepath.Join(townRoot, ".dolt-data", name, ".dolt")
		if err := os.MkdirAll(doltDir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Create beads dirs
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, "myrig", "mayor", "rig", ".beads"), 0755); err != nil {
		t.Fatal(err)
	}

	updated, errs := EnsureAllMetadata(townRoot)
	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(updated) != 2 {
		t.Errorf("expected 2 updated, got %d: %v", len(updated), updated)
	}
}

func TestFindRigBeadsDir(t *testing.T) {
	townRoot := t.TempDir()

	// Test HQ
	if dir := findRigBeadsDir(townRoot, "hq"); dir != filepath.Join(townRoot, ".beads") {
		t.Errorf("hq beads dir = %q, want %q", dir, filepath.Join(townRoot, ".beads"))
	}

	// Test rig with mayor/rig/.beads
	mayorBeads := filepath.Join(townRoot, "myrig", "mayor", "rig", ".beads")
	if err := os.MkdirAll(mayorBeads, 0755); err != nil {
		t.Fatal(err)
	}
	if dir := findRigBeadsDir(townRoot, "myrig"); dir != mayorBeads {
		t.Errorf("myrig beads dir = %q, want %q", dir, mayorBeads)
	}

	// Test rig with only rig-root .beads
	rigBeads := filepath.Join(townRoot, "otherrig", ".beads")
	if err := os.MkdirAll(rigBeads, 0755); err != nil {
		t.Fatal(err)
	}
	if dir := findRigBeadsDir(townRoot, "otherrig"); dir != rigBeads {
		t.Errorf("otherrig beads dir = %q, want %q", dir, rigBeads)
	}
}

func TestMoveDir_SameFilesystem(t *testing.T) {
	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "src")
	dest := filepath.Join(tmpDir, "dest")

	// Create source with nested content
	if err := os.MkdirAll(filepath.Join(src, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "subdir", "nested.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := moveDir(src, dest); err != nil {
		t.Fatalf("moveDir failed: %v", err)
	}

	// Source should be gone
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source directory still exists after move")
	}

	// Dest should have the content
	data, err := os.ReadFile(filepath.Join(dest, "file.txt"))
	if err != nil {
		t.Fatalf("reading moved file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("file content = %q, want %q", string(data), "hello")
	}

	data, err = os.ReadFile(filepath.Join(dest, "subdir", "nested.txt"))
	if err != nil {
		t.Fatalf("reading moved nested file: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("nested file content = %q, want %q", string(data), "world")
	}
}

func TestMigrateRigFromBeads(t *testing.T) {
	townRoot := t.TempDir()

	// Create source database
	rigName := "testrig"
	sourcePath := filepath.Join(townRoot, rigName, ".beads", "dolt", "beads")
	if err := os.MkdirAll(filepath.Join(sourcePath, ".dolt"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourcePath, ".dolt", "config.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Create beads dir for metadata
	beadsDir := filepath.Join(townRoot, rigName, "mayor", "rig", ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := MigrateRigFromBeads(townRoot, rigName, sourcePath); err != nil {
		t.Fatalf("MigrateRigFromBeads failed: %v", err)
	}

	// Source should be gone
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Errorf("source directory still exists after migration")
	}

	// Target should have .dolt
	targetDir := filepath.Join(townRoot, ".dolt-data", rigName)
	if _, err := os.Stat(filepath.Join(targetDir, ".dolt")); err != nil {
		t.Errorf("target .dolt directory missing: %v", err)
	}

	// config.json should have been migrated
	data, err := os.ReadFile(filepath.Join(targetDir, ".dolt", "config.json"))
	if err != nil {
		t.Fatalf("reading migrated config: %v", err)
	}
	if string(data) != `{}` {
		t.Errorf("config content = %q, want %q", string(data), `{}`)
	}
}

func TestMigrateRigFromBeads_AlreadyExists(t *testing.T) {
	townRoot := t.TempDir()

	rigName := "existing"
	sourcePath := filepath.Join(townRoot, "src", ".beads", "dolt", "beads")
	if err := os.MkdirAll(filepath.Join(sourcePath, ".dolt"), 0755); err != nil {
		t.Fatal(err)
	}

	// Target already exists
	targetDir := filepath.Join(townRoot, ".dolt-data", rigName, ".dolt")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := MigrateRigFromBeads(townRoot, rigName, sourcePath)
	if err == nil {
		t.Fatal("expected error for already-existing target, got nil")
	}
}

func TestHasServerModeMetadata_NoMetadata(t *testing.T) {
	townRoot := t.TempDir()

	// Create empty workspace
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "rigs.json"), []byte(`{"rigs":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	rigs := HasServerModeMetadata(townRoot)
	if len(rigs) != 0 {
		t.Errorf("expected no server-mode rigs, got %v", rigs)
	}
}

func TestHasServerModeMetadata_WithServerMode(t *testing.T) {
	townRoot := t.TempDir()

	// Create town beads with server mode
	beadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	metadata := `{"backend":"dolt","dolt_mode":"server","dolt_database":"hq"}`
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(metadata), 0644); err != nil {
		t.Fatal(err)
	}

	// Create rig with server mode
	rigBeadsDir := filepath.Join(townRoot, "myrig", "mayor", "rig", ".beads")
	if err := os.MkdirAll(rigBeadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	rigMetadata := `{"backend":"dolt","dolt_mode":"server","dolt_database":"myrig"}`
	if err := os.WriteFile(filepath.Join(rigBeadsDir, "metadata.json"), []byte(rigMetadata), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "rigs.json"),
		[]byte(`{"rigs":{"myrig":{}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	rigs := HasServerModeMetadata(townRoot)
	if len(rigs) != 2 {
		t.Errorf("expected 2 server-mode rigs, got %d: %v", len(rigs), rigs)
	}
}

func TestHasServerModeMetadata_MixedModes(t *testing.T) {
	townRoot := t.TempDir()

	// Town beads with server mode
	beadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"),
		[]byte(`{"backend":"dolt","dolt_mode":"server","dolt_database":"hq"}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Rig with sqlite (not server mode)
	rigBeadsDir := filepath.Join(townRoot, "sqliterig", "mayor", "rig", ".beads")
	if err := os.MkdirAll(rigBeadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigBeadsDir, "metadata.json"),
		[]byte(`{"backend":"sqlite"}`), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "rigs.json"),
		[]byte(`{"rigs":{"sqliterig":{}}}`), 0644); err != nil {
		t.Fatal(err)
	}

	rigs := HasServerModeMetadata(townRoot)
	if len(rigs) != 1 {
		t.Errorf("expected 1 server-mode rig (hq only), got %d: %v", len(rigs), rigs)
	}
	if len(rigs) > 0 && rigs[0] != "hq" {
		t.Errorf("expected hq, got %s", rigs[0])
	}
}

func TestCheckServerReachable_NoServer(t *testing.T) {
	townRoot := t.TempDir()

	// CheckServerReachable should fail when no server is listening
	// Using default port 3307 - if a real server is running, skip
	err := CheckServerReachable(townRoot)
	if err == nil {
		t.Skip("A server is actually running on port 3307, cannot test unreachable case")
	}
	if err != nil && !contains(err.Error(), "not reachable") {
		t.Errorf("expected 'not reachable' in error, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestFindMigratableDatabases_SkipsAlreadyMigrated(t *testing.T) {
	townRoot := t.TempDir()

	rigName := "already"
	// Source exists
	sourceDir := filepath.Join(townRoot, rigName, ".beads", "dolt", "beads", ".dolt")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Target also exists (already migrated)
	targetDir := filepath.Join(townRoot, ".dolt-data", rigName, ".dolt")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}

	migrations := FindMigratableDatabases(townRoot)

	for _, m := range migrations {
		if m.RigName == rigName {
			t.Errorf("should not include already-migrated rig %q", rigName)
		}
	}
}

// =============================================================================
// Mid-migration crash recovery tests
// =============================================================================

// TestMidMigrationCrashRecovery_PartialMigration tests that after migrating
// some rigs but not others (simulating a crash), resuming migration completes
// all remaining rigs without corrupting already-migrated ones.
func TestMidMigrationCrashRecovery_PartialMigration(t *testing.T) {
	townRoot := t.TempDir()

	// Create 3 rigs with source databases
	rigs := []string{"rig-alpha", "rig-beta", "rig-gamma"}
	for _, rig := range rigs {
		sourceDolt := filepath.Join(townRoot, rig, ".beads", "dolt", "beads", ".dolt")
		if err := os.MkdirAll(sourceDolt, 0755); err != nil {
			t.Fatal(err)
		}
		// Write a marker file so we can verify data integrity
		marker := filepath.Join(sourceDolt, "marker.txt")
		if err := os.WriteFile(marker, []byte("data-"+rig), 0644); err != nil {
			t.Fatal(err)
		}
		// Create beads dir for metadata
		beadsDir := filepath.Join(townRoot, rig, "mayor", "rig", ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Phase 1: Migrate only the first rig (simulating crash after rig 1)
	migrations := FindMigratableDatabases(townRoot)
	if len(migrations) != 3 {
		t.Fatalf("expected 3 migratable databases, got %d", len(migrations))
	}

	// Migrate only rig-alpha
	for _, m := range migrations {
		if m.RigName == "rig-alpha" {
			if err := MigrateRigFromBeads(townRoot, m.RigName, m.SourcePath); err != nil {
				t.Fatalf("migrating %s: %v", m.RigName, err)
			}
			break
		}
	}

	// Verify rig-alpha is migrated and data intact
	alphaTarget := filepath.Join(townRoot, ".dolt-data", "rig-alpha", ".dolt", "marker.txt")
	data, err := os.ReadFile(alphaTarget)
	if err != nil {
		t.Fatalf("reading migrated alpha marker: %v", err)
	}
	if string(data) != "data-rig-alpha" {
		t.Errorf("alpha marker = %q, want %q", string(data), "data-rig-alpha")
	}

	// Phase 2: Resume migration (find remaining databases)
	remaining := FindMigratableDatabases(townRoot)
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining migratable databases after partial migration, got %d", len(remaining))
	}

	// Verify rig-alpha is NOT in the remaining list
	for _, m := range remaining {
		if m.RigName == "rig-alpha" {
			t.Error("rig-alpha should not appear in remaining migrations")
		}
	}

	// Migrate the rest
	for _, m := range remaining {
		if err := MigrateRigFromBeads(townRoot, m.RigName, m.SourcePath); err != nil {
			t.Fatalf("migrating %s on resume: %v", m.RigName, err)
		}
	}

	// Verify all 3 rigs are now migrated with correct data
	for _, rig := range rigs {
		markerPath := filepath.Join(townRoot, ".dolt-data", rig, ".dolt", "marker.txt")
		data, err := os.ReadFile(markerPath)
		if err != nil {
			t.Fatalf("reading marker for %s: %v", rig, err)
		}
		expected := "data-" + rig
		if string(data) != expected {
			t.Errorf("%s marker = %q, want %q", rig, string(data), expected)
		}
	}

	// No more migratable databases should remain
	final := FindMigratableDatabases(townRoot)
	if len(final) != 0 {
		t.Errorf("expected 0 migratable databases after full migration, got %d", len(final))
	}
}

// TestMidMigrationCrashRecovery_SourceGoneTargetExists tests that if a crash
// happened after the move but before metadata update, the system recognizes
// the rig as already migrated (target exists, source gone).
func TestMidMigrationCrashRecovery_SourceGoneTargetExists(t *testing.T) {
	townRoot := t.TempDir()

	rigName := "crashed-rig"

	// Simulate post-move state: source is gone, target exists
	targetDolt := filepath.Join(townRoot, ".dolt-data", rigName, ".dolt")
	if err := os.MkdirAll(targetDolt, 0755); err != nil {
		t.Fatal(err)
	}

	// Source does NOT exist (was already moved)
	// FindMigratableDatabases should not list this rig
	migrations := FindMigratableDatabases(townRoot)
	for _, m := range migrations {
		if m.RigName == rigName {
			t.Error("should not attempt to re-migrate a rig whose target already exists")
		}
	}

	// EnsureMetadata should still work to repair metadata.json
	beadsDir := filepath.Join(townRoot, rigName, "mayor", "rig", ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureMetadata(townRoot, rigName); err != nil {
		t.Fatalf("EnsureMetadata for crashed rig: %v", err)
	}

	// Verify metadata was written correctly
	metaData, err := os.ReadFile(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		t.Fatalf("reading metadata: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("parsing metadata: %v", err)
	}
	if meta["backend"] != "dolt" {
		t.Errorf("backend = %v, want dolt", meta["backend"])
	}
}

// =============================================================================
// Concurrent access during migration tests
// =============================================================================

// TestConcurrentMetadataAccess tests that concurrent EnsureMetadata calls
// for different rigs don't interfere with each other.
func TestConcurrentMetadataAccess(t *testing.T) {
	townRoot := t.TempDir()

	rigs := []string{"rig-a", "rig-b", "rig-c", "rig-d", "rig-e"}
	for _, rig := range rigs {
		beadsDir := filepath.Join(townRoot, rig, "mayor", "rig", ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, len(rigs))

	for i, rig := range rigs {
		wg.Add(1)
		go func(idx int, rigName string) {
			defer wg.Done()
			errs[idx] = EnsureMetadata(townRoot, rigName)
		}(i, rig)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("EnsureMetadata for %s failed: %v", rigs[i], err)
		}
	}

	// Verify each rig got the correct metadata
	for _, rig := range rigs {
		metaPath := filepath.Join(townRoot, rig, "mayor", "rig", ".beads", "metadata.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			t.Fatalf("reading metadata for %s: %v", rig, err)
		}
		var meta map[string]interface{}
		if err := json.Unmarshal(data, &meta); err != nil {
			t.Fatalf("parsing metadata for %s: %v", rig, err)
		}
		if meta["dolt_database"] != rig {
			t.Errorf("%s: dolt_database = %v, want %s", rig, meta["dolt_database"], rig)
		}
		if meta["backend"] != "dolt" {
			t.Errorf("%s: backend = %v, want dolt", rig, meta["backend"])
		}
	}
}

// TestConcurrentFindMigratableDatabases tests that FindMigratableDatabases
// can be called concurrently (simulating gt status during migration).
func TestConcurrentFindMigratableDatabases(t *testing.T) {
	townRoot := t.TempDir()

	// Create a rig with source database
	rigName := "concurrent-rig"
	sourceDolt := filepath.Join(townRoot, rigName, ".beads", "dolt", "beads", ".dolt")
	if err := os.MkdirAll(sourceDolt, 0755); err != nil {
		t.Fatal(err)
	}
	doltDataDir := filepath.Join(townRoot, ".dolt-data")
	if err := os.MkdirAll(doltDataDir, 0755); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make([][]Migration, 10)

	// Concurrent reads of migratable databases
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = FindMigratableDatabases(townRoot)
		}(i)
	}

	wg.Wait()

	// All results should be consistent
	for i, r := range results {
		if len(r) != 1 {
			t.Errorf("goroutine %d: expected 1 migration, got %d", i, len(r))
		}
	}
}

// TestConcurrentMigrateAndFind tests that FindMigratableDatabases returns
// consistent results even while a migration is in progress (the source is
// being moved and the target is appearing).
func TestConcurrentMigrateAndFind(t *testing.T) {
	townRoot := t.TempDir()

	// Create multiple rigs
	rigs := []string{"mig-a", "mig-b", "mig-c"}
	for _, rig := range rigs {
		sourceDolt := filepath.Join(townRoot, rig, ".beads", "dolt", "beads", ".dolt")
		if err := os.MkdirAll(sourceDolt, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sourceDolt, "config.json"), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		beadsDir := filepath.Join(townRoot, rig, "mayor", "rig", ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	doltDataDir := filepath.Join(townRoot, ".dolt-data")
	if err := os.MkdirAll(doltDataDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Run migration and concurrent finds
	var wg sync.WaitGroup
	findErrs := make([]error, 0)
	var mu sync.Mutex

	// Start concurrent finders
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				migrations := FindMigratableDatabases(townRoot)
				// Should never panic or return corrupt data
				// Count should be between 0 and 3
				if len(migrations) > 3 {
					mu.Lock()
					findErrs = append(findErrs, filepath.ErrBadPattern)
					mu.Unlock()
				}
			}
		}()
	}

	// Migrate concurrently
	for _, rig := range rigs {
		wg.Add(1)
		go func(rigName string) {
			defer wg.Done()
			sourcePath := filepath.Join(townRoot, rigName, ".beads", "dolt", "beads")
			_ = MigrateRigFromBeads(townRoot, rigName, sourcePath)
		}(rig)
	}

	wg.Wait()

	if len(findErrs) > 0 {
		t.Errorf("concurrent finds returned invalid results: %d errors", len(findErrs))
	}

	// After everything settles, should be 0 remaining
	final := FindMigratableDatabases(townRoot)
	if len(final) != 0 {
		t.Errorf("expected 0 migratable databases after concurrent migration, got %d", len(final))
	}
}

// =============================================================================
// Metadata corruption and repair tests
// =============================================================================

// TestEnsureMetadata_RepairsCorruptJSON tests that EnsureMetadata can handle
// a corrupted metadata.json file and overwrite it with correct data.
func TestEnsureMetadata_RepairsCorruptJSON(t *testing.T) {
	townRoot := t.TempDir()

	beadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write corrupt JSON
	metaPath := filepath.Join(beadsDir, "metadata.json")
	if err := os.WriteFile(metaPath, []byte(`{corrupt json!!!`), 0600); err != nil {
		t.Fatal(err)
	}

	// EnsureMetadata should succeed (overwrites corrupt data)
	if err := EnsureMetadata(townRoot, "hq"); err != nil {
		t.Fatalf("EnsureMetadata failed on corrupt file: %v", err)
	}

	// Verify valid JSON was written
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading metadata: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("metadata is not valid JSON after repair: %v", err)
	}
	if meta["backend"] != "dolt" {
		t.Errorf("backend = %v, want dolt", meta["backend"])
	}
	if meta["dolt_database"] != "hq" {
		t.Errorf("dolt_database = %v, want hq", meta["dolt_database"])
	}
}

// TestEnsureMetadata_RepairsEmptyFile tests that an empty metadata.json
// gets properly populated.
func TestEnsureMetadata_RepairsEmptyFile(t *testing.T) {
	townRoot := t.TempDir()

	beadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write empty file
	metaPath := filepath.Join(beadsDir, "metadata.json")
	if err := os.WriteFile(metaPath, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureMetadata(townRoot, "hq"); err != nil {
		t.Fatalf("EnsureMetadata failed on empty file: %v", err)
	}

	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading metadata: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("metadata is not valid JSON after repair: %v", err)
	}
	if meta["backend"] != "dolt" {
		t.Errorf("backend = %v, want dolt", meta["backend"])
	}
}

// TestEnsureMetadata_RepairsWrongBackend tests that metadata.json with
// backend=sqlite gets corrected to dolt.
func TestEnsureMetadata_RepairsWrongBackend(t *testing.T) {
	townRoot := t.TempDir()

	beadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write metadata with wrong backend
	metaPath := filepath.Join(beadsDir, "metadata.json")
	original := map[string]interface{}{
		"backend":  "sqlite",
		"database": "beads.db",
		"custom":   "keep-me",
	}
	data, _ := json.Marshal(original)
	if err := os.WriteFile(metaPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureMetadata(townRoot, "hq"); err != nil {
		t.Fatalf("EnsureMetadata failed: %v", err)
	}

	repaired, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading metadata: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(repaired, &meta); err != nil {
		t.Fatalf("parsing metadata: %v", err)
	}
	if meta["backend"] != "dolt" {
		t.Errorf("backend = %v, want dolt", meta["backend"])
	}
	if meta["dolt_mode"] != "server" {
		t.Errorf("dolt_mode = %v, want server", meta["dolt_mode"])
	}
	if meta["custom"] != "keep-me" {
		t.Errorf("custom field not preserved: %v", meta["custom"])
	}
}

// TestEnsureMetadata_RepairsMissingDoltFields tests that metadata.json
// with backend=dolt but missing dolt_mode/dolt_database gets repaired.
func TestEnsureMetadata_RepairsMissingDoltFields(t *testing.T) {
	townRoot := t.TempDir()

	beadsDir := filepath.Join(townRoot, "myrig", "mayor", "rig", ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write partial dolt metadata (missing dolt_mode and dolt_database)
	metaPath := filepath.Join(beadsDir, "metadata.json")
	partial := map[string]interface{}{
		"backend":  "dolt",
		"database": "dolt",
	}
	data, _ := json.Marshal(partial)
	if err := os.WriteFile(metaPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureMetadata(townRoot, "myrig"); err != nil {
		t.Fatalf("EnsureMetadata failed: %v", err)
	}

	repaired, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading metadata: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(repaired, &meta); err != nil {
		t.Fatalf("parsing metadata: %v", err)
	}
	if meta["dolt_mode"] != "server" {
		t.Errorf("dolt_mode = %v, want server", meta["dolt_mode"])
	}
	if meta["dolt_database"] != "myrig" {
		t.Errorf("dolt_database = %v, want myrig", meta["dolt_database"])
	}
}

// TestEnsureAllMetadata_RepairsAllCorrupt tests that EnsureAllMetadata
// repairs metadata for all known databases, even if some are corrupt.
func TestEnsureAllMetadata_RepairsAllCorrupt(t *testing.T) {
	townRoot := t.TempDir()

	// Create two databases in .dolt-data
	for _, name := range []string{"hq", "corruptrig"} {
		doltDir := filepath.Join(townRoot, ".dolt-data", name, ".dolt")
		if err := os.MkdirAll(doltDir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Create beads dirs with corrupt metadata
	hqBeads := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(hqBeads, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hqBeads, "metadata.json"), []byte(`CORRUPT`), 0600); err != nil {
		t.Fatal(err)
	}

	rigBeads := filepath.Join(townRoot, "corruptrig", "mayor", "rig", ".beads")
	if err := os.MkdirAll(rigBeads, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigBeads, "metadata.json"), []byte(`{invalid`), 0600); err != nil {
		t.Fatal(err)
	}

	updated, errs := EnsureAllMetadata(townRoot)
	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(updated) != 2 {
		t.Errorf("expected 2 updated, got %d: %v", len(updated), updated)
	}

	// Verify both now have valid metadata
	for _, pair := range []struct {
		name string
		path string
	}{
		{"hq", filepath.Join(hqBeads, "metadata.json")},
		{"corruptrig", filepath.Join(rigBeads, "metadata.json")},
	} {
		data, err := os.ReadFile(pair.path)
		if err != nil {
			t.Fatalf("reading %s metadata: %v", pair.name, err)
		}
		var meta map[string]interface{}
		if err := json.Unmarshal(data, &meta); err != nil {
			t.Fatalf("%s metadata invalid JSON after repair: %v", pair.name, err)
		}
		if meta["backend"] != "dolt" {
			t.Errorf("%s: backend = %v, want dolt", pair.name, meta["backend"])
		}
	}
}

// =============================================================================
// Idempotency tests
// =============================================================================

// TestMigrateRigFromBeads_IdempotentDetection tests that running migration
// twice for the same rig: first succeeds, second correctly reports already done.
func TestMigrateRigFromBeads_IdempotentDetection(t *testing.T) {
	townRoot := t.TempDir()

	rigName := "idem-rig"
	sourcePath := filepath.Join(townRoot, rigName, ".beads", "dolt", "beads")
	if err := os.MkdirAll(filepath.Join(sourcePath, ".dolt"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourcePath, ".dolt", "data.txt"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	beadsDir := filepath.Join(townRoot, rigName, "mayor", "rig", ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// First migration succeeds
	if err := MigrateRigFromBeads(townRoot, rigName, sourcePath); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}

	// Verify data is at target
	targetData, err := os.ReadFile(filepath.Join(townRoot, ".dolt-data", rigName, ".dolt", "data.txt"))
	if err != nil {
		t.Fatalf("reading target data: %v", err)
	}
	if string(targetData) != "original" {
		t.Errorf("target data = %q, want %q", string(targetData), "original")
	}

	// Second call: source is gone, target exists → should error
	err = MigrateRigFromBeads(townRoot, rigName, sourcePath)
	if err == nil {
		t.Fatal("expected error on second migration attempt, got nil")
	}

	// Target data should still be intact
	targetData, err = os.ReadFile(filepath.Join(townRoot, ".dolt-data", rigName, ".dolt", "data.txt"))
	if err != nil {
		t.Fatalf("reading target data after second attempt: %v", err)
	}
	if string(targetData) != "original" {
		t.Errorf("target data corrupted after second attempt: %q", string(targetData))
	}
}

// TestFindAndMigrateAll_Idempotent tests the full find-then-migrate workflow
// twice: first run migrates, second run finds nothing to do.
// =============================================================================
// Max connections and admission control tests
// =============================================================================

func TestDefaultConfig_MaxConnections(t *testing.T) {
	townRoot := t.TempDir()
	config := DefaultConfig(townRoot)

	if config.MaxConnections != DefaultMaxConnections {
		t.Errorf("MaxConnections = %d, want %d", config.MaxConnections, DefaultMaxConnections)
	}
	if config.MaxConnections != 50 {
		t.Errorf("DefaultMaxConnections = %d, want 50", config.MaxConnections)
	}
}

func TestHasConnectionCapacity_ZeroMax(t *testing.T) {
	// When MaxConnections is 0, the function should use Dolt default (1000).
	// Since we can't connect to a real server in unit tests, we just verify
	// the function doesn't panic and returns an error (no server).
	townRoot := t.TempDir()

	// Create minimal config structure
	daemonDir := filepath.Join(townRoot, "daemon")
	if err := os.MkdirAll(daemonDir, 0755); err != nil {
		t.Fatal(err)
	}

	// HasConnectionCapacity should return true (optimistic) when query fails
	hasCapacity, _, err := HasConnectionCapacity(townRoot)
	if err == nil {
		t.Skip("Dolt server is actually running, cannot test offline case")
	}
	if !hasCapacity {
		t.Error("expected optimistic true when server is unreachable")
	}
}

func TestFindAndMigrateAll_Idempotent(t *testing.T) {
	townRoot := t.TempDir()

	// Create 2 rigs
	for _, rig := range []string{"idm-a", "idm-b"} {
		sourceDolt := filepath.Join(townRoot, rig, ".beads", "dolt", "beads", ".dolt")
		if err := os.MkdirAll(sourceDolt, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sourceDolt, "config.json"), []byte(`{"rig":"`+rig+`"}`), 0644); err != nil {
			t.Fatal(err)
		}
		beadsDir := filepath.Join(townRoot, rig, "mayor", "rig", ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// First pass: find and migrate all
	pass1 := FindMigratableDatabases(townRoot)
	if len(pass1) != 2 {
		t.Fatalf("pass 1: expected 2 migratable, got %d", len(pass1))
	}
	for _, m := range pass1 {
		if err := MigrateRigFromBeads(townRoot, m.RigName, m.SourcePath); err != nil {
			t.Fatalf("pass 1: migrating %s: %v", m.RigName, err)
		}
	}

	// Update metadata (as gt dolt migrate does)
	updated1, errs1 := EnsureAllMetadata(townRoot)
	if len(errs1) > 0 {
		t.Errorf("pass 1 metadata errors: %v", errs1)
	}
	if len(updated1) != 2 {
		t.Errorf("pass 1: expected 2 metadata updates, got %d", len(updated1))
	}

	// Second pass: find should return empty, metadata update should be harmless
	pass2 := FindMigratableDatabases(townRoot)
	if len(pass2) != 0 {
		t.Errorf("pass 2: expected 0 migratable, got %d", len(pass2))
	}

	updated2, errs2 := EnsureAllMetadata(townRoot)
	if len(errs2) > 0 {
		t.Errorf("pass 2 metadata errors: %v", errs2)
	}
	if len(updated2) != 2 {
		t.Errorf("pass 2: expected 2 metadata updates (idempotent), got %d", len(updated2))
	}

	// Verify data integrity after two passes
	for _, rig := range []string{"idm-a", "idm-b"} {
		configPath := filepath.Join(townRoot, ".dolt-data", rig, ".dolt", "config.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("reading %s config: %v", rig, err)
		}
		expected := `{"rig":"` + rig + `"}`
		if string(data) != expected {
			t.Errorf("%s config = %q, want %q", rig, string(data), expected)
		}

		metaPath := filepath.Join(townRoot, rig, "mayor", "rig", ".beads", "metadata.json")
		metaData, err := os.ReadFile(metaPath)
		if err != nil {
			t.Fatalf("reading %s metadata: %v", rig, err)
		}
		var meta map[string]interface{}
		if err := json.Unmarshal(metaData, &meta); err != nil {
			t.Fatalf("%s metadata invalid: %v", rig, err)
		}
		if meta["backend"] != "dolt" {
			t.Errorf("%s: backend = %v, want dolt", rig, meta["backend"])
		}
	}
}
