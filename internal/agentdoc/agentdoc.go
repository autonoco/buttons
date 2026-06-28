// Package agentdoc centralizes the per-unit agent-instruction filename.
//
// Buttons standardizes on AGENTS.md — the agents.md convention used by
// Codex, OpenClaw, Cursor, and others. AGENT.md (singular) is still
// honored on read as a legacy fallback so buttons and drawers created
// before the rename keep attaching their prompts; writes always use the
// canonical name.
package agentdoc

import (
	"os"
	"path/filepath"
)

// Name is the canonical agent-instruction filename. All writes use this.
const Name = "AGENTS.md"

// Legacy is the pre-rename filename, honored on read only.
const Legacy = "AGENT.md"

// Path returns the agent-instruction path inside dir for READING: it
// prefers the canonical AGENTS.md, falls back to a legacy AGENT.md when
// only that exists, and otherwise returns the canonical path (so a
// genuinely missing file still surfaces as a not-exist on the new name).
func Path(dir string) string {
	canonical := filepath.Join(dir, Name)
	if _, err := os.Stat(canonical); err == nil {
		return canonical
	}
	if legacy := filepath.Join(dir, Legacy); exists(legacy) {
		return legacy
	}
	return canonical
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
