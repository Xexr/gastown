package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Epic create flags
var (
	epicCreateNoIntegrationBranch bool
	epicCreateBaseBranch          string
	epicCreateParent              string
	epicCreateLabels              []string
	epicCreateDescription         string
	epicCreatePriority            int
)

var epicCmd = &cobra.Command{
	Use:     "epic",
	GroupID: GroupWork,
	Short:   "Manage epics",
	RunE:    requireSubcommand,
}

var epicCreateCmd = &cobra.Command{
	Use:   "create <title>",
	Short: "Create an epic (optionally with an integration branch)",
	Long: `Create a beads epic. When integration_branch_enabled is true in rig
config, an integration branch is also created from the sanitized epic title.

The branch is named integration/<kebab-case-title>. Use --no-integration-branch
to skip branch creation even when the feature is enabled.

Examples:
  gt epic create "User Authentication"
  gt epic create "API v2 Migration" --parent gt-abc
  gt epic create "Big Feature" --base-branch develop
  gt epic create "Quick Epic" --no-integration-branch
  gt epic create "Urgent Fix" -p 1 -l urgent -l backend`,
	Args: cobra.ExactArgs(1),
	RunE: runEpicCreate,
}

func init() {
	epicCreateCmd.Flags().BoolVar(&epicCreateNoIntegrationBranch, "no-integration-branch", false, "Skip integration branch creation")
	epicCreateCmd.Flags().StringVar(&epicCreateBaseBranch, "base-branch", "", "Base branch for integration branch (default: main)")
	epicCreateCmd.Flags().StringVar(&epicCreateParent, "parent", "", "Parent bead for the epic")
	epicCreateCmd.Flags().StringSliceVarP(&epicCreateLabels, "label", "l", nil, "Labels (repeatable)")
	epicCreateCmd.Flags().StringVarP(&epicCreateDescription, "description", "d", "", "Epic description")
	epicCreateCmd.Flags().IntVarP(&epicCreatePriority, "priority", "p", 0, "Priority (0-3)")

	epicCmd.AddCommand(epicCreateCmd)
	rootCmd.AddCommand(epicCmd)
}

func runEpicCreate(cmd *cobra.Command, args []string) error {
	title := args[0]

	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Find current rig
	_, r, err := findCurrentRig(townRoot)
	if err != nil {
		return err
	}

	// Initialize beads for the rig
	bd := beads.New(r.Path)

	// Create epic bead
	createOpts := beads.CreateOptions{
		Title:       title,
		Type:        "epic",
		Priority:    epicCreatePriority,
		Description: epicCreateDescription,
		Parent:      epicCreateParent,
	}
	epic, err := bd.Create(createOpts)
	if err != nil {
		return fmt.Errorf("creating epic: %w", err)
	}

	// Add labels if provided
	if len(epicCreateLabels) > 0 {
		if err := bd.Update(epic.ID, beads.UpdateOptions{SetLabels: epicCreateLabels}); err != nil {
			fmt.Printf("  %s\n", style.Dim.Render("(warning: could not set labels)"))
		}
	}

	fmt.Printf("%s Created epic %s: %s\n", style.Bold.Render("✓"), epic.ID, title)

	// Create integration branch unless opted out
	if epicCreateNoIntegrationBranch {
		return nil
	}

	// Sanitize title for branch name
	slug := beads.SanitizeTitleForBranch(title, beads.MaxSanitizedTitleLen)
	if slug == "" {
		fmt.Printf("  %s\n", style.Dim.Render("(skipping integration branch: title produced empty slug)"))
		return nil
	}
	branchName := "integration/" + slug

	// Initialize git for the rig
	g, err := getRigGit(r.Path)
	if err != nil {
		return fmt.Errorf("initializing git: %w", err)
	}

	if _, err := createIntegrationBranchForEpic(bd, g, epic.ID, branchName, epicCreateBaseBranch, epic.Description); err != nil {
		return err
	}

	baseBranchDisplay := epicCreateBaseBranch
	if baseBranchDisplay == "" {
		baseBranchDisplay = "main"
	}
	fmt.Printf("%s Created integration branch: %s (from %s)\n", style.Bold.Render("✓"), branchName, baseBranchDisplay)

	return nil
}
