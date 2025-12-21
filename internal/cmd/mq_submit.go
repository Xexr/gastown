package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// branchInfo holds parsed branch information.
type branchInfo struct {
	Branch string // Full branch name
	Issue  string // Issue ID extracted from branch
	Worker string // Worker name (polecat name)
}

// parseBranchName extracts issue ID and worker from a branch name.
// Supports formats:
//   - polecat/<worker>/<issue>  → issue=<issue>, worker=<worker>
//   - <issue>                   → issue=<issue>, worker=""
func parseBranchName(branch string) branchInfo {
	info := branchInfo{Branch: branch}

	// Try polecat/<worker>/<issue> format
	if strings.HasPrefix(branch, "polecat/") {
		parts := strings.SplitN(branch, "/", 3)
		if len(parts) == 3 {
			info.Worker = parts[1]
			info.Issue = parts[2]
			return info
		}
	}

	// Try to find an issue ID pattern in the branch name
	// Common patterns: prefix-xxx, prefix-xxx.n (subtask)
	issuePattern := regexp.MustCompile(`([a-z]+-[a-z0-9]+(?:\.[0-9]+)?)`)
	if matches := issuePattern.FindStringSubmatch(branch); len(matches) > 1 {
		info.Issue = matches[1]
	}

	return info
}

func runMqSubmit(cmd *cobra.Command, args []string) error {
	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Find current rig
	rigName, _, err := findCurrentRig(townRoot)
	if err != nil {
		return err
	}

	// Initialize git for the current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}
	g := git.NewGit(cwd)

	// Get current branch
	branch := mqSubmitBranch
	if branch == "" {
		branch, err = g.CurrentBranch()
		if err != nil {
			return fmt.Errorf("getting current branch: %w", err)
		}
	}

	if branch == "main" || branch == "master" {
		return fmt.Errorf("cannot submit main/master branch to merge queue")
	}

	// Parse branch info
	info := parseBranchName(branch)

	// Override with explicit flags
	issueID := mqSubmitIssue
	if issueID == "" {
		issueID = info.Issue
	}
	worker := info.Worker

	if issueID == "" {
		return fmt.Errorf("cannot determine source issue from branch '%s'; use --issue to specify", branch)
	}

	// Initialize beads for looking up source issue
	bd := beads.New(cwd)

	// Determine target branch
	target := "main"
	if mqSubmitEpic != "" {
		// Explicit --epic flag takes precedence
		target = "integration/" + mqSubmitEpic
	} else {
		// Auto-detect: check if source issue has a parent epic with an integration branch
		autoTarget, err := detectIntegrationBranch(bd, g, issueID)
		if err != nil {
			// Non-fatal: log and continue with main as target
			fmt.Printf("  %s\n", style.Dim.Render(fmt.Sprintf("(note: %v)", err)))
		} else if autoTarget != "" {
			target = autoTarget
		}
	}

	// Get source issue for priority inheritance
	var priority int
	if mqSubmitPriority >= 0 {
		priority = mqSubmitPriority
	} else {
		// Try to inherit from source issue
		sourceIssue, err := bd.Show(issueID)
		if err != nil {
			// Issue not found, use default priority
			priority = 2
		} else {
			priority = sourceIssue.Priority
		}
	}

	// Build title
	title := fmt.Sprintf("Merge: %s", issueID)

	// Build description with MR fields
	mrFields := &beads.MRFields{
		Branch:      branch,
		Target:      target,
		SourceIssue: issueID,
		Worker:      worker,
		Rig:         rigName,
	}
	description := beads.FormatMRFields(mrFields)

	// Create the merge-request issue
	createOpts := beads.CreateOptions{
		Title:       title,
		Type:        "merge-request",
		Priority:    priority,
		Description: description,
	}

	issue, err := bd.Create(createOpts)
	if err != nil {
		return fmt.Errorf("creating merge request: %w", err)
	}

	// Success output
	fmt.Printf("%s Created merge request\n", style.Bold.Render("✓"))
	fmt.Printf("  MR ID: %s\n", style.Bold.Render(issue.ID))
	fmt.Printf("  Source: %s\n", branch)
	fmt.Printf("  Target: %s\n", target)
	fmt.Printf("  Issue: %s\n", issueID)
	if worker != "" {
		fmt.Printf("  Worker: %s\n", worker)
	}
	fmt.Printf("  Priority: P%d\n", priority)

	return nil
}

// detectIntegrationBranch checks if an issue is a child of an epic that has an integration branch.
// Returns the integration branch target (e.g., "integration/gt-epic") if found, or "" if not.
func detectIntegrationBranch(bd *beads.Beads, g *git.Git, issueID string) (string, error) {
	// Get the source issue
	issue, err := bd.Show(issueID)
	if err != nil {
		return "", fmt.Errorf("looking up issue %s: %w", issueID, err)
	}

	// Check if issue has a parent
	if issue.Parent == "" {
		return "", nil // No parent, no integration branch
	}

	// Get the parent issue
	parent, err := bd.Show(issue.Parent)
	if err != nil {
		return "", fmt.Errorf("looking up parent %s: %w", issue.Parent, err)
	}

	// Check if parent is an epic
	if parent.Type != "epic" {
		return "", nil // Parent is not an epic
	}

	// Check if integration branch exists
	integrationBranch := "integration/" + parent.ID

	// Check local first (faster)
	exists, err := g.BranchExists(integrationBranch)
	if err != nil {
		return "", fmt.Errorf("checking local branch: %w", err)
	}
	if exists {
		return integrationBranch, nil
	}

	// Check remote
	exists, err = g.RemoteBranchExists("origin", integrationBranch)
	if err != nil {
		// Remote check failure is non-fatal
		return "", nil
	}
	if exists {
		return integrationBranch, nil
	}

	return "", nil // No integration branch found
}
