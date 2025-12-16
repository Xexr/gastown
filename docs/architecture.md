# Gas Town Architecture

Gas Town is a multi-agent workspace manager that coordinates AI coding agents working on software projects. It provides the infrastructure for running swarms of agents, managing their lifecycle, and coordinating their work through mail and issue tracking.

## Core Concepts

### Town

A **Town** is a complete Gas Town installation - the workspace where everything lives. A town contains:
- Town configuration (`config/` directory)
- Mayor's home (`mayor/` directory at town level)
- One or more **Rigs** (managed project repositories)

### Rig

A **Rig** is a container directory for managing a project and its agents. Importantly, the rig itself is NOT a git clone - it's a pure container that holds:
- Rig configuration (`config.json`)
- Rig-level beads database (`.beads/`) for coordinating work
- Agent directories, each with their own git clone

This design prevents agent confusion: each agent has exactly one place to work (their own clone), with no ambiguous "rig root" that could tempt a lost agent.

### Overseer (Human Operator)

The **Overseer** is the human operator of Gas Town - not an AI agent, but the person who runs the system. The Overseer:

- **Sets strategy**: Defines project goals and priorities
- **Provisions resources**: Adds machines, polecats, and rigs
- **Reviews output**: Approves swarm results and merged code
- **Handles escalations**: Makes final decisions on stuck or ambiguous work
- **Operates the system**: Runs `gt` commands, monitors dashboards

The Mayor reports to the Overseer. When agents can't resolve issues, they escalate up through the chain: Polecat → Witness → Mayor → Overseer.

### Agents

Gas Town has four AI agent roles:

| Agent | Scope | Responsibility |
|-------|-------|----------------|
| **Mayor** | Town-wide | Global coordination, swarm dispatch, cross-rig decisions |
| **Witness** | Per-rig | Worker lifecycle, nudging, pre-kill verification, session cycling |
| **Refinery** | Per-rig | Merge queue processing, PR review, integration |
| **Polecat** | Per-rig | Implementation work on assigned issues |

### Mail

Agents communicate via **mail** - JSONL-based inboxes for asynchronous messaging. Each agent has an inbox at `mail/inbox.jsonl`. Mail enables:
- Work assignment (Mayor → Refinery → Polecat)
- Status reporting (Polecat → Witness → Mayor)
- Session handoff (Agent → Self for context cycling)
- Escalation (Witness → Mayor for stuck workers)

### Beads

**Beads** is the issue tracking system. Gas Town agents use beads to:
- Track work items (`bd ready`, `bd list`)
- Create issues for discovered work (`bd create`)
- Claim and complete work (`bd update`, `bd close`)
- Sync state to git (`bd sync`)

Polecats have direct beads write access and file their own issues.

## Directory Structure

### Town Level

```
~/ai/                              # Town root
├── config/                        # Town configuration (VISIBLE, not hidden)
│   ├── town.json                  # {"type": "town", "name": "..."}
│   ├── rigs.json                  # Registry of managed rigs
│   └── federation.json            # Remote machine config (future)
│
├── mayor/                         # Mayor's HOME at town level
│   ├── CLAUDE.md                  # Mayor role prompting
│   ├── mail/inbox.jsonl           # Mayor's inbox
│   └── state.json                 # Mayor state
│
├── wyvern/                        # A rig (project repository)
└── beads/                         # Another rig
```

### Rig Level

```
wyvern/                            # Rig = container (NOT a git clone)
├── config.json                    # Rig configuration
├── .beads/                        # Rig-level issue tracking
│   ├── beads.db                   # SQLite database
│   └── issues.jsonl               # Git-synced issues
│
├── polecats/                      # Worker directories
│   ├── Nux/                       # Full git clone (BEADS_DIR=../.beads)
│   ├── Toast/                     # Full git clone (BEADS_DIR=../.beads)
│   └── Capable/                   # Full git clone (BEADS_DIR=../.beads)
│
├── refinery/                      # Refinery agent
│   ├── rig/                       # Authoritative "main" clone
│   ├── state.json
│   └── mail/inbox.jsonl
│
├── witness/                       # Witness agent (per-rig pit boss)
│   ├── state.json                 # May not need its own clone
│   └── mail/inbox.jsonl
│
└── mayor/                         # Mayor's presence in this rig
    ├── rig/                       # Mayor's rig-specific clone
    └── state.json
```

**Key points:**
- The rig root has no `.git/` - it's not a repository
- All agents use `BEADS_DIR` to point to the rig's `.beads/`
- Refinery's clone is the authoritative "main branch" view
- Witness may not need its own clone (just monitors polecat state)

### Why Decentralized?

Agents live IN rigs rather than in a central location:
- **Locality**: Each agent works in the context of its rig
- **Independence**: Rigs can be added/removed without restructuring
- **Parallelism**: Multiple rigs can have active swarms simultaneously
- **Simplicity**: Agent finds its context by looking at its own directory

## Agent Responsibilities

### Mayor

The Mayor is the global coordinator:
- **Swarm dispatch**: Decides which rigs need swarms, what work to assign
- **Cross-rig coordination**: Routes work between rigs when needed
- **Escalation handling**: Resolves issues Witnesses can't handle
- **Strategic decisions**: Architecture, priorities, integration planning

**NOT Mayor's job**: Per-worker cleanup, session killing, nudging workers

### Witness

The Witness is the per-rig "pit boss":
- **Worker monitoring**: Track polecat health and progress
- **Nudging**: Prompt workers toward completion
- **Pre-kill verification**: Ensure git state is clean before killing sessions
- **Session lifecycle**: Kill sessions, update worker state
- **Self-cycling**: Hand off to fresh session when context fills
- **Escalation**: Report stuck workers to Mayor

**Key principle**: Witness owns ALL per-worker cleanup. Mayor is never involved in routine worker management.

### Refinery

The Refinery manages the merge queue:
- **PR review**: Check polecat work before merging
- **Integration**: Merge completed work to main
- **Conflict resolution**: Handle merge conflicts
- **Quality gate**: Ensure tests pass, code quality maintained

### Polecat

Polecats are the workers that do actual implementation:
- **Issue completion**: Work on assigned beads issues
- **Self-verification**: Run decommission checklist before signaling done
- **Beads access**: Create issues for discovered work, close completed work
- **Clean handoff**: Ensure git state is clean for Witness verification

## Key Workflows

### Swarm Dispatch

```
Mayor                     Refinery                    Polecats
  │                          │                           │
  ├─── [dispatch swarm] ────►│                           │
  │                          ├─── [assign issues] ──────►│
  │                          │                           │ (work)
  │                          │◄── [PR ready] ────────────┤
  │                          │ (review/merge)            │
  │◄── [swarm complete] ─────┤                           │
```

### Worker Cleanup (Witness-Owned)

```
Polecat                   Witness                     Mayor         Overseer
  │                          │                           │              │
  │ (completes work)         │                           │              │
  ├─── [done signal] ───────►│                           │              │
  │                          │ (capture git state)       │              │
  │                          │ (assess cleanliness)      │              │
  │◄── [nudge if dirty] ─────┤                           │              │
  │ (fixes issues)           │                           │              │
  ├─── [done signal] ───────►│                           │              │
  │                          │ (verify clean)            │              │
  │                          │ (kill session)            │              │
  │                          │                           │              │
  │                          │ (if stuck 3x) ───────────►│              │
  │                          │                           │ (can't fix) ─►│
  │                          │                           │              │ (human decision)
```

### Session Cycling (Mail-to-Self)

When an agent's context fills, it hands off to its next session:

1. **Recognize**: Notice context filling (slow responses, losing track of state)
2. **Capture**: Gather current state (active work, pending decisions, warnings)
3. **Compose**: Write structured handoff note
4. **Send**: Mail handoff to own inbox
5. **Exit**: End session cleanly
6. **Resume**: New session reads handoff, picks up where old session left off

## Key Design Decisions

### 1. Witness Owns Worker Cleanup

**Decision**: Witness handles all per-worker cleanup. Mayor is never involved.

**Rationale**:
- Separation of concerns (Mayor strategic, Witness operational)
- Reduced coordination overhead
- Faster shutdown
- Cleaner escalation path

### 2. Polecats Have Direct Beads Access

**Decision**: Polecats can create, update, and close beads issues directly.

**Rationale**:
- Simplifies architecture (no proxy through Witness)
- Empowers workers to file discovered work
- Faster feedback loop
- Beads v0.30.0+ handles multi-agent conflicts

### 3. Session Cycling via Mail-to-Self

**Decision**: Agents mail handoff notes to themselves when cycling sessions.

**Rationale**:
- Consistent pattern across all agent types
- Timestamped and logged
- Works with existing inbox infrastructure
- Clean separation between sessions

### 4. Decentralized Agent Architecture

**Decision**: Agents live in rigs (`<rig>/witness/rig/`) not centralized (`mayor/rigs/<rig>/`).

**Rationale**:
- Agents work in context of their rig
- Rigs are independent units
- Simpler role detection
- Cleaner directory structure

### 5. Visible Config Directory

**Decision**: Use `config/` not `.gastown/` for town configuration.

**Rationale**: AI models often miss hidden directories. Visible is better.

### 6. Rig as Container, Not Clone

**Decision**: The rig directory is a pure container, not a git clone of the project.

**Rationale**:
- **Prevents confusion**: Agents historically get lost (polecats in refinery, mayor in polecat dirs). If the rig root were a clone, it's another tempting target for confused agents. Two confused agents at once = collision disaster.
- **Single work location**: Each agent has exactly one place to work (their own `/rig/` clone)
- **Clear role detection**: "Am I in a `/rig/` directory?" = I'm in an agent clone
- **Refinery is canonical main**: Refinery's clone serves as the authoritative "main branch" - it pulls, merges PRs, and pushes. No need for a separate rig-root clone.

### 7. Plugins as Agents

**Decision**: Plugins are just additional agents with identities, mailboxes, and access to beads. No special plugin infrastructure.

**Rationale**:
- Fits Gas Town's intentionally rough aesthetic
- Zero new infrastructure needed (uses existing mail, beads, identities)
- Composable - plugins can invoke other plugins via mail
- Debuggable - just look at mail logs and bead history
- Extensible - anyone can add a plugin by creating a directory

**Structure**: `<rig>/plugins/<name>/` with optional `rig/`, `CLAUDE.md`, `mail/`, `state.json`.

### 8. Rig-Level Beads via BEADS_DIR

**Decision**: Each rig has its own `.beads/` directory. Agents use the `BEADS_DIR` environment variable to point to it.

**Rationale**:
- **Centralized issue tracking**: All polecats in a rig share the same beads database
- **Project separation**: Even if the project repo has its own `.beads/`, Gas Town agents use the rig's beads instead
- **OSS-friendly**: For contributing to projects you don't own, rig beads stay separate from upstream
- **Already supported**: Beads supports `BEADS_DIR` env var (see beads `internal/beads/beads.go`)

**Configuration**: Gas Town sets `BEADS_DIR` when spawning agents:
```bash
export BEADS_DIR=/path/to/rig/.beads
```

**See also**: beads issue `bd-411u` for documentation of this pattern.

## Configuration

### town.json

```json
{
  "type": "town",
  "version": 1,
  "name": "stevey-gastown",
  "created_at": "2024-01-15T10:30:00Z"
}
```

### rigs.json

```json
{
  "version": 1,
  "rigs": {
    "wyvern": {
      "git_url": "https://github.com/steveyegge/wyvern",
      "added_at": "2024-01-15T10:30:00Z"
    }
  }
}
```

### rig.json (Per-Rig Config)

Each rig has a `config.json` at its root:

```json
{
  "type": "rig",
  "version": 1,
  "name": "wyvern",
  "git_url": "https://github.com/steveyegge/wyvern",
  "beads": {
    "prefix": "wyv",
    "sync_remote": "origin"    // Optional: git remote for bd sync
  }
}
```

The rig's `.beads/` directory is always at the rig root. Gas Town:
1. Creates `.beads/` when adding a rig (`gt rig add`)
2. Runs `bd init --prefix <prefix>` to initialize it
3. Sets `BEADS_DIR` environment variable when spawning agents

This ensures all agents in the rig share a single beads database, separate from any beads the project itself might use.

## CLI Commands

### Town Management

```bash
gt install [path]      # Install Gas Town at path
gt doctor              # Check workspace health
gt doctor --fix        # Auto-fix issues
```

### Agent Operations

```bash
gt status              # Overall town status
gt rigs                # List all rigs
gt polecats <rig>      # List polecats in a rig
```

### Communication

```bash
gt inbox               # Check inbox
gt send <addr> -s "Subject" -m "Message"
gt inject <polecat> "Message"    # Direct injection to session
gt capture <polecat> "<cmd>"     # Run command in polecat session
```

### Session Management

```bash
gt spawn --issue <id>  # Start polecat on issue
gt kill <polecat>      # Kill polecat session
gt wake <polecat>      # Mark polecat as active
gt sleep <polecat>     # Mark polecat as inactive
```

### Emergency Operations

```bash
gt stop --all              # Kill ALL sessions (emergency halt)
gt stop --rig <name>       # Kill all sessions in one rig
gt doctor --fix            # Auto-repair common issues
```

## Plugins

Gas Town supports **plugins** - but in the simplest possible way: plugins are just more agents.

### Philosophy

Gas Town is intentionally rough and lightweight. A "credible plugin system" with manifests, schemas, and invocation frameworks would be pretentious for a project named after a Mad Max wasteland. Instead, plugins follow the same patterns as all Gas Town agents:

- **Identity**: Plugins have persistent identities like polecats and witnesses
- **Communication**: Plugins use mail for input/output
- **Artifacts**: Plugins produce beads, files, or other handoff artifacts
- **Lifecycle**: Plugins can be invoked on-demand or at specific workflow points

### Plugin Structure

Plugins live in a rig's `plugins/` directory:

```
wyvern/                            # Rig
├── plugins/
│   └── merge-oracle/              # A plugin
│       ├── rig/                   # Plugin's git clone (if needed)
│       ├── CLAUDE.md              # Plugin's instructions/prompts
│       ├── mail/inbox.jsonl       # Plugin's mailbox
│       └── state.json             # Plugin state (optional)
```

That's it. No plugin.yaml, no special registration. If the directory exists, the plugin exists.

### Invoking Plugins

Plugins are invoked like any other agent - via mail:

```bash
# Refinery asks merge-oracle to analyze pending changesets
gt send wyvern/plugins/merge-oracle -s "Analyze merge queue" -m "..."

# Mayor asks plan-oracle for a work breakdown
gt send beads/plugins/plan-oracle -s "Plan for bd-xyz" -m "..."
```

Plugins do their work (potentially spawning Claude sessions) and respond via mail, creating any necessary artifacts (beads, files, branches).

### Hook Points

Existing agents can be configured to notify plugins at specific points. This is just convention - agents check if a plugin exists and mail it:

| Workflow Point | Agent | Example Plugin |
|----------------|-------|----------------|
| Before merge processing | Refinery | merge-oracle |
| Before swarm dispatch | Mayor | plan-oracle |
| On worker stuck | Witness | debug-oracle |
| On PR ready | Refinery | review-oracle |

Configuration is minimal - perhaps a line in the agent's CLAUDE.md or state.json noting which plugins to consult.

### Example: Merge Oracle

The **merge-oracle** plugin analyzes changesets before the Refinery processes them:

**Input** (via mail from Refinery):
- List of pending changesets
- Current merge queue state

**Processing**:
1. Build overlap graph (which changesets touch same files/regions)
2. Classify disjointness (fully disjoint → parallel safe, overlapping → needs sequencing)
3. Use LLM to assess semantic complexity of overlapping components
4. Identify high-risk patterns (deletions vs modifications, conflicting business logic)

**Output**:
- Bead with merge plan (parallel groups, sequential chains)
- Mail to Refinery with recommendation (proceed / escalate to Mayor)
- If escalation needed: mail to Mayor with explanation

The merge-oracle's `CLAUDE.md` contains the prompts and classification criteria. Gas Town doesn't need to know the internals.

### Example: Plan Oracle

The **plan-oracle** plugin helps decompose work:

**Input**: An issue/epic that needs breakdown

**Processing**:
1. Analyze the scope and requirements
2. Identify dependencies and blockers
3. Estimate complexity (for parallelization decisions)
4. Suggest task breakdown

**Output**:
- Beads for the sub-tasks (created via `bd create`)
- Dependency links (via `bd dep add`)
- Mail back with summary and recommendations

### Why This Design

1. **Fits Gas Town's aesthetic**: Rough, text-based, agent-shaped
2. **Zero new infrastructure**: Uses existing mail, beads, identities
3. **Composable**: Plugins can invoke other plugins
4. **Debuggable**: Just look at mail logs and bead history
5. **Extensible**: Anyone can add a plugin by creating a directory

### Plugin Discovery

```bash
gt plugins <rig>           # List plugins in a rig
gt plugin status <name>    # Check plugin state
```

Or just `ls <rig>/plugins/`.

## Failure Modes and Recovery

Gas Town is designed for resilience. Common failure modes and their recovery:

| Failure | Detection | Recovery |
|---------|-----------|----------|
| Agent crash | Session gone, state shows 'working' | `gt doctor` detects, reset state to idle |
| Git dirty state | Witness pre-kill check fails | Nudge worker, or manual commit/discard |
| Beads sync conflict | `bd sync` fails | Beads tombstones handle most cases |
| Tmux crash | All sessions inaccessible | `gt doctor --fix` cleans up |
| Stuck swarm | No progress for 30+ minutes | Witness escalates, Overseer intervenes |
| Disk full | Write operations fail | Clean logs, remove old clones |

### Recovery Principles

1. **Fail safe**: Prefer stopping over corrupting data
2. **State is recoverable**: Git and beads have built-in recovery
3. **Doctor heals**: `gt doctor --fix` handles common issues
4. **Emergency stop**: `gt stop --all` as last resort
5. **Human escalation**: Some failures need Overseer intervention

### Doctor Checks

`gt doctor` performs health checks at both workspace and rig levels:

**Workspace checks**: Config validity, Mayor mailbox, rig registry
**Rig checks**: Git state, clone health, Witness/Refinery presence
**Swarm checks**: Stuck detection, zombie sessions, heartbeat health

Run `gt doctor` regularly. Run `gt doctor --fix` to auto-repair issues.

## Future: Federation

Federation enables work distribution across multiple machines via SSH. Not yet implemented, but the architecture supports:
- Machine registry (local, ssh, gcp)
- Extended addressing: `[machine:]rig/polecat`
- Cross-machine mail routing
- Remote session management

## Implementation Status

Gas Town is being ported from Python (gastown-py) to Go (gastown). The Go port (GGT) is in development:

- **Epic**: gt-u1j (Port Gas Town to Go)
- **Scaffolding**: gt-u1j.1 (Go scaffolding - blocker for implementation)
- **Management**: gt-f9x (Town & Rig Management: install, doctor, federation)

See beads issues with `bd list --status=open` for current work items.
