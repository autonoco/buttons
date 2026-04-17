// Package agentskill writes per-project agent instruction files.
//
// Three concerns live here:
//
//   1. The canonical "what Buttons is and how to use it" content, shared
//      by every target (Cursor, Claude, etc.).
//
//   2. Per-target rendering — each coding agent reads its instructions
//      from a different file with a different expected shape (MDC
//      frontmatter for Cursor, bare markdown for Claude/AGENTS.md, etc.).
//
//   3. Idempotent install: if a target file already exists, we want to
//      add or update just the Buttons section without clobbering the
//      user's other content. Every generated section is wrapped in
//      BUTTONS:START / BUTTONS:END HTML comments so a subsequent
//      `buttons init` can find + replace in-place.
//
// The `.buttons/AGENT.md` file is intentionally different: it's the
// folder's own onboarding doc, not an agent-discovery file. It's always
// created and always overwritten on `init` — it belongs to Buttons, not
// to the user's editor conventions.
package agentskill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Target identifies a coding-agent integration. The ID is stable across
// versions — it's what `--agent` takes on the CLI and what we use as
// the CLI output key in JSON mode.
type Target struct {
	ID          string // machine-readable slug
	Label       string // human-readable name for the picker
	Description string // one-line hint shown alongside the picker option
	Path        string // relative file path written into the project
	Format      format // how the body is wrapped for this target
}

type format int

const (
	// formatMarkdownSection wraps the body in BUTTONS:START/END markers
	// and appends (or updates in place) inside an existing markdown file.
	// Used for CLAUDE.md, AGENTS.md, .clinerules, copilot-instructions.
	formatMarkdownSection format = iota
	// formatCursorRule writes a Cursor .mdc file with MDC frontmatter.
	// Cursor reads files under .cursor/rules/ with this structure, so
	// the file is standalone and we can write/overwrite it whole.
	formatCursorRule
)

// Targets is the list of supported agent integrations, in the order
// they appear in the interactive picker. Order matters — the most
// widely-used tools come first.
var Targets = []Target{
	{
		ID:          "cursor",
		Label:       "Cursor",
		Description: "Writes .cursor/rules/buttons.mdc",
		Path:        ".cursor/rules/buttons.mdc",
		Format:      formatCursorRule,
	},
	{
		ID:          "claude",
		Label:       "Claude Code",
		Description: "Appends a section to CLAUDE.md",
		Path:        "CLAUDE.md",
		Format:      formatMarkdownSection,
	},
	{
		ID:          "cline",
		Label:       "Cline",
		Description: "Appends a section to .clinerules",
		Path:        ".clinerules",
		Format:      formatMarkdownSection,
	},
	{
		ID:          "copilot",
		Label:       "GitHub Copilot",
		Description: "Appends a section to .github/copilot-instructions.md",
		Path:        ".github/copilot-instructions.md",
		Format:      formatMarkdownSection,
	},
	{
		ID:          "agents-md",
		Label:       "AGENTS.md",
		Description: "Appends a section to AGENTS.md (cross-agent convention)",
		Path:        "AGENTS.md",
		Format:      formatMarkdownSection,
	},
}

// TargetByID returns the Target whose ID matches, or (_, false).
func TargetByID(id string) (Target, bool) {
	for _, t := range Targets {
		if t.ID == id {
			return t, true
		}
	}
	return Target{}, false
}

// Markers delimit the Buttons-managed section inside shared markdown
// files. Chosen to be visible to humans reading the file (so they know
// the section is managed) while staying invisible in rendered markdown.
const (
	markerStart = "<!-- BUTTONS:START -->"
	markerEnd   = "<!-- BUTTONS:END -->"
)

// Body is the shared "what Buttons is" content appended to markdown
// files. Kept short on purpose — pointers to `buttons --help` and the
// `.buttons/AGENT.md` reference carry the detailed docs.
const Body = `## Buttons

This project uses [Buttons](https://buttons.sh) — a CLI workflow engine. Buttons are reusable, named actions with typed args and structured output.

**Before writing a one-off shell command, check ` + "`buttons list`" + ` for an existing button.** If you write a script you'd want again, save it with ` + "`buttons create <name>`" + `.

Common commands:

- ` + "`buttons list --json`" + ` — see all buttons
- ` + "`buttons press <name>`" + ` — run one
- ` + "`buttons press <name> --arg key=value`" + ` — with args
- ` + "`buttons create <name>`" + ` — scaffold a shell button (edit ~/.buttons/buttons/<name>/main.sh)

See ` + "`.buttons/AGENT.md`" + ` for more detail.`

// AgentMDBody is the always-installed `.buttons/AGENT.md` content.
// Different audience than Body: this one teaches the concept to an
// agent that happens to open the file, whereas Body is structured to
// live alongside a user's existing agent instructions.
const AgentMDBody = `# Buttons

This folder is managed by [Buttons](https://buttons.sh) — a CLI workflow engine for AI agents.

## What a button is

A reusable, named action with typed args and structured output. Each one wraps a script, an HTTP call, or an instruction to an agent. Press it whenever, get the same shape back.

## For the agent reading this

Run ` + "`buttons --help`" + ` or ` + "`buttons list --json`" + ` to discover what's here. Prefer pressing an existing button over writing a new script inline; if you write a one-off script you'd want again, save it with ` + "`buttons create <name>`" + `.

Common commands:

    buttons list [--json]           see all buttons
    buttons press <name>            run one
    buttons press <name> --arg k=v  with args
    buttons create <name>           scaffold a shell button you can edit

Project-local buttons (in this folder) and global buttons (at ~/.buttons/) are both visible to ` + "`buttons list`" + `.

Full docs: run ` + "`buttons --help`" + ` or see https://buttons.sh
`

// cursorRule is the full file written to .cursor/rules/buttons.mdc.
// MDC frontmatter tells Cursor when to apply the rule; we keep it
// low-aggression (alwaysApply: false) so it's available as context
// without forcing itself into every prompt.
const cursorRule = `---
description: Buttons CLI workflow engine — use existing buttons before writing one-off scripts
alwaysApply: false
---

` + Body + `
`

// InstallOpts controls the WriteProject + WriteAgentMD pipeline.
type InstallOpts struct {
	// ProjectRoot is the directory that will receive .buttons/ plus any
	// selected target files. Normally the user's CWD when they run
	// `buttons init`.
	ProjectRoot string

	// TargetIDs is the set of target IDs to install. An empty slice
	// means "install no agent skill files" — `.buttons/AGENT.md` is
	// always written regardless.
	TargetIDs []string
}

// WriteResult is returned per-target so callers (especially --json
// output) can tell which files were created vs. updated-in-place.
type WriteResult struct {
	TargetID string `json:"target_id"`
	Path     string `json:"path"`
	Action   string `json:"action"` // "created" | "updated" | "appended"
}

// WriteAgentMD writes .buttons/AGENT.md. The `.buttons/` directory is
// assumed to exist already (buttons init creates it first). Always
// overwrites — this file belongs to Buttons, not to the user.
func WriteAgentMD(projectRoot string) (string, error) {
	path := filepath.Join(projectRoot, ".buttons", "AGENT.md")
	if err := os.WriteFile(path, []byte(AgentMDBody), 0600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// Install writes the skill file for each selected target. Returns one
// WriteResult per target (in the same order as TargetIDs). Errors stop
// the loop — caller sees partial results plus the error.
func Install(opts InstallOpts) ([]WriteResult, error) {
	results := make([]WriteResult, 0, len(opts.TargetIDs))
	for _, id := range opts.TargetIDs {
		target, ok := TargetByID(id)
		if !ok {
			return results, fmt.Errorf("unknown agent target: %s", id)
		}
		res, err := writeTarget(opts.ProjectRoot, target)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}

func writeTarget(projectRoot string, t Target) (WriteResult, error) {
	path := filepath.Join(projectRoot, t.Path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return WriteResult{}, fmt.Errorf("create parent of %s: %w", path, err)
	}

	switch t.Format {
	case formatCursorRule:
		// Cursor rules are single-purpose files — safe to overwrite
		// entirely. If the user hand-edits, the next `buttons init`
		// will reset their changes; that's acceptable for a managed rule.
		existed := fileExists(path)
		if err := os.WriteFile(path, []byte(cursorRule), 0600); err != nil {
			return WriteResult{}, fmt.Errorf("write %s: %w", path, err)
		}
		action := "created"
		if existed {
			action = "updated"
		}
		return WriteResult{TargetID: t.ID, Path: path, Action: action}, nil

	case formatMarkdownSection:
		return writeMarkdownSection(path, t.ID)
	}
	return WriteResult{}, fmt.Errorf("unknown format for target %s", t.ID)
}

// writeMarkdownSection appends or updates the Buttons-managed section
// in a shared markdown file. Behavior:
//
//   - File doesn't exist   → create it containing just the section.
//   - File exists, markers present → replace the section between markers.
//   - File exists, no markers     → append the section (after a blank line)
//     to preserve whatever the user already had.
func writeMarkdownSection(path, targetID string) (WriteResult, error) {
	section := markerStart + "\n" + Body + "\n" + markerEnd + "\n"

	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(section), 0600); err != nil {
				return WriteResult{}, fmt.Errorf("write %s: %w", path, err)
			}
			return WriteResult{TargetID: targetID, Path: path, Action: "created"}, nil
		}
		return WriteResult{}, fmt.Errorf("read %s: %w", path, err)
	}

	content := string(existing)

	// Update-in-place path: find existing markers and replace the range.
	if strings.Contains(content, markerStart) && strings.Contains(content, markerEnd) {
		startIdx := strings.Index(content, markerStart)
		endIdx := strings.Index(content, markerEnd) + len(markerEnd)
		// Guard: malformed file (end before start) — fall through to append
		// rather than producing garbage.
		if endIdx > startIdx {
			updated := content[:startIdx] + strings.TrimSuffix(section, "\n") + content[endIdx:]
			if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
				return WriteResult{}, fmt.Errorf("write %s: %w", path, err)
			}
			return WriteResult{TargetID: targetID, Path: path, Action: "updated"}, nil
		}
	}

	// Append path: preserve existing content, add a blank line separator
	// unless the file already ends with one.
	var b strings.Builder
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n\n") {
		if strings.HasSuffix(content, "\n") {
			b.WriteString("\n")
		} else {
			b.WriteString("\n\n")
		}
	}
	b.WriteString(section)

	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		return WriteResult{}, fmt.Errorf("write %s: %w", path, err)
	}
	return WriteResult{TargetID: targetID, Path: path, Action: "appended"}, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
