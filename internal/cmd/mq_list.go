package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/style"
)

func runMQList(cmd *cobra.Command, args []string) error {
	rigName := args[0]

	_, r, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}

	// Create beads wrapper for the rig
	b := beads.New(r.Path)

	// Build list options - query for merge-request type
	// Priority -1 means no priority filter (otherwise 0 would filter to P0 only)
	opts := beads.ListOptions{
		Type:     "merge-request",
		Priority: -1,
	}

	// Apply status filter if specified
	if mqListStatus != "" {
		opts.Status = mqListStatus
	} else if !mqListReady {
		// Default to open if not showing ready
		opts.Status = "open"
	}

	var issues []*beads.Issue

	if mqListReady {
		// Use ready query which filters by no blockers
		allReady, err := b.Ready()
		if err != nil {
			return fmt.Errorf("querying ready MRs: %w", err)
		}
		// Filter to only merge-request type
		for _, issue := range allReady {
			if issue.Type == "merge-request" {
				issues = append(issues, issue)
			}
		}
	} else {
		issues, err = b.List(opts)
		if err != nil {
			return fmt.Errorf("querying merge queue: %w", err)
		}
	}

	// Apply additional filters
	var filtered []*beads.Issue
	for _, issue := range issues {
		// Parse MR fields
		fields := beads.ParseMRFields(issue)

		// Filter by worker
		if mqListWorker != "" {
			worker := ""
			if fields != nil {
				worker = fields.Worker
			}
			if !strings.EqualFold(worker, mqListWorker) {
				continue
			}
		}

		// Filter by epic (target branch)
		if mqListEpic != "" {
			target := ""
			if fields != nil {
				target = fields.Target
			}
			expectedTarget := "integration/" + mqListEpic
			if target != expectedTarget {
				continue
			}
		}

		filtered = append(filtered, issue)
	}

	// JSON output
	if mqListJSON {
		return outputJSON(filtered)
	}

	// Human-readable output
	fmt.Printf("%s Merge queue for '%s':\n\n", style.Bold.Render("📋"), rigName)

	if len(filtered) == 0 {
		fmt.Printf("  %s\n", style.Dim.Render("(empty)"))
		return nil
	}

	// Print header
	fmt.Printf("  %-12s %-12s %-8s %-30s %-10s %s\n",
		"ID", "STATUS", "PRIORITY", "BRANCH", "WORKER", "AGE")
	fmt.Printf("  %s\n", strings.Repeat("-", 90))

	// Print each MR
	for _, issue := range filtered {
		fields := beads.ParseMRFields(issue)

		// Determine display status
		displayStatus := issue.Status
		if issue.Status == "open" {
			if len(issue.BlockedBy) > 0 || issue.BlockedByCount > 0 {
				displayStatus = "blocked"
			} else {
				displayStatus = "ready"
			}
		}

		// Format status with styling
		styledStatus := displayStatus
		switch displayStatus {
		case "ready":
			styledStatus = style.Bold.Render("ready")
		case "in_progress":
			styledStatus = style.Bold.Render("in_progress")
		case "blocked":
			styledStatus = style.Dim.Render("blocked")
		case "closed":
			styledStatus = style.Dim.Render("closed")
		}

		// Get MR fields
		branch := ""
		worker := ""
		if fields != nil {
			branch = fields.Branch
			worker = fields.Worker
		}

		// Truncate branch if too long
		if len(branch) > 30 {
			branch = branch[:27] + "..."
		}

		// Format priority
		priority := fmt.Sprintf("P%d", issue.Priority)

		// Calculate age
		age := formatMRAge(issue.CreatedAt)

		// Truncate ID if needed
		displayID := issue.ID
		if len(displayID) > 12 {
			displayID = displayID[:12]
		}

		fmt.Printf("  %-12s %-12s %-8s %-30s %-10s %s\n",
			displayID, styledStatus, priority, branch, worker, style.Dim.Render(age))

		// Show blocking info if blocked
		if displayStatus == "blocked" && len(issue.BlockedBy) > 0 {
			fmt.Printf("  %s\n", style.Dim.Render(fmt.Sprintf("             (waiting on %s)", issue.BlockedBy[0])))
		}
	}

	return nil
}

// formatMRAge formats the age of an MR from its created_at timestamp.
func formatMRAge(createdAt string) string {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		// Try other formats
		t, err = time.Parse("2006-01-02T15:04:05Z", createdAt)
		if err != nil {
			return "?"
		}
	}

	d := time.Since(t)

	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// outputJSON outputs data as JSON.
func outputJSON(data interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
