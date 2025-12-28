package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/steveyegge/gastown/internal/keepalive"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/tmux"
)

// Daemon is the town-level background service.
// Its only job is to ensure Deacon is running and send periodic heartbeats.
// All health checking, nudging, and decision-making belongs in the Deacon molecule.
type Daemon struct {
	config        *Config
	tmux          *tmux.Tmux
	logger        *log.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	lastMOTDIndex int // tracks last MOTD to avoid consecutive repeats
}

// New creates a new daemon instance.
func New(config *Config) (*Daemon, error) {
	// Ensure daemon directory exists
	daemonDir := filepath.Dir(config.LogFile)
	if err := os.MkdirAll(daemonDir, 0755); err != nil {
		return nil, fmt.Errorf("creating daemon directory: %w", err)
	}

	// Open log file
	logFile, err := os.OpenFile(config.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening log file: %w", err)
	}

	logger := log.New(logFile, "", log.LstdFlags)
	ctx, cancel := context.WithCancel(context.Background())

	return &Daemon{
		config: config,
		tmux:   tmux.NewTmux(),
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

// Run starts the daemon main loop.
func (d *Daemon) Run() error {
	d.logger.Printf("Daemon starting (PID %d)", os.Getpid())

	// Write PID file
	if err := os.WriteFile(d.config.PidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return fmt.Errorf("writing PID file: %w", err)
	}
	defer func() { _ = os.Remove(d.config.PidFile) }() // best-effort cleanup

	// Update state
	state := &State{
		Running:   true,
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}
	if err := SaveState(d.config.TownRoot, state); err != nil {
		d.logger.Printf("Warning: failed to save state: %v", err)
	}

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)

	// Dynamic heartbeat timer with exponential backoff based on activity
	// Start with base interval
	nextInterval := d.config.HeartbeatInterval
	timer := time.NewTimer(nextInterval)
	defer timer.Stop()

	d.logger.Printf("Daemon running, initial heartbeat interval %v", nextInterval)

	// Initial heartbeat
	d.heartbeat(state)

	for {
		select {
		case <-d.ctx.Done():
			d.logger.Println("Daemon context cancelled, shutting down")
			return d.shutdown(state)

		case sig := <-sigChan:
			if sig == syscall.SIGUSR1 {
				// SIGUSR1: immediate lifecycle processing (from gt handoff)
				d.logger.Println("Received SIGUSR1, processing lifecycle requests immediately")
				d.processLifecycleRequests()
			} else {
				d.logger.Printf("Received signal %v, shutting down", sig)
				return d.shutdown(state)
			}

		case <-timer.C:
			d.heartbeat(state)

			// Calculate next interval based on activity
			nextInterval = d.calculateHeartbeatInterval()
			timer.Reset(nextInterval)
			d.logger.Printf("Next heartbeat in %v", nextInterval)
		}
	}
}

// Backoff thresholds for exponential slowdown when idle
const (
	// Base interval when there's recent activity
	baseInterval = 5 * time.Minute

	// Tier thresholds for backoff
	tier1Threshold = 5 * time.Minute  // 0-5 min idle → 5 min interval
	tier2Threshold = 15 * time.Minute // 5-15 min idle → 10 min interval
	tier3Threshold = 45 * time.Minute // 15-45 min idle → 30 min interval
	// 45+ min idle → 60 min interval (max)

	// Corresponding intervals
	tier1Interval = 5 * time.Minute
	tier2Interval = 10 * time.Minute
	tier3Interval = 30 * time.Minute
	tier4Interval = 60 * time.Minute // max
)

// calculateHeartbeatInterval determines the next heartbeat interval based on activity.
// Reads ~/gt/daemon/activity.json to determine how long since the last gt/bd command.
// Returns exponentially increasing intervals as idle time grows.
//
// | Idle Duration | Next Heartbeat |
// |---------------|----------------|
// | 0-5 min       | 5 min (base)   |
// | 5-15 min      | 10 min         |
// | 15-45 min     | 30 min         |
// | 45+ min       | 60 min (max)   |
func (d *Daemon) calculateHeartbeatInterval() time.Duration {
	activity := keepalive.ReadTownActivity()
	if activity == nil {
		// No activity file - assume recent activity (might be first run)
		return baseInterval
	}

	idleDuration := activity.Age()

	switch {
	case idleDuration < tier1Threshold:
		return tier1Interval
	case idleDuration < tier2Threshold:
		return tier2Interval
	case idleDuration < tier3Threshold:
		return tier3Interval
	default:
		return tier4Interval
	}
}

// heartbeat performs one heartbeat cycle.
// The daemon's job is minimal: ensure Deacon is running and send heartbeats.
// All health checking and decision-making belongs in the Deacon molecule.
func (d *Daemon) heartbeat(state *State) {
	d.logger.Println("Heartbeat starting")

	// 1. Ensure Deacon is running (process management)
	d.ensureDeaconRunning()

	// 2. Send heartbeat to Deacon (simple notification, no decision-making)
	d.pokeDeacon()

	// 3. Trigger pending polecat spawns (bootstrap mode - ZFC violation acceptable)
	// This ensures polecats get nudged even when Deacon isn't in a patrol cycle.
	// Uses regex-based WaitForClaudeReady, which is acceptable for daemon bootstrap.
	d.triggerPendingSpawns()

	// 4. Process lifecycle requests
	d.processLifecycleRequests()

	// 5. Check for stale agents (timeout fallback - gt-2hzl4)
	// Agents that report "running" but haven't updated in too long are marked dead
	d.checkStaleAgents()

	// Update state
	state.LastHeartbeat = time.Now()
	state.HeartbeatCount++
	if err := SaveState(d.config.TownRoot, state); err != nil {
		d.logger.Printf("Warning: failed to save state: %v", err)
	}

	d.logger.Printf("Heartbeat complete (#%d)", state.HeartbeatCount)
}

// DeaconSessionName is the tmux session name for the Deacon.
const DeaconSessionName = "gt-deacon"

// DeaconRole is the role name for the Deacon's handoff bead.
const DeaconRole = "deacon"

// deaconMOTDMessages contains rotating motivational and educational tips
// for the Deacon heartbeat. These make the thankless patrol role more fun.
var deaconMOTDMessages = []string{
	"Thanks for keeping the town running!",
	"You are Gas Town's most critical role.",
	"You are the heart of Gas Town! Be watchful!",
	"Tip: Polecats are transient - spawn freely, kill liberally.",
	"Tip: Witnesses monitor polecats; you monitor witnesses.",
	"Tip: Wisps are transient molecules for patrol cycles.",
	"The town sleeps soundly because you never do.",
	"Tip: Mayor handles cross-rig coordination; you handle health.",
	"Your vigilance keeps the agents honest.",
	"Tip: Use 'gt deacon heartbeat' to signal you're alive.",
	"Every heartbeat you check keeps Gas Town beating.",
	"Tip: Stale agents need nudging; very stale ones need restarting.",
}

// nextMOTD returns the next MOTD message, rotating through the list
// and avoiding consecutive repeats.
func (d *Daemon) nextMOTD() string {
	if len(deaconMOTDMessages) == 0 {
		return "HEARTBEAT: run your rounds"
	}

	// Pick a random index that's different from the last one
	nextIdx := d.lastMOTDIndex
	for nextIdx == d.lastMOTDIndex && len(deaconMOTDMessages) > 1 {
		nextIdx = int(time.Now().UnixNano() % int64(len(deaconMOTDMessages)))
	}
	d.lastMOTDIndex = nextIdx
	return deaconMOTDMessages[nextIdx]
}

// ensureDeaconRunning checks if the Deacon session exists and Claude is running.
// If the session exists but Claude has exited, it restarts Claude.
// If the session doesn't exist, it creates it and starts Claude.
// The Deacon is the system's heartbeat - it must always be running.
func (d *Daemon) ensureDeaconRunning() {
	// Check agent bead state (ZFC: trust what agent reports)
	// This is the preferred state source per gt-39ttg
	beadState, beadErr := d.getAgentBeadState("gt-deacon")
	if beadErr == nil {
		// Agent bead exists - check its state
		if beadState == "running" || beadState == "working" {
			// Agent reports it's running - trust it
			// (Future: gt-2hzl4 will add timeout fallback for stale state)
			return
		}
		// Agent reports not running - fall through to tmux check
	}
	// If agent bead not found, fall through to legacy tmux detection

	sessionExists, err := d.tmux.HasSession(DeaconSessionName)
	if err != nil {
		d.logger.Printf("Error checking Deacon session: %v", err)
		return
	}

	if sessionExists {
		// Session exists - check if Claude is actually running
		cmd, err := d.tmux.GetPaneCommand(DeaconSessionName)
		if err != nil {
			d.logger.Printf("Error checking Deacon pane command: %v", err)
			return
		}

		// If Claude is running (node process), we're good
		if cmd == "node" {
			return
		}

		// Claude has exited (shell is showing) - restart it
		d.logger.Printf("Deacon session exists but Claude exited (cmd=%s), restarting...", cmd)
		if err := d.tmux.SendKeys(DeaconSessionName, "export GT_ROLE=deacon BD_ACTOR=deacon && claude --dangerously-skip-permissions"); err != nil {
			d.logger.Printf("Error restarting Claude in Deacon session: %v", err)
		}
		return
	}

	// Session doesn't exist - create it and start Claude
	d.logger.Println("Deacon session not running, starting...")

	// Create session in deacon directory (ensures correct CLAUDE.md is loaded)
	deaconDir := filepath.Join(d.config.TownRoot, "deacon")
	if err := d.tmux.NewSession(DeaconSessionName, deaconDir); err != nil {
		d.logger.Printf("Error creating Deacon session: %v", err)
		return
	}

	// Set environment (non-fatal: session works without these)
	_ = d.tmux.SetEnvironment(DeaconSessionName, "GT_ROLE", "deacon")
	_ = d.tmux.SetEnvironment(DeaconSessionName, "BD_ACTOR", "deacon")

	// Launch Claude directly (no shell respawn loop)
	// The daemon will detect if Claude exits and restart it on next heartbeat
	// Export GT_ROLE and BD_ACTOR so Claude inherits them (tmux SetEnvironment doesn't export to processes)
	if err := d.tmux.SendKeys(DeaconSessionName, "export GT_ROLE=deacon BD_ACTOR=deacon && claude --dangerously-skip-permissions"); err != nil {
		d.logger.Printf("Error launching Claude in Deacon session: %v", err)
		return
	}

	d.logger.Println("Deacon session started successfully")
}

// pokeDeacon sends a heartbeat message to the Deacon session.
// Simple notification - no staleness checking or backoff logic.
// The Deacon molecule decides what to do with heartbeats.
func (d *Daemon) pokeDeacon() {
	running, err := d.tmux.HasSession(DeaconSessionName)
	if err != nil {
		d.logger.Printf("Error checking Deacon session: %v", err)
		return
	}

	if !running {
		d.logger.Println("Deacon session not running after ensure, skipping poke")
		return
	}

	// Send heartbeat message with rotating MOTD
	motd := d.nextMOTD()
	msg := fmt.Sprintf("HEARTBEAT: %s", motd)
	if err := d.tmux.SendKeysReplace(DeaconSessionName, msg, 50); err != nil {
		d.logger.Printf("Error poking Deacon: %v", err)
		return
	}

	d.logger.Println("Poked Deacon")
}

// NOTE: pokeMayor, pokeWitnesses, and pokeWitness have been removed.
// The Deacon molecule is responsible for monitoring Mayor and Witnesses.
// The daemon only ensures Deacon is running and sends it heartbeats.

// triggerPendingSpawns polls pending polecat spawns and triggers those that are ready.
// This is bootstrap mode - uses regex-based WaitForClaudeReady which is acceptable
// for daemon operations when no AI agent is guaranteed to be running.
// The timeout is short (2s) to avoid blocking the heartbeat.
func (d *Daemon) triggerPendingSpawns() {
	const triggerTimeout = 2 * time.Second

	// Check for pending spawns (from POLECAT_STARTED messages in Deacon inbox)
	pending, err := polecat.CheckInboxForSpawns(d.config.TownRoot)
	if err != nil {
		d.logger.Printf("Error checking pending spawns: %v", err)
		return
	}

	if len(pending) == 0 {
		return
	}

	d.logger.Printf("Found %d pending spawn(s), attempting to trigger...", len(pending))

	// Trigger pending spawns (uses WaitForClaudeReady with short timeout)
	results, err := polecat.TriggerPendingSpawns(d.config.TownRoot, triggerTimeout)
	if err != nil {
		d.logger.Printf("Error triggering spawns: %v", err)
		return
	}

	// Log results
	triggered := 0
	for _, r := range results {
		if r.Triggered {
			triggered++
			d.logger.Printf("Triggered polecat: %s/%s", r.Spawn.Rig, r.Spawn.Polecat)
		} else if r.Error != nil {
			d.logger.Printf("Error triggering %s: %v", r.Spawn.Session, r.Error)
		}
	}

	if triggered > 0 {
		d.logger.Printf("Triggered %d/%d pending spawn(s)", triggered, len(pending))
	}

	// Prune stale pending spawns (older than 5 minutes - likely dead sessions)
	pruned, _ := polecat.PruneStalePending(d.config.TownRoot, 5*time.Minute)
	if pruned > 0 {
		d.logger.Printf("Pruned %d stale pending spawn(s)", pruned)
	}
}

// processLifecycleRequests checks for and processes lifecycle requests.
func (d *Daemon) processLifecycleRequests() {
	d.ProcessLifecycleRequests()
}

// shutdown performs graceful shutdown.
func (d *Daemon) shutdown(state *State) error {
	d.logger.Println("Daemon shutting down")

	state.Running = false
	if err := SaveState(d.config.TownRoot, state); err != nil {
		d.logger.Printf("Warning: failed to save final state: %v", err)
	}

	d.logger.Println("Daemon stopped")
	return nil
}

// Stop signals the daemon to stop.
func (d *Daemon) Stop() {
	d.cancel()
}

// IsRunning checks if a daemon is running for the given town.
func IsRunning(townRoot string) (bool, int, error) {
	pidFile := filepath.Join(townRoot, "daemon", "daemon.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, err
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return false, 0, nil
	}

	// Check if process is running
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, 0, nil
	}

	// On Unix, FindProcess always succeeds. Send signal 0 to check if alive.
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		// Process not running, clean up stale PID file (best-effort cleanup)
		_ = os.Remove(pidFile)
		return false, 0, nil
	}

	return true, pid, nil
}

// StopDaemon stops the running daemon for the given town.
func StopDaemon(townRoot string) error {
	running, pid, err := IsRunning(townRoot)
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("daemon is not running")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process: %w", err)
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sending SIGTERM: %w", err)
	}

	// Wait a bit for graceful shutdown
	time.Sleep(500 * time.Millisecond)

	// Check if still running
	if err := process.Signal(syscall.Signal(0)); err == nil {
		// Still running, force kill (best-effort)
		_ = process.Signal(syscall.SIGKILL)
	}

	// Clean up PID file (best-effort cleanup)
	pidFile := filepath.Join(townRoot, "daemon", "daemon.pid")
	_ = os.Remove(pidFile)

	return nil
}
