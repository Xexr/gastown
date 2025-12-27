package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// MoleculeProgressInfo contains progress information for a molecule instance.
type MoleculeProgressInfo struct {
	RootID       string   `json:"root_id"`
	RootTitle    string   `json:"root_title"`
	MoleculeID   string   `json:"molecule_id,omitempty"`
	TotalSteps   int      `json:"total_steps"`
	DoneSteps    int      `json:"done_steps"`
	InProgress   int      `json:"in_progress_steps"`
	ReadySteps   []string `json:"ready_steps"`
	BlockedSteps []string `json:"blocked_steps"`
	Percent      int      `json:"percent_complete"`
	Complete     bool     `json:"complete"`
}

// MoleculeStatusInfo contains status information for an agent's hook.
type MoleculeStatusInfo struct {
	Target           string                `json:"target"`
	Role             string                `json:"role"`
	HasWork          bool                  `json:"has_work"`
	PinnedBead       *beads.Issue          `json:"pinned_bead,omitempty"`
	AttachedMolecule string                `json:"attached_molecule,omitempty"`
	AttachedAt       string                `json:"attached_at,omitempty"`
	IsWisp           bool                  `json:"is_wisp"`
	Progress         *MoleculeProgressInfo `json:"progress,omitempty"`
	NextAction       string                `json:"next_action,omitempty"`
}

// MoleculeCurrentInfo contains info about what an agent should be working on.
type MoleculeCurrentInfo struct {
	Identity      string `json:"identity"`
	HandoffID     string `json:"handoff_id,omitempty"`
	HandoffTitle  string `json:"handoff_title,omitempty"`
	MoleculeID    string `json:"molecule_id,omitempty"`
	MoleculeTitle string `json:"molecule_title,omitempty"`
	StepsComplete int    `json:"steps_complete"`
	StepsTotal    int    `json:"steps_total"`
	CurrentStepID string `json:"current_step_id,omitempty"`
	CurrentStep   string `json:"current_step,omitempty"`
	Status        string `json:"status"` // "working", "naked", "complete", "blocked"
}

func runMoleculeProgress(cmd *cobra.Command, args []string) error {
	rootID := args[0]

	workDir, err := findLocalBeadsDir()
	if err != nil {
		return fmt.Errorf("not in a beads workspace: %w", err)
	}

	b := beads.New(workDir)

	// Get the root issue
	root, err := b.Show(rootID)
	if err != nil {
		return fmt.Errorf("getting root issue: %w", err)
	}

	// Find all children of the root issue
	children, err := b.List(beads.ListOptions{
		Parent:   rootID,
		Status:   "all",
		Priority: -1,
	})
	if err != nil {
		return fmt.Errorf("listing children: %w", err)
	}

	if len(children) == 0 {
		return fmt.Errorf("no steps found for %s (not a molecule root?)", rootID)
	}

	// Build progress info
	progress := MoleculeProgressInfo{
		RootID:    rootID,
		RootTitle: root.Title,
	}

	// Try to find molecule ID from first child's description
	for _, child := range children {
		if molID := extractMoleculeID(child.Description); molID != "" {
			progress.MoleculeID = molID
			break
		}
	}

	// Build set of closed issue IDs for dependency checking
	closedIDs := make(map[string]bool)
	for _, child := range children {
		if child.Status == "closed" {
			closedIDs[child.ID] = true
		}
	}

	// Categorize steps
	for _, child := range children {
		progress.TotalSteps++

		switch child.Status {
		case "closed":
			progress.DoneSteps++
		case "in_progress":
			progress.InProgress++
		case "open":
			// Check if all dependencies are closed
			allDepsClosed := true
			for _, depID := range child.DependsOn {
				if !closedIDs[depID] {
					allDepsClosed = false
					break
				}
			}

			if len(child.DependsOn) == 0 || allDepsClosed {
				progress.ReadySteps = append(progress.ReadySteps, child.ID)
			} else {
				progress.BlockedSteps = append(progress.BlockedSteps, child.ID)
			}
		}
	}

	// Calculate completion percentage
	if progress.TotalSteps > 0 {
		progress.Percent = (progress.DoneSteps * 100) / progress.TotalSteps
	}
	progress.Complete = progress.DoneSteps == progress.TotalSteps

	// JSON output
	if moleculeJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(progress)
	}

	// Human-readable output
	fmt.Printf("\n%s %s\n\n", style.Bold.Render("🧬 Molecule Progress:"), root.Title)
	fmt.Printf("  Root: %s\n", rootID)
	if progress.MoleculeID != "" {
		fmt.Printf("  Molecule: %s\n", progress.MoleculeID)
	}
	fmt.Println()

	// Progress bar
	barWidth := 20
	filled := (progress.Percent * barWidth) / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	fmt.Printf("  [%s] %d%% (%d/%d)\n\n", bar, progress.Percent, progress.DoneSteps, progress.TotalSteps)

	// Step status
	fmt.Printf("  Done:        %d\n", progress.DoneSteps)
	fmt.Printf("  In Progress: %d\n", progress.InProgress)
	fmt.Printf("  Ready:       %d", len(progress.ReadySteps))
	if len(progress.ReadySteps) > 0 {
		fmt.Printf(" (%s)", strings.Join(progress.ReadySteps, ", "))
	}
	fmt.Println()
	fmt.Printf("  Blocked:     %d\n", len(progress.BlockedSteps))

	if progress.Complete {
		fmt.Printf("\n  %s\n", style.Bold.Render("✓ Molecule complete!"))
	}

	return nil
}

// extractMoleculeID extracts the molecule ID from an issue's description.
func extractMoleculeID(description string) string {
	lines := strings.Split(description, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "instantiated_from:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "instantiated_from:"))
		}
	}
	return ""
}

func runMoleculeStatus(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	// Find town root
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return fmt.Errorf("not in a Gas Town workspace")
	}

	// Determine target agent
	var target string
	var roleCtx RoleContext

	if len(args) > 0 {
		// Explicit target provided
		target = args[0]
	} else {
		// Auto-detect using env-aware role detection
		roleInfo, err := GetRoleWithContext(cwd, townRoot)
		if err != nil {
			return fmt.Errorf("detecting role: %w", err)
		}
		roleCtx = RoleContext{
			Role:     roleInfo.Role,
			Rig:      roleInfo.Rig,
			Polecat:  roleInfo.Polecat,
			TownRoot: townRoot,
			WorkDir:  cwd,
		}
		target = buildAgentIdentity(roleCtx)
		if target == "" {
			return fmt.Errorf("cannot determine agent identity (role: %s)", roleCtx.Role)
		}
	}

	// Find beads directory
	workDir, err := findLocalBeadsDir()
	if err != nil {
		return fmt.Errorf("not in a beads workspace: %w", err)
	}

	b := beads.New(workDir)

	// Find pinned beads for this agent
	pinnedBeads, err := b.List(beads.ListOptions{
		Status:   beads.StatusPinned,
		Assignee: target,
		Priority: -1,
	})
	if err != nil {
		return fmt.Errorf("listing pinned beads: %w", err)
	}

	// Build status info
	status := MoleculeStatusInfo{
		Target:  target,
		Role:    string(roleCtx.Role),
		HasWork: len(pinnedBeads) > 0,
	}

	if len(pinnedBeads) > 0 {
		// Take the first pinned bead (agents typically have one pinned bead)
		status.PinnedBead = pinnedBeads[0]

		// Check for attached molecule
		attachment := beads.ParseAttachmentFields(pinnedBeads[0])
		if attachment != nil {
			status.AttachedMolecule = attachment.AttachedMolecule
			status.AttachedAt = attachment.AttachedAt

			// Check if it's a wisp (look for wisp indicator in description)
			status.IsWisp = strings.Contains(pinnedBeads[0].Description, "wisp: true") ||
				strings.Contains(pinnedBeads[0].Description, "is_wisp: true")

			// Get progress if there's an attached molecule
			if attachment.AttachedMolecule != "" {
				progress, _ := getMoleculeProgressInfo(b, attachment.AttachedMolecule)
				status.Progress = progress

				// Determine next action
				status.NextAction = determineNextAction(status)
			}
		}
	}

	// Determine next action if no work is slung
	if !status.HasWork {
		status.NextAction = "Check inbox for work assignments: gt mail inbox"
	} else if status.AttachedMolecule == "" {
		status.NextAction = "Attach a molecule to start work: gt mol attach <bead-id> <molecule-id>"
	}

	// JSON output
	if moleculeJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	// Human-readable output
	return outputMoleculeStatus(status)
}

// buildAgentIdentity constructs the agent identity string from role context.
func buildAgentIdentity(ctx RoleContext) string {
	switch ctx.Role {
	case RoleMayor:
		return "mayor"
	case RoleDeacon:
		return "deacon"
	case RoleWitness:
		return ctx.Rig + "/witness"
	case RoleRefinery:
		return ctx.Rig + "/refinery"
	case RolePolecat:
		return ctx.Rig + "/" + ctx.Polecat
	case RoleCrew:
		return ctx.Rig + "/crew/" + ctx.Polecat
	default:
		return ""
	}
}

// getMoleculeProgressInfo gets progress info for a molecule instance.
func getMoleculeProgressInfo(b *beads.Beads, moleculeRootID string) (*MoleculeProgressInfo, error) {
	// Get the molecule root issue
	root, err := b.Show(moleculeRootID)
	if err != nil {
		return nil, fmt.Errorf("getting molecule root: %w", err)
	}

	// Find all children of the root issue
	children, err := b.List(beads.ListOptions{
		Parent:   moleculeRootID,
		Status:   "all",
		Priority: -1,
	})
	if err != nil {
		return nil, fmt.Errorf("listing children: %w", err)
	}

	if len(children) == 0 {
		// No children - might be a simple issue, not a molecule
		return nil, nil
	}

	// Build progress info
	progress := &MoleculeProgressInfo{
		RootID:    moleculeRootID,
		RootTitle: root.Title,
	}

	// Try to find molecule ID from first child's description
	for _, child := range children {
		if molID := extractMoleculeID(child.Description); molID != "" {
			progress.MoleculeID = molID
			break
		}
	}

	// Build set of closed issue IDs for dependency checking
	closedIDs := make(map[string]bool)
	for _, child := range children {
		if child.Status == "closed" {
			closedIDs[child.ID] = true
		}
	}

	// Categorize steps
	for _, child := range children {
		progress.TotalSteps++

		switch child.Status {
		case "closed":
			progress.DoneSteps++
		case "in_progress":
			progress.InProgress++
		case "open":
			// Check if all dependencies are closed
			allDepsClosed := true
			for _, depID := range child.DependsOn {
				if !closedIDs[depID] {
					allDepsClosed = false
					break
				}
			}

			if len(child.DependsOn) == 0 || allDepsClosed {
				progress.ReadySteps = append(progress.ReadySteps, child.ID)
			} else {
				progress.BlockedSteps = append(progress.BlockedSteps, child.ID)
			}
		}
	}

	// Calculate completion percentage
	if progress.TotalSteps > 0 {
		progress.Percent = (progress.DoneSteps * 100) / progress.TotalSteps
	}
	progress.Complete = progress.DoneSteps == progress.TotalSteps

	return progress, nil
}

// determineNextAction suggests the next action based on status.
func determineNextAction(status MoleculeStatusInfo) string {
	if status.Progress == nil {
		return ""
	}

	if status.Progress.Complete {
		return "Molecule complete! Close the bead: bd close " + status.PinnedBead.ID
	}

	if status.Progress.InProgress > 0 {
		return "Continue working on in-progress steps"
	}

	if len(status.Progress.ReadySteps) > 0 {
		return fmt.Sprintf("Start next ready step: bd update %s --status=in_progress", status.Progress.ReadySteps[0])
	}

	if len(status.Progress.BlockedSteps) > 0 {
		return "All remaining steps are blocked - waiting on dependencies"
	}

	return ""
}

// outputMoleculeStatus outputs human-readable status.
func outputMoleculeStatus(status MoleculeStatusInfo) error {
	// Header with hook icon
	fmt.Printf("\n%s Hook Status: %s\n", style.Bold.Render("🪝"), status.Target)
	if status.Role != "" && status.Role != "unknown" {
		fmt.Printf("Role: %s\n", status.Role)
	}
	fmt.Println()

	if !status.HasWork {
		fmt.Printf("%s\n", style.Dim.Render("Nothing on hook - no work slung"))
		fmt.Printf("\n%s %s\n", style.Bold.Render("Next:"), status.NextAction)
		return nil
	}

	// Show pinned bead info
	if status.PinnedBead == nil {
		fmt.Printf("%s\n", style.Dim.Render("Work indicated but no bead found"))
		return nil
	}
	fmt.Printf("%s %s: %s\n", style.Bold.Render("📌 Pinned:"), status.PinnedBead.ID, status.PinnedBead.Title)

	// Show attached molecule
	if status.AttachedMolecule != "" {
		molType := "Molecule"
		if status.IsWisp {
			molType = "Wisp"
		}
		fmt.Printf("%s %s: %s\n", style.Bold.Render("🧬 "+molType+":"), status.AttachedMolecule, "")
		if status.AttachedAt != "" {
			fmt.Printf("   Attached: %s\n", status.AttachedAt)
		}
	} else {
		fmt.Printf("%s\n", style.Dim.Render("No molecule attached"))
	}

	// Show progress if available
	if status.Progress != nil {
		fmt.Println()

		// Progress bar
		barWidth := 20
		filled := (status.Progress.Percent * barWidth) / 100
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		fmt.Printf("Progress: [%s] %d%% (%d/%d steps)\n",
			bar, status.Progress.Percent, status.Progress.DoneSteps, status.Progress.TotalSteps)

		// Step breakdown
		fmt.Printf("  Done:        %d\n", status.Progress.DoneSteps)
		fmt.Printf("  In Progress: %d\n", status.Progress.InProgress)
		fmt.Printf("  Ready:       %d", len(status.Progress.ReadySteps))
		if len(status.Progress.ReadySteps) > 0 && len(status.Progress.ReadySteps) <= 3 {
			fmt.Printf(" (%s)", strings.Join(status.Progress.ReadySteps, ", "))
		}
		fmt.Println()
		fmt.Printf("  Blocked:     %d\n", len(status.Progress.BlockedSteps))

		if status.Progress.Complete {
			fmt.Printf("\n%s\n", style.Bold.Render("✓ Molecule complete!"))
		}
	}

	// Next action hint
	if status.NextAction != "" {
		fmt.Printf("\n%s %s\n", style.Bold.Render("Next:"), status.NextAction)
	}

	return nil
}

func runMoleculeCurrent(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}

	// Find town root
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding workspace: %w", err)
	}
	if townRoot == "" {
		return fmt.Errorf("not in a Gas Town workspace")
	}

	// Determine target agent identity
	var target string
	var roleCtx RoleContext

	if len(args) > 0 {
		// Explicit target provided
		target = args[0]
	} else {
		// Auto-detect using env-aware role detection
		roleInfo, err := GetRoleWithContext(cwd, townRoot)
		if err != nil {
			return fmt.Errorf("detecting role: %w", err)
		}
		roleCtx = RoleContext{
			Role:     roleInfo.Role,
			Rig:      roleInfo.Rig,
			Polecat:  roleInfo.Polecat,
			TownRoot: townRoot,
			WorkDir:  cwd,
		}
		target = buildAgentIdentity(roleCtx)
		if target == "" {
			return fmt.Errorf("cannot determine agent identity (role: %s)", roleCtx.Role)
		}
	}

	// Find beads directory
	workDir, err := findLocalBeadsDir()
	if err != nil {
		return fmt.Errorf("not in a beads workspace: %w", err)
	}

	b := beads.New(workDir)

	// Extract role from target for handoff bead lookup
	parts := strings.Split(target, "/")
	role := parts[len(parts)-1]

	// Find handoff bead for this identity
	handoff, err := b.FindHandoffBead(role)
	if err != nil {
		return fmt.Errorf("finding handoff bead: %w", err)
	}

	// Build current info
	info := MoleculeCurrentInfo{
		Identity: target,
	}

	if handoff == nil {
		info.Status = "naked"
		return outputMoleculeCurrent(info)
	}

	info.HandoffID = handoff.ID
	info.HandoffTitle = handoff.Title

	// Check for attached molecule
	attachment := beads.ParseAttachmentFields(handoff)
	if attachment == nil || attachment.AttachedMolecule == "" {
		info.Status = "naked"
		return outputMoleculeCurrent(info)
	}

	info.MoleculeID = attachment.AttachedMolecule

	// Get the molecule root to find its title and children
	molRoot, err := b.Show(attachment.AttachedMolecule)
	if err != nil {
		// Molecule not found - might be a template ID, still report what we have
		info.Status = "working"
		return outputMoleculeCurrent(info)
	}

	info.MoleculeTitle = molRoot.Title

	// Find all children (steps) of the molecule root
	children, err := b.List(beads.ListOptions{
		Parent:   attachment.AttachedMolecule,
		Status:   "all",
		Priority: -1,
	})
	if err != nil {
		// No steps - just an issue, not a molecule instance
		info.Status = "working"
		return outputMoleculeCurrent(info)
	}

	info.StepsTotal = len(children)

	// Build set of closed issue IDs for dependency checking
	closedIDs := make(map[string]bool)
	var inProgressSteps []*beads.Issue
	var readySteps []*beads.Issue

	for _, child := range children {
		switch child.Status {
		case "closed":
			info.StepsComplete++
			closedIDs[child.ID] = true
		case "in_progress":
			inProgressSteps = append(inProgressSteps, child)
		}
	}

	// Find ready steps (open with all deps closed)
	for _, child := range children {
		if child.Status == "open" {
			allDepsClosed := true
			for _, depID := range child.DependsOn {
				if !closedIDs[depID] {
					allDepsClosed = false
					break
				}
			}
			if len(child.DependsOn) == 0 || allDepsClosed {
				readySteps = append(readySteps, child)
			}
		}
	}

	// Determine current step and status
	if info.StepsComplete == info.StepsTotal && info.StepsTotal > 0 {
		info.Status = "complete"
	} else if len(inProgressSteps) > 0 {
		// First in-progress step is the current one
		info.Status = "working"
		info.CurrentStepID = inProgressSteps[0].ID
		info.CurrentStep = inProgressSteps[0].Title
	} else if len(readySteps) > 0 {
		// First ready step is the next to work on
		info.Status = "working"
		info.CurrentStepID = readySteps[0].ID
		info.CurrentStep = readySteps[0].Title
	} else if info.StepsTotal > 0 {
		// Has steps but none ready or in-progress -> blocked
		info.Status = "blocked"
	} else {
		info.Status = "working"
	}

	return outputMoleculeCurrent(info)
}

// outputMoleculeCurrent outputs the current info in the appropriate format.
func outputMoleculeCurrent(info MoleculeCurrentInfo) error {
	if moleculeJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	// Human-readable output matching spec format
	fmt.Printf("Identity: %s\n", info.Identity)

	if info.HandoffID != "" {
		fmt.Printf("Handoff:  %s (%s)\n", info.HandoffID, info.HandoffTitle)
	} else {
		fmt.Printf("Handoff:  %s\n", style.Dim.Render("(none)"))
	}

	if info.MoleculeID != "" {
		if info.MoleculeTitle != "" {
			fmt.Printf("Molecule: %s (%s)\n", info.MoleculeID, info.MoleculeTitle)
		} else {
			fmt.Printf("Molecule: %s\n", info.MoleculeID)
		}
	} else {
		fmt.Printf("Molecule: %s\n", style.Dim.Render("(none attached)"))
	}

	if info.StepsTotal > 0 {
		fmt.Printf("Progress: %d/%d steps complete\n", info.StepsComplete, info.StepsTotal)
	}

	if info.CurrentStepID != "" {
		fmt.Printf("Current:  %s - %s\n", info.CurrentStepID, info.CurrentStep)
	} else if info.Status == "naked" {
		fmt.Printf("Status:   %s\n", style.Dim.Render("naked - awaiting work assignment"))
	} else if info.Status == "complete" {
		fmt.Printf("Status:   %s\n", style.Bold.Render("complete - molecule finished"))
	} else if info.Status == "blocked" {
		fmt.Printf("Status:   %s\n", style.Dim.Render("blocked - waiting on dependencies"))
	}

	return nil
}

// getGitRootForMolStatus returns the git root for hook file lookup.
func getGitRootForMolStatus() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
