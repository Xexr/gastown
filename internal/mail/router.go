package mail

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/tmux"
)

// ErrUnknownList indicates a mailing list name was not found in configuration.
var ErrUnknownList = errors.New("unknown mailing list")

// Router handles message delivery via beads.
// It routes messages to the correct beads database based on address:
// - Town-level (mayor/, deacon/) -> {townRoot}/.beads
// - Rig-level (rig/polecat) -> {townRoot}/{rig}/.beads
type Router struct {
	workDir  string // fallback directory to run bd commands in
	townRoot string // town root directory (e.g., ~/gt)
	tmux     *tmux.Tmux
}

// NewRouter creates a new mail router.
// workDir should be a directory containing a .beads database.
// The town root is auto-detected from workDir if possible.
func NewRouter(workDir string) *Router {
	// Try to detect town root from workDir
	townRoot := detectTownRoot(workDir)

	return &Router{
		workDir:  workDir,
		townRoot: townRoot,
		tmux:     tmux.NewTmux(),
	}
}

// NewRouterWithTownRoot creates a router with an explicit town root.
func NewRouterWithTownRoot(workDir, townRoot string) *Router {
	return &Router{
		workDir:  workDir,
		townRoot: townRoot,
		tmux:     tmux.NewTmux(),
	}
}

// isListAddress returns true if the address uses list:name syntax.
func isListAddress(address string) bool {
	return strings.HasPrefix(address, "list:")
}

// parseListName extracts the list name from a list:name address.
func parseListName(address string) string {
	return strings.TrimPrefix(address, "list:")
}

// expandList returns the recipients for a mailing list.
// Returns ErrUnknownList if the list is not found.
func (r *Router) expandList(listName string) ([]string, error) {
	// Load messaging config from town root
	if r.townRoot == "" {
		return nil, fmt.Errorf("%w: %s (no town root)", ErrUnknownList, listName)
	}

	configPath := config.MessagingConfigPath(r.townRoot)
	cfg, err := config.LoadMessagingConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading messaging config: %w", err)
	}

	recipients, ok := cfg.Lists[listName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownList, listName)
	}

	if len(recipients) == 0 {
		return nil, fmt.Errorf("%w: %s (empty list)", ErrUnknownList, listName)
	}

	return recipients, nil
}

// detectTownRoot finds the town root by looking for mayor/town.json.
func detectTownRoot(startDir string) string {
	dir := startDir
	for {
		// Check for primary marker (mayor/town.json)
		markerPath := filepath.Join(dir, "mayor", "town.json")
		if _, err := os.Stat(markerPath); err == nil {
			return dir
		}

		// Move up
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// resolveBeadsDir returns the correct .beads directory for the given address.
//
// Two-level beads architecture:
// - ALL mail uses town beads ({townRoot}/.beads) regardless of address
// - Rig-level beads ({rig}/.beads) are for project issues only, not mail
//
// This ensures messages are visible to all agents in the town.
func (r *Router) resolveBeadsDir(address string) string {
	// If no town root, fall back to workDir's .beads
	if r.townRoot == "" {
		return filepath.Join(r.workDir, ".beads")
	}

	// All mail uses town-level beads
	return filepath.Join(r.townRoot, ".beads")
}

// isTownLevelAddress returns true if the address is for a town-level agent or the overseer.
func isTownLevelAddress(address string) bool {
	addr := strings.TrimSuffix(address, "/")
	return addr == "mayor" || addr == "deacon" || addr == "overseer"
}

// shouldBeWisp determines if a message should be stored as a wisp.
// Returns true if:
// - Message.Wisp is explicitly set
// - Subject matches lifecycle message patterns (POLECAT_*, NUDGE, etc.)
func (r *Router) shouldBeWisp(msg *Message) bool {
	if msg.Wisp {
		return true
	}
	// Auto-detect lifecycle messages by subject prefix
	subjectLower := strings.ToLower(msg.Subject)
	wispPrefixes := []string{
		"polecat_started",
		"polecat_done",
		"start_work",
		"nudge",
	}
	for _, prefix := range wispPrefixes {
		if strings.HasPrefix(subjectLower, prefix) {
			return true
		}
	}
	return false
}

// Send delivers a message via beads message.
// Routes the message to the correct beads database based on recipient address.
// If the recipient is a mailing list (list:name), fans out to all list members,
// creating a separate copy for each recipient.
func (r *Router) Send(msg *Message) error {
	// Check for mailing list address
	if isListAddress(msg.To) {
		return r.sendToList(msg)
	}

	// Convert addresses to beads identities
	toIdentity := addressToIdentity(msg.To)

	// Build labels for from/thread/reply-to/cc
	var labels []string
	labels = append(labels, "from:"+msg.From)
	if msg.ThreadID != "" {
		labels = append(labels, "thread:"+msg.ThreadID)
	}
	if msg.ReplyTo != "" {
		labels = append(labels, "reply-to:"+msg.ReplyTo)
	}
	// Add CC labels (one per recipient)
	for _, cc := range msg.CC {
		ccIdentity := addressToIdentity(cc)
		labels = append(labels, "cc:"+ccIdentity)
	}

	// Build command: bd create <subject> --type=message --assignee=<recipient> -d <body>
	args := []string{"create", msg.Subject,
		"--type", "message",
		"--assignee", toIdentity,
		"-d", msg.Body,
	}

	// Add priority flag
	beadsPriority := PriorityToBeads(msg.Priority)
	args = append(args, "--priority", fmt.Sprintf("%d", beadsPriority))

	// Add labels
	if len(labels) > 0 {
		args = append(args, "--labels", strings.Join(labels, ","))
	}

	// Add actor for attribution (sender identity)
	args = append(args, "--actor", msg.From)

	// Add --ephemeral flag for ephemeral messages (stored in single DB, filtered from JSONL export)
	if r.shouldBeWisp(msg) {
		args = append(args, "--ephemeral")
	}

	beadsDir := r.resolveBeadsDir(msg.To)
	cmd := exec.Command("bd", args...)
	cmd.Env = append(cmd.Environ(),
		"BEADS_DIR="+beadsDir,
	)
	cmd.Dir = filepath.Dir(beadsDir) // Run in parent of .beads

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return errors.New(errMsg)
		}
		return fmt.Errorf("sending message: %w", err)
	}

	// Notify recipient if they have an active session (best-effort notification)
	// Skip notification for self-mail (handoffs to future-self don't need present-self notified)
	if !isSelfMail(msg.From, msg.To) {
		_ = r.notifyRecipient(msg)
	}

	return nil
}

// sendToList expands a mailing list and sends individual copies to each recipient.
// Each recipient gets their own message copy with the same content.
// Returns a ListDeliveryResult with details about the fan-out.
func (r *Router) sendToList(msg *Message) error {
	listName := parseListName(msg.To)
	recipients, err := r.expandList(listName)
	if err != nil {
		return err
	}

	// Send to each recipient
	var lastErr error
	successCount := 0
	for _, recipient := range recipients {
		// Create a copy of the message for this recipient
		copy := *msg
		copy.To = recipient

		if err := r.Send(&copy); err != nil {
			lastErr = err
			continue
		}
		successCount++
	}

	// If all sends failed, return the last error
	if successCount == 0 && lastErr != nil {
		return fmt.Errorf("sending to list %s: %w", listName, lastErr)
	}

	return nil
}

// ExpandListAddress expands a list:name address to its recipients.
// Returns ErrUnknownList if the list is not found.
// This is exported for use by commands that want to show fan-out details.
func (r *Router) ExpandListAddress(address string) ([]string, error) {
	if !isListAddress(address) {
		return nil, fmt.Errorf("not a list address: %s", address)
	}
	return r.expandList(parseListName(address))
}

// isSelfMail returns true if sender and recipient are the same identity.
// Normalizes addresses by removing trailing slashes for comparison.
func isSelfMail(from, to string) bool {
	fromNorm := strings.TrimSuffix(from, "/")
	toNorm := strings.TrimSuffix(to, "/")
	return fromNorm == toNorm
}

// GetMailbox returns a Mailbox for the given address.
// Routes to the correct beads database based on the address.
func (r *Router) GetMailbox(address string) (*Mailbox, error) {
	beadsDir := r.resolveBeadsDir(address)
	workDir := filepath.Dir(beadsDir) // Parent of .beads
	return NewMailboxFromAddress(address, workDir), nil
}

// notifyRecipient sends a notification to a recipient's tmux session.
// Uses send-keys to echo a visible banner to ensure notification is seen.
// Supports mayor/, rig/polecat, and rig/refinery addresses.
func (r *Router) notifyRecipient(msg *Message) error {
	sessionID := addressToSessionID(msg.To)
	if sessionID == "" {
		return nil // Unable to determine session ID
	}

	// Check if session exists
	hasSession, err := r.tmux.HasSession(sessionID)
	if err != nil || !hasSession {
		return nil // No active session, skip notification
	}

	// Send visible notification banner to the terminal
	return r.tmux.SendNotificationBanner(sessionID, msg.From, msg.Subject)
}

// addressToSessionID converts a mail address to a tmux session ID.
// Returns empty string if address format is not recognized.
func addressToSessionID(address string) string {
	// Mayor address: "mayor/" or "mayor"
	if strings.HasPrefix(address, "mayor") {
		return "gt-mayor"
	}

	// Rig-based address: "rig/target"
	parts := strings.SplitN(address, "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		return ""
	}

	rig := parts[0]
	target := parts[1]

	// Polecat: gt-rig-polecat
	// Refinery: gt-rig-refinery (if refinery has its own session)
	return fmt.Sprintf("gt-%s-%s", rig, target)
}
