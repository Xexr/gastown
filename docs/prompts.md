# Gas Town Prompt Architecture

This document defines the canonical prompt system for Go Gas Town (GGT). Prompts are the personality and competence instructions that shape each agent's behavior.

## Design Principles

1. **Role-specific context**: Each agent type gets tailored prompts for their responsibilities
2. **Recovery-friendly**: Prompts include recovery instructions for context loss
3. **Command-focused**: Essential commands prominently featured
4. **Checklist-driven**: Critical workflows enforced via checklists
5. **Handoff-aware**: Session cycling built into prompt design

## Prompt Categories

### 1. Role Prompts (Primary Context)

Delivered via `gt prime` command. These establish agent identity and capabilities.

| Role | Template | Purpose |
|------|----------|---------|
| Mayor | `mayor.md` | Global coordination, swarm dispatch, cross-rig decisions |
| Witness | `witness.md` | Worker monitoring, nudging, pre-kill verification, session cycling |
| Refinery | `refinery.md` | Merge queue processing, PR review, integration |
| Polecat | `polecat.md` | Implementation work on assigned issues |
| Crew | `crew.md` | Overseer's personal workspace, user-managed, persistent identity |
| Unknown | `unknown.md` | Fallback when role detection fails |

### 2. Mail Templates (Structured Messages)

Parseable messages for swarm pipeline communication.

| Template | Flow | Purpose |
|----------|------|---------|
| `SWARM_STARTED` | Refinery → Mayor | Swarm initialized, workers ready |
| `WORKER_READY` | Worker → Refinery | Worker setup complete |
| `WORK_COMPLETE` | Worker → Refinery | Task finished, ready for merge |
| `MERGE_CONFLICT` | Refinery → Worker/Mayor | Merge failed, needs resolution |
| `SWARM_COMPLETE` | Refinery → Mayor | All tasks done |
| `SWARM_FAILED` | Refinery → Mayor | Swarm failed, needs intervention |

### 3. Spawn Injection (Work Assignment)

Prompts injected when assigning work to agents.

| Context | Content |
|---------|---------|
| New polecat | Issue details, commands, completion checklist |
| Reused polecat | Compact issue summary, `bd show` reminder |
| Ephemeral worker | Issue details with read-only beads instructions |

### 4. Lifecycle Templates (Session Management)

Templates for session cycling and handoffs.

| Template | Purpose |
|----------|---------|
| `HANDOFF` | Session-to-self handoff with state capture |
| `ESCALATION` | Worker → Witness → Mayor escalation |
| `NUDGE` | Witness → Worker progress reminder |

## PGT Prompt Inventory

### Role Prompts (templates/*.md.j2)

**mayor.md.j2** (~100 lines)
- Role identification and scope
- Workspace locations (`~/ai/mayor/rigs/<rig>/`)
- Directory structure diagram
- Essential commands (status, inbox, spawn, refinery)
- Working on code and beads section
- Delegation rules
- Session end checklist with handoff protocol

**polecat.md.j2** (~120 lines)
- Role identification with rig/polecat name injection
- Ephemeral mode conditional sections
- Critical "work in YOUR directory only" warning
- Essential commands (finding work, working, completing)
- Ephemeral-specific status reporting commands
- Required completion checklist (bd close FIRST)
- "If you get stuck" section

**refinery.md.j2** (~70 lines)
- Role identification with rig name
- Directory structure showing refinery/rig/ location
- Polecat management commands
- Work management and sync commands
- Workflow steps (inbox → ready → spawn → monitor → report)
- Swarm management commands
- Session end checklist

**unknown.md.j2** (~20 lines)
- Location unknown message
- Navigation suggestions
- Orientation commands

### Static Fallbacks (prime_cmd.py)

Each role has a `_get_<role>_context_static()` function providing fallback prompts when Jinja2 is unavailable. These are simplified versions of the templates.

### Mail Templates (mail/templates.py)

~450 lines of structured message generators:
- Metadata format: `[KEY]: value` lines for parsing
- Human-readable context below metadata
- Priority levels (normal, high)
- Message types (NOTIFICATION, TASK)

### Spawn Injection (spawn.py)

**send_initial_prompt()** (~40 lines)
- Issue ID and title
- Description (if available)
- Additional instructions
- Command reference (bd show, bd update, bd close)

**Direct injection prompts** (inline)
- Compact single-line format for tmux injection
- Issue ID, title, extra prompt, completion reminder

## Gap Analysis

### Missing Prompts

| Gap | Priority | Notes |
|-----|----------|-------|
| **Witness role prompt** | P0 | New role, no prompts exist |
| Session handoff template | P1 | Currently ad-hoc in agent logic |
| Escalation message template | P1 | No standard format |
| Nudge message template | P2 | Witness → Worker reminders |
| Direct landing workflow | P2 | Mayor bypass of Refinery |

### Missing Features

| Feature | Priority | Notes |
|---------|----------|-------|
| Prompt versioning | P2 | No way to track prompt changes |
| Prompt testing | P3 | No validation that prompts work |
| Dynamic context | P2 | Templates don't adapt to agent state |

## GGT Architecture Recommendations

### 1. Prompt Storage

```
gastown/
├── prompts/
│   ├── roles/
│   │   ├── mayor.md
│   │   ├── witness.md
│   │   ├── refinery.md
│   │   ├── polecat.md
│   │   ├── crew.md
│   │   └── unknown.md
│   ├── mail/
│   │   ├── swarm_started.md
│   │   ├── work_complete.md
│   │   └── ...
│   ├── spawn/
│   │   ├── new_polecat.md
│   │   ├── reuse_polecat.md
│   │   └── ephemeral_worker.md
│   └── lifecycle/
│       ├── handoff.md
│       ├── escalation.md
│       └── nudge.md
```

### 2. Template Engine

Use Go's `text/template` with a simple context struct:

```go
type PromptContext struct {
    Role        string
    RigName     string
    PolecatName string
    Ephemeral   bool
    IssueID     string
    IssueTitle  string
    // ... additional fields
}
```

### 3. Prompt Registry

```go
type PromptRegistry struct {
    roles     map[string]*template.Template
    mail      map[string]*template.Template
    spawn     map[string]*template.Template
    lifecycle map[string]*template.Template
}

func (r *PromptRegistry) Render(category, name string, ctx PromptContext) (string, error)
```

### 4. CLI Integration

```bash
gt prime              # Auto-detect role, render appropriate prompt
gt prime --mayor      # Force mayor prompt
gt prime --witness    # Force witness prompt
gt prompt render <category>/<name> --context '{"rig": "foo"}'
gt prompt list        # List all available prompts
gt prompt validate    # Check all prompts parse correctly
```

## Witness Prompt Design

The Witness is a new role - the per-rig "pit boss" who monitors workers. Here's the canonical prompt:

```markdown
# Gastown Witness Context

> **Recovery**: Run `gt prime` after compaction, clear, or new session

## Your Role: WITNESS (Pit Boss for {{ rig_name }})

You are the per-rig worker monitor. You watch polecats, nudge them toward completion,
verify clean git state before kills, and escalate stuck workers to the Mayor.

**You do NOT do implementation work.** Your job is oversight, not coding.

## Your Workspace

You work from: `~/ai/{{ rig_name }}/witness/`

You monitor polecats in: `~/ai/{{ rig_name }}/polecats/*/`

## Core Responsibilities

1. **Monitor workers**: Track polecat health and progress
2. **Nudge**: Prompt slow workers toward completion
3. **Pre-kill verification**: Ensure git state is clean before killing sessions
4. **Session lifecycle**: Kill sessions, update worker state
5. **Self-cycling**: Hand off to fresh session when context fills
6. **Escalation**: Report stuck workers to Mayor

**Key principle**: You own ALL per-worker cleanup. Mayor is never involved in routine worker management.

## Essential Commands

### Monitoring
- `gt polecats {{ rig_name }}` - List all polecats and status
- `gt polecat status <name>` - Detailed polecat status
- `gt polecat git-state <name>` - Check git cleanliness

### Worker Management
- `gt polecat nudge <name> "message"` - Send nudge to worker
- `gt polecat kill <name>` - Kill worker session (after verification!)
- `gt polecat wake <name>` - Mark worker as active
- `gt polecat sleep <name>` - Mark worker as inactive

### Communication
- `gt inbox` - Check your messages
- `gt send mayor/ -s "Subject" -m "Message"` - Escalate to Mayor
- `gt send {{ rig_name }}/<polecat> -s "Subject" -m "Message"` - Message worker

### Beads
- `bd list --status=in_progress` - Active work in this rig
- `bd show <id>` - Issue details

## Pre-Kill Verification Checklist

Before killing ANY polecat session, verify:

```
[ ] 1. gt polecat git-state <name>    # Must be clean
[ ] 2. Check for uncommitted work     # git status in polecat dir
[ ] 3. Check for unpushed commits     # git log origin/main..HEAD
[ ] 4. Verify issue closed            # bd show <id> shows closed
```

If git state is dirty:
1. Nudge the worker to clean up
2. Wait for response (give 2-3 nudges max)
3. If still dirty after 3 nudges → Escalate to Mayor

## Nudge Protocol

When a worker seems stuck or slow:

1. **First nudge** (gentle): "How's progress on <issue>? Need any help?"
2. **Second nudge** (direct): "Please wrap up <issue> soon. What's blocking you?"
3. **Third nudge** (final): "Final check on <issue>. If blocked, I'll escalate to Mayor."
4. **Escalate**: If no progress after 3 nudges, send escalation to Mayor

Use: `gt polecat nudge <name> "<message>"`

## Escalation Template

When escalating to Mayor:

```
gt send mayor/ -s "Escalation: <polecat> stuck on <issue>" -m "
Worker: <polecat>
Issue: <issue-id>
Status: <description of problem>

Attempts:
- Nudge 1: <date/time> - <response or no response>
- Nudge 2: <date/time> - <response or no response>
- Nudge 3: <date/time> - <response or no response>

Git state: <clean/dirty - details if dirty>

Recommendation: <what you think should happen>
"
```

## Session Self-Cycling

When your context fills up:

1. Capture current state (active workers, pending nudges, recent events)
2. Send handoff to yourself:
   ```
   gt send {{ rig_name }}/witness -s "🤝 HANDOFF: Witness session cycle" -m "
   Active workers: <list>
   Pending nudges: <list>
   Recent escalations: <list>
   Notes: <anything important>
   "
   ```
3. Exit cleanly

## Session End Checklist

```
[ ] gt polecats {{ rig_name }}     (check all worker states)
[ ] Review any pending nudges
[ ] Escalate any truly stuck workers
[ ] HANDOFF if work incomplete:
    gt send {{ rig_name }}/witness -s "🤝 HANDOFF: ..." -m "..."
```
```

## Crew Prompt Design

Crew workers are the overseer's personal workspaces - a new role that differs from polecats:

```markdown
# Gas Town Crew Worker Context

> **Recovery**: Run `gt prime` after compaction, clear, or new session

## Your Role: CREW WORKER ({{ name }} in {{ rig }})

You are a **crew worker** - the overseer's (human's) personal workspace within the {{ rig }} rig.
Unlike polecats which are witness-managed and ephemeral, you are:

- **Persistent**: Your workspace is never auto-garbage-collected
- **User-managed**: The overseer controls your lifecycle, not the Witness
- **Long-lived identity**: You keep your name ({{ name }}) across sessions
- **Integrated**: Mail and handoff mechanics work just like other Gas Town agents

**Key difference from polecats**: No one is watching you. You work directly with the overseer.

## Your Workspace

You work from: `{{ workspace_path }}`

This is a full git clone of the project repository.

## Essential Commands

### Finding Work
- `gt mail inbox` - Check your messages
- `bd ready` - Available issues (if beads configured)
- `bd list --status=in_progress` - Your active work

### Working
- `bd update <id> --status=in_progress` - Claim an issue
- `bd show <id>` - View issue details
- Standard git workflow (status, add, commit, push)

### Completing Work
- `bd close <id>` - Close the issue
- `bd sync` - Sync beads changes

## Context Cycling (Handoff)

When context fills up, send a handoff mail to yourself:

```bash
gt mail send {{ rig }}/{{ name }} -s "HANDOFF: Work in progress" -m "
Working on: <issue>
Branch: <branch>
Status: <done/remaining>
Next steps: <list>
"
```

Or use: `gt crew refresh {{ name }}`

## No Witness Monitoring

Unlike polecats, crew workers have no Witness oversight:
- No automatic nudging
- No pre-kill verification
- No escalation on blocks
- No automatic cleanup

**You are responsible for**: Managing progress, asking for help, keeping git clean.

## Session End Checklist

```
[ ] git status / git push
[ ] bd sync (if configured)
[ ] Check inbox
[ ] HANDOFF if incomplete
```
```

## Implementation Plan

### Phase 1: Core Role Prompts
1. Port mayor.md.j2 to Go template
2. Create witness.md (new!)
3. Port refinery.md.j2
4. Port polecat.md.j2
5. Create crew.md (new!)
6. Port unknown.md.j2

### Phase 2: Mail Templates
1. Define mail template format in Go
2. Port swarm pipeline templates
3. Add handoff and escalation templates

### Phase 3: Spawn & Lifecycle
1. Port spawn injection prompts
2. Add lifecycle templates (nudge, escalation)
3. Integrate with `gt spawn` command

### Phase 4: CLI & Validation
1. Implement `gt prime` with role detection
2. Add `gt prompt` subcommands
3. Add prompt validation/testing

## Related Issues

- `gt-u1j`: Port Gas Town to Go (parent epic)
- `gt-f9x`: Town & Rig Management
- `gt-cik`: Overseer Crew: User-managed persistent workspaces
- `gt-iib`: Decentralized rig structure (affects prompt paths)
