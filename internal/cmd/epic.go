package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
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
	epicCreateRig                 string
)

var epicCmd = &cobra.Command{
	Use:     "epic",
	GroupID: GroupWork,
	Short:   "Manage epics",
	RunE:    requireSubcommand,
}

var epicCreateCmd = &cobra.Command{
	Use:   "create <title>",
	Short: "Create an epic with an integration branch",
	Long: `Create a beads epic and an integration branch named from the title.

Requires integration_branch_enabled: true in rig settings/config.json.

The branch is named integration/<kebab-case-title>. Use --no-integration-branch
to skip branch creation (epic only).

When --parent points to an epic with an integration branch, the new branch
is forked from the parent's integration branch (not main). If the parent
epic has no integration branch, the command errors with instructions to
create one first. Use --base-branch to override this behavior.

Examples:
  gt epic create "User Authentication"
  gt epic create "API v2 Migration" --parent gt-abc
  gt epic create "Big Feature" --base-branch develop
  gt epic create "Quick Epic" --no-integration-branch
  gt epic create "Urgent Fix" -p 1 -l urgent -l backend
  gt epic create "Cross-Rig Feature" --rig greenplace`,
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
	epicCreateCmd.Flags().StringVar(&epicCreateRig, "rig", "", "Rig to create epic in (default: infer from cwd)")

	epicCmd.AddCommand(epicCreateCmd)
	rootCmd.AddCommand(epicCmd)
}

// findRigByName resolves a rig by name from the town root.
func findRigByName(townRoot, rigName string) (*rig.Rig, error) {
	rigsConfigPath := filepath.Join(townRoot, "mayor", "rigs.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Rigs: make(map[string]config.RigEntry)}
	}

	g := git.NewGit(townRoot)
	rigMgr := rig.NewManager(townRoot, rigsConfig, g)
	return rigMgr.GetRig(rigName)
}

func runEpicCreate(cmd *cobra.Command, args []string) error {
	title := args[0]

	// Find workspace
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Resolve rig: --rig flag > infer from cwd
	var r *rig.Rig
	if epicCreateRig != "" {
		r, err = findRigByName(townRoot, epicCreateRig)
		if err != nil {
			return fmt.Errorf("rig '%s' not found", epicCreateRig)
		}
	} else {
		_, r, err = findCurrentRig(townRoot)
		if err != nil {
			return fmt.Errorf("could not determine rig (use --rig flag): %w", err)
		}
	}

	// Require integration branches to be enabled in rig config
	settingsPath := filepath.Join(r.Path, "settings", "config.json")
	settings, err := config.LoadRigSettings(settingsPath)
	if err != nil || settings.MergeQueue == nil || !settings.MergeQueue.IsIntegrationBranchEnabled() {
		return fmt.Errorf("integration branches are not enabled for this rig\n\n  Set \"integration_branch_enabled\": true in %s", settingsPath)
	}

	// Initialize beads for the rig
	bd := beads.New(r.Path)

	// Resolve base branch for the integration branch.
	// Priority: --base-branch flag > parent epic's integration branch > rig default_branch.
	// When --parent points to an epic, we require it to have an integration branch
	// (unless --base-branch is explicitly set or --no-integration-branch is used).
	baseBranch := epicCreateBaseBranch
	if baseBranch == "" && epicCreateParent != "" && !epicCreateNoIntegrationBranch {
		parent, err := bd.Show(epicCreateParent)
		if err != nil {
			return fmt.Errorf("looking up parent '%s': %w", epicCreateParent, err)
		}
		if parent.Type == "epic" {
			parentBranch := beads.GetIntegrationBranchField(parent.Description)
			if parentBranch == "" {
				return fmt.Errorf("parent epic '%s' (%s) has no integration branch\n\n"+
					"  Create one first:\n"+
					"    gt mq integration create %s\n\n"+
					"  Then re-run:\n"+
					"    gt epic create %q --parent %s",
					epicCreateParent, parent.Title,
					epicCreateParent,
					title, epicCreateParent)
			}
			baseBranch = parentBranch
		}
	}
	if baseBranch == "" {
		baseBranch = r.DefaultBranch()
	}

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

	if _, err := createIntegrationBranchForEpic(bd, g, epic.ID, branchName, baseBranch, epic.Description); err != nil {
		return err
	}

	fmt.Printf("%s Created integration branch: %s (from %s)\n", style.Bold.Render("✓"), branchName, baseBranch)

	return nil
}
