package mail

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectTownRoot(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	townRoot := filepath.Join(tmpDir, "town")
	mayorDir := filepath.Join(townRoot, "mayor")
	rigDir := filepath.Join(townRoot, "gastown", "polecats", "test")

	// Create mayor/town.json marker
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mayorDir, "town.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rigDir, 0755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		startDir string
		want     string
	}{
		{
			name:     "from town root",
			startDir: townRoot,
			want:     townRoot,
		},
		{
			name:     "from rig subdirectory",
			startDir: rigDir,
			want:     townRoot,
		},
		{
			name:     "from mayor directory",
			startDir: mayorDir,
			want:     townRoot,
		},
		{
			name:     "from non-town directory",
			startDir: tmpDir,
			want:     "", // No town.json marker above tmpDir
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectTownRoot(tt.startDir)
			if got != tt.want {
				t.Errorf("detectTownRoot(%q) = %q, want %q", tt.startDir, got, tt.want)
			}
		})
	}
}

func TestIsTownLevelAddress(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{"mayor", true},
		{"mayor/", true},
		{"deacon", true},
		{"deacon/", true},
		{"gastown/refinery", false},
		{"gastown/polecats/Toast", false},
		{"gastown/", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			got := isTownLevelAddress(tt.address)
			if got != tt.want {
				t.Errorf("isTownLevelAddress(%q) = %v, want %v", tt.address, got, tt.want)
			}
		})
	}
}

func TestAddressToSessionID(t *testing.T) {
	tests := []struct {
		address string
		want    string
	}{
		{"mayor", "gt-mayor"},
		{"mayor/", "gt-mayor"},
		{"gastown/refinery", "gt-gastown-refinery"},
		{"gastown/Toast", "gt-gastown-Toast"},
		{"beads/witness", "gt-beads-witness"},
		{"gastown/", ""},   // Empty target
		{"gastown", ""},    // No slash
		{"", ""},           // Empty address
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			got := addressToSessionID(tt.address)
			if got != tt.want {
				t.Errorf("addressToSessionID(%q) = %q, want %q", tt.address, got, tt.want)
			}
		})
	}
}

func TestIsSelfMail(t *testing.T) {
	tests := []struct {
		from string
		to   string
		want bool
	}{
		{"mayor/", "mayor/", true},
		{"mayor", "mayor/", true},
		{"mayor/", "mayor", true},
		{"gastown/Toast", "gastown/Toast", true},
		{"gastown/Toast/", "gastown/Toast", true},
		{"mayor/", "deacon/", false},
		{"gastown/Toast", "gastown/Nux", false},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.from+"->"+tt.to, func(t *testing.T) {
			got := isSelfMail(tt.from, tt.to)
			if got != tt.want {
				t.Errorf("isSelfMail(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestShouldBeWisp(t *testing.T) {
	r := &Router{}

	tests := []struct {
		name    string
		msg     *Message
		want    bool
	}{
		{
			name: "explicit wisp flag",
			msg:  &Message{Subject: "Regular message", Wisp: true},
			want: true,
		},
		{
			name: "POLECAT_STARTED subject",
			msg:  &Message{Subject: "POLECAT_STARTED: Toast"},
			want: true,
		},
		{
			name: "polecat_done subject (lowercase)",
			msg:  &Message{Subject: "polecat_done: work complete"},
			want: true,
		},
		{
			name: "NUDGE subject",
			msg:  &Message{Subject: "NUDGE: check your hook"},
			want: true,
		},
		{
			name: "START_WORK subject",
			msg:  &Message{Subject: "START_WORK: gt-123"},
			want: true,
		},
		{
			name: "regular message",
			msg:  &Message{Subject: "Please review this PR"},
			want: false,
		},
		{
			name: "handoff message (not auto-wisp)",
			msg:  &Message{Subject: "HANDOFF: context notes"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.shouldBeWisp(tt.msg)
			if got != tt.want {
				t.Errorf("shouldBeWisp(%v) = %v, want %v", tt.msg.Subject, got, tt.want)
			}
		})
	}
}

func TestResolveBeadsDir(t *testing.T) {
	// With town root set
	r := NewRouterWithTownRoot("/work/dir", "/home/user/gt")
	got := r.resolveBeadsDir("gastown/Toast")
	want := "/home/user/gt/.beads"
	if got != want {
		t.Errorf("resolveBeadsDir with townRoot = %q, want %q", got, want)
	}

	// Without town root (fallback to workDir)
	r2 := &Router{workDir: "/work/dir", townRoot: ""}
	got2 := r2.resolveBeadsDir("mayor/")
	want2 := "/work/dir/.beads"
	if got2 != want2 {
		t.Errorf("resolveBeadsDir without townRoot = %q, want %q", got2, want2)
	}
}

func TestNewRouterWithTownRoot(t *testing.T) {
	r := NewRouterWithTownRoot("/work/rig", "/home/gt")
	if r.workDir != "/work/rig" {
		t.Errorf("workDir = %q, want '/work/rig'", r.workDir)
	}
	if r.townRoot != "/home/gt" {
		t.Errorf("townRoot = %q, want '/home/gt'", r.townRoot)
	}
}

func TestIsListAddress(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{"list:oncall", true},
		{"list:cleanup/gastown", true},
		{"list:", true}, // Edge case: empty list name (will fail on expand)
		{"mayor/", false},
		{"gastown/witness", false},
		{"listoncall", false}, // Missing colon
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			got := isListAddress(tt.address)
			if got != tt.want {
				t.Errorf("isListAddress(%q) = %v, want %v", tt.address, got, tt.want)
			}
		})
	}
}

func TestParseListName(t *testing.T) {
	tests := []struct {
		address string
		want    string
	}{
		{"list:oncall", "oncall"},
		{"list:cleanup/gastown", "cleanup/gastown"},
		{"list:", ""},
		{"list:alerts", "alerts"},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			got := parseListName(tt.address)
			if got != tt.want {
				t.Errorf("parseListName(%q) = %q, want %q", tt.address, got, tt.want)
			}
		})
	}
}

func TestExpandList(t *testing.T) {
	// Create temp directory with messaging config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write messaging.json with test lists
	configContent := `{
  "type": "messaging",
  "version": 1,
  "lists": {
    "oncall": ["mayor/", "gastown/witness"],
    "cleanup/gastown": ["gastown/witness", "deacon/"]
  }
}`
	if err := os.WriteFile(filepath.Join(configDir, "messaging.json"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewRouterWithTownRoot(tmpDir, tmpDir)

	tests := []struct {
		name      string
		listName  string
		want      []string
		wantErr   bool
		errString string
	}{
		{
			name:     "oncall list",
			listName: "oncall",
			want:     []string{"mayor/", "gastown/witness"},
		},
		{
			name:     "cleanup/gastown list",
			listName: "cleanup/gastown",
			want:     []string{"gastown/witness", "deacon/"},
		},
		{
			name:      "unknown list",
			listName:  "nonexistent",
			wantErr:   true,
			errString: "unknown mailing list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.expandList(tt.listName)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expandList(%q) expected error, got nil", tt.listName)
				} else if tt.errString != "" && !contains(err.Error(), tt.errString) {
					t.Errorf("expandList(%q) error = %v, want containing %q", tt.listName, err, tt.errString)
				}
				return
			}
			if err != nil {
				t.Errorf("expandList(%q) unexpected error: %v", tt.listName, err)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("expandList(%q) = %v, want %v", tt.listName, got, tt.want)
				return
			}
			for i, addr := range got {
				if addr != tt.want[i] {
					t.Errorf("expandList(%q)[%d] = %q, want %q", tt.listName, i, addr, tt.want[i])
				}
			}
		})
	}
}

func TestExpandListNoTownRoot(t *testing.T) {
	r := &Router{workDir: "/tmp", townRoot: ""}
	_, err := r.expandList("oncall")
	if err == nil {
		t.Error("expandList with no townRoot should error")
	}
	if !contains(err.Error(), "no town root") {
		t.Errorf("expandList error = %v, want containing 'no town root'", err)
	}
}

// contains checks if s contains substr (helper for error checking)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
