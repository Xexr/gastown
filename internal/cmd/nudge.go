package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

func init() {
	rootCmd.AddCommand(nudgeCmd)
}

var nudgeCmd = &cobra.Command{
	Use:     "nudge <rig/polecat> <message>",
	GroupID: GroupComm,
	Short:   "Send a message to a polecat session reliably",
	Long: `Sends a message to a polecat's Claude Code session.

Uses a reliable delivery pattern:
1. Sends text in literal mode (-l flag)
2. Waits 500ms for paste to complete
3. Sends Enter as a separate command

This is the ONLY way to send messages to Claude sessions.
Do not use raw tmux send-keys elsewhere.

Examples:
  gt nudge gastown/furiosa "Check your mail and start working"
  gt nudge gastown/alpha "What's your status?"`,
	Args: cobra.ExactArgs(2),
	RunE: runNudge,
}

func runNudge(cmd *cobra.Command, args []string) error {
	target := args[0]
	message := args[1]

	t := tmux.NewTmux()

	// Check if target is rig/polecat format or raw session name
	if strings.Contains(target, "/") {
		// Parse rig/polecat format
		rigName, polecatName, err := parseAddress(target)
		if err != nil {
			return err
		}

		var sessionName string

		// Check if this is a crew address (polecatName starts with "crew/")
		if strings.HasPrefix(polecatName, "crew/") {
			// Extract crew name and use crew session naming
			crewName := strings.TrimPrefix(polecatName, "crew/")
			sessionName = crewSessionName(rigName, crewName)
		} else {
			// Regular polecat - use session manager
			mgr, _, err := getSessionManager(rigName)
			if err != nil {
				return err
			}
			sessionName = mgr.SessionName(polecatName)
		}

		// Send nudge using the reliable NudgeSession
		if err := t.NudgeSession(sessionName, message); err != nil {
			return fmt.Errorf("nudging session: %w", err)
		}

		fmt.Printf("%s Nudged %s/%s\n", style.Bold.Render("✓"), rigName, polecatName)

		// Log nudge event
		if townRoot, err := workspace.FindFromCwd(); err == nil && townRoot != "" {
			LogNudge(townRoot, target, message)
		}
	} else {
		// Raw session name (legacy)
		exists, err := t.HasSession(target)
		if err != nil {
			return fmt.Errorf("checking session: %w", err)
		}
		if !exists {
			return fmt.Errorf("session %q not found", target)
		}

		if err := t.NudgeSession(target, message); err != nil {
			return fmt.Errorf("nudging session: %w", err)
		}

		fmt.Printf("✓ Nudged %s\n", target)

		// Log nudge event
		if townRoot, err := workspace.FindFromCwd(); err == nil && townRoot != "" {
			LogNudge(townRoot, target, message)
		}
	}

	return nil
}
