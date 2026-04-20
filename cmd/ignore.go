package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
)

// buttons ignore / unignore writes entries to .buttons/.gitignore so
// individual buttons (or drawers) can be kept out of git without
// touching the repo's top-level .gitignore. Common use case: agents
// create lots of scratch/test buttons while iterating — those don't
// belong in a team's repo, but a few production buttons (like
// publish-npm) do. This command lets the agent opt out per-button.
//
// .buttons/.gitignore is just a standard gitignore file scoped to
// the .buttons/ subtree. Git applies it natively; no extra plumbing.

var ignoreCmd = &cobra.Command{
	Use:   "ignore [name]",
	Short: "Keep a button or drawer out of git (writes .buttons/.gitignore)",
	Long: `Adds the named button/drawer to .buttons/.gitignore so git
won't track it. Useful for scratch/test buttons an agent spins up
while iterating.

  buttons ignore NAME                — ignore a button
  buttons ignore drawer/NAME         — ignore a drawer
  buttons ignore                     — list currently-ignored entries
  buttons unignore NAME              — re-include in git
  buttons create NAME --ignore       — create + ignore in one step

The .buttons/.gitignore file is a standard gitignore scoped to the
.buttons/ subtree — git applies it natively, no extra config.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIgnore,
}

var unignoreCmd = &cobra.Command{
	Use:   "unignore [name]",
	Short: "Re-include a previously-ignored button or drawer in git",
	Args:  cobra.ExactArgs(1),
	RunE:  runUnignore,
}

func init() {
	rootCmd.AddCommand(ignoreCmd)
	rootCmd.AddCommand(unignoreCmd)
}

func runIgnore(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return listIgnored()
	}
	entry, err := normalizeIgnoreTarget(args[0])
	if err != nil {
		return err
	}
	added, err := addIgnoreEntry(entry)
	if err != nil {
		return handleServiceError(err)
	}
	if jsonOutput {
		return config.WriteJSON(map[string]any{"ok": true, "ignored": entry, "added": added})
	}
	if added {
		fmt.Fprintf(os.Stderr, "ignored: %s (added to .buttons/.gitignore)\n", entry)
	} else {
		fmt.Fprintf(os.Stderr, "already ignored: %s\n", entry)
	}
	return nil
}

func runUnignore(cmd *cobra.Command, args []string) error {
	entry, err := normalizeIgnoreTarget(args[0])
	if err != nil {
		return err
	}
	removed, err := removeIgnoreEntry(entry)
	if err != nil {
		return handleServiceError(err)
	}
	if jsonOutput {
		return config.WriteJSON(map[string]any{"ok": true, "unignored": entry, "removed": removed})
	}
	if removed {
		fmt.Fprintf(os.Stderr, "unignored: %s (removed from .buttons/.gitignore)\n", entry)
	} else {
		fmt.Fprintf(os.Stderr, "not in ignore list: %s\n", entry)
	}
	return nil
}

// normalizeIgnoreTarget turns a user-facing name ("slow",
// "drawer/deploy-flow") into the gitignore line ("buttons/slow/",
// "drawers/deploy-flow/"). Validates the name shape so a typo can't
// sneak a weird pattern into the file.
func normalizeIgnoreTarget(raw string) (string, error) {
	if strings.HasPrefix(raw, "drawer/") {
		name := strings.TrimPrefix(raw, "drawer/")
		if err := button.ValidateName(name); err != nil {
			return "", err
		}
		return "drawers/" + button.Slugify(name) + "/", nil
	}
	if strings.HasPrefix(raw, "button/") {
		raw = strings.TrimPrefix(raw, "button/")
	}
	if err := button.ValidateName(raw); err != nil {
		return "", err
	}
	return "buttons/" + button.Slugify(raw) + "/", nil
}

// addIgnoreEntry appends entry to .buttons/.gitignore if not already
// present. Returns true when a new line was added. Creates the file
// (0o600) if it doesn't exist yet.
func addIgnoreEntry(entry string) (bool, error) {
	path, err := gitignorePath()
	if err != nil {
		return false, err
	}
	existing, err := readIgnoreLines(path)
	if err != nil {
		return false, err
	}
	for _, line := range existing {
		if line == entry {
			return false, nil
		}
	}
	existing = append(existing, entry)
	sort.Strings(existing)
	return true, writeIgnoreLines(path, existing)
}

// removeIgnoreEntry deletes entry from .buttons/.gitignore. Returns
// true if the entry was present. Missing file is treated as "nothing
// to remove".
func removeIgnoreEntry(entry string) (bool, error) {
	path, err := gitignorePath()
	if err != nil {
		return false, err
	}
	existing, err := readIgnoreLines(path)
	if err != nil {
		return false, err
	}
	out := existing[:0]
	removed := false
	for _, line := range existing {
		if line == entry {
			removed = true
			continue
		}
		out = append(out, line)
	}
	if !removed {
		return false, nil
	}
	return true, writeIgnoreLines(path, out)
}

func listIgnored() error {
	path, err := gitignorePath()
	if err != nil {
		return err
	}
	lines, err := readIgnoreLines(path)
	if err != nil {
		return handleServiceError(err)
	}
	if jsonOutput {
		return config.WriteJSON(lines)
	}
	if len(lines) == 0 {
		fmt.Fprintln(os.Stderr, "no buttons or drawers are currently ignored")
		return nil
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	return nil
}

// gitignorePath is the on-disk location of the .buttons/.gitignore
// file. Lives at the same data-dir the CLI is currently operating
// on (project-local when .buttons/ is found via walk-up; global
// ~/.buttons/ otherwise).
func gitignorePath() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".gitignore"), nil
}

// readIgnoreLines returns the non-comment, non-blank lines of the
// ignore file, preserving order. Missing file = empty slice.
func readIgnoreLines(path string) ([]string, error) {
	// #nosec G304 -- path is always DataDir + ".gitignore"; no user
	// input reaches the raw filename.
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer f.Close()
	out := []string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, s.Err()
}

// writeIgnoreLines replaces the file with the provided lines, always
// ending with a trailing newline. Creates parent dirs if needed.
func writeIgnoreLines(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	content := "# Managed by `buttons ignore` / `buttons unignore`.\n" +
		"# Lines in this file are paths relative to this .buttons/ dir.\n\n"
	if len(lines) > 0 {
		content += strings.Join(lines, "\n") + "\n"
	}
	return os.WriteFile(path, []byte(content), 0o600)
}
