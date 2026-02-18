package cmd

import (
	"fmt"
	"strings"
	"time"
)

// QueueMetadata holds queue dispatch parameters stored in a bead's description.
// Delimited by ---queue--- so it can be cleanly parsed without conflicting
// with existing description content.
type QueueMetadata struct {
	TargetRig  string `json:"target_rig"`
	Formula    string `json:"formula,omitempty"`
	Args       string `json:"args,omitempty"`
	Vars       string `json:"vars,omitempty"` // comma-separated key=value pairs
	EnqueuedAt string `json:"enqueued_at"`
	Merge      string `json:"merge,omitempty"`
	Convoy     string `json:"convoy,omitempty"`
	BaseBranch string `json:"base_branch,omitempty"`
}

const queueMetadataDelimiter = "---queue---"

// FormatQueueMetadata formats metadata as key-value lines for bead description.
func FormatQueueMetadata(m *QueueMetadata) string {
	var lines []string
	lines = append(lines, queueMetadataDelimiter)

	if m.TargetRig != "" {
		lines = append(lines, fmt.Sprintf("target_rig: %s", m.TargetRig))
	}
	if m.Formula != "" {
		lines = append(lines, fmt.Sprintf("formula: %s", m.Formula))
	}
	if m.Args != "" {
		lines = append(lines, fmt.Sprintf("args: %s", m.Args))
	}
	if m.Vars != "" {
		lines = append(lines, fmt.Sprintf("vars: %s", m.Vars))
	}
	if m.EnqueuedAt != "" {
		lines = append(lines, fmt.Sprintf("enqueued_at: %s", m.EnqueuedAt))
	}
	if m.Merge != "" {
		lines = append(lines, fmt.Sprintf("merge: %s", m.Merge))
	}
	if m.Convoy != "" {
		lines = append(lines, fmt.Sprintf("convoy: %s", m.Convoy))
	}
	if m.BaseBranch != "" {
		lines = append(lines, fmt.Sprintf("base_branch: %s", m.BaseBranch))
	}

	return strings.Join(lines, "\n")
}

// ParseQueueMetadata extracts queue metadata from a bead description.
// Returns nil if no ---queue--- section is found.
func ParseQueueMetadata(description string) *QueueMetadata {
	idx := strings.Index(description, queueMetadataDelimiter)
	if idx < 0 {
		return nil
	}

	section := description[idx+len(queueMetadataDelimiter):]
	m := &QueueMetadata{}

	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Stop at a second delimiter or non-kv line
		if line == queueMetadataDelimiter {
			break
		}

		parts := strings.SplitN(line, ": ", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "target_rig":
			m.TargetRig = val
		case "formula":
			m.Formula = val
		case "args":
			m.Args = val
		case "vars":
			m.Vars = val
		case "enqueued_at":
			m.EnqueuedAt = val
		case "merge":
			m.Merge = val
		case "convoy":
			m.Convoy = val
		case "base_branch":
			m.BaseBranch = val
		}
	}

	return m
}

// StripQueueMetadata removes the ---queue--- section from a bead description.
// Used when dequeuing a bead for dispatch (clean up the metadata).
func StripQueueMetadata(description string) string {
	idx := strings.Index(description, queueMetadataDelimiter)
	if idx < 0 {
		return description
	}
	return strings.TrimRight(description[:idx], "\n")
}

// NewQueueMetadata creates a QueueMetadata with the current timestamp.
func NewQueueMetadata(rigName string) *QueueMetadata {
	return &QueueMetadata{
		TargetRig:  rigName,
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
	}
}
