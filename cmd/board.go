package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/tui"
	"github.com/spf13/cobra"
)

// boardEmbedded is an internal flag set by the window spawner when it
// re-invokes `buttons board` inside the freshly opened terminal. Hidden
// because it's an implementation detail — users never type --embedded
// themselves.
var boardEmbedded bool

// boardInline is the user-visible opt-out: force the board to render in
// the current terminal instead of popping a new window. Kept for SSH
// sessions, CI environments, and "I know what I'm doing" moments.
var boardInline bool

var boardCmd = &cobra.Command{
	Use:   "board [name]",
	Short: "Open the button board in a new terminal window",
	Long: `The board is the human UI for buttons — an always-on dashboard
that shows every button in the active project, their state, and
recent press outcomes. Agents never invoke it; they use the CLI.

By default, running ` + "`buttons board`" + ` opens the dashboard in a new
terminal window on the host OS and returns to your current shell. The
new window runs until you close it.

Use ` + "`--inline`" + ` to render the board in the current terminal instead —
helpful when you're SSH'd somewhere, inside a screen / tmux session
you want to stay in, or on a headless machine without a window server.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBoard,
}

func runBoard(cmd *cobra.Command, args []string) error {
	if jsonOutput {
		_ = config.WriteJSONError("NOT_APPLICABLE", "board is an interactive TUI; --json is not supported")
		return errSilent
	}

	var initial string
	if len(args) > 0 {
		initial = args[0]
	}

	// Run inline when:
	//   - we're the spawned child (--embedded, set by the spawner)
	//   - the user explicitly asked (--inline)
	//   - stdout is not a TTY (piped / CI / headless — spawning a GUI
	//     window would be pointless or straight-up fail)
	if boardEmbedded || boardInline || config.IsNonTTY() {
		return runBoardInline(initial)
	}

	// Default path: pop a new OS terminal window and let the current
	// shell continue unblocked.
	if err := spawnBoardWindow(initial); err != nil {
		fmt.Fprintf(os.Stderr, "could not open a new terminal window: %v\n", err)
		fmt.Fprintln(os.Stderr, "falling back to inline mode — pass --inline next time to skip the spawn attempt")
		return runBoardInline(initial)
	}

	fmt.Fprintln(os.Stderr, "opened buttons board in a new terminal window")
	return nil
}

func runBoardInline(initial string) error {
	svc := button.NewService()
	if err := tui.Run(svc, initial); err != nil {
		fmt.Fprintf(os.Stderr, "board: %v\n", err)
		return errSilent
	}
	return nil
}

// spawnBoardWindow launches a new OS-native terminal window running
// `buttons board --embedded`, then returns immediately so the parent
// shell's prompt comes back.
//
// The spawned command cd's into the parent's CWD first so project-local
// .buttons/ discovery lands on the same project the user ran it from.
// `exec` replaces the shell with the buttons process so closing the TUI
// also closes the window — no leftover shell prompt floating around.
func spawnBoardWindow(initial string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}

	// Shell command the new terminal will run. Single-quoting paths
	// keeps arbitrary characters (spaces, $, etc.) literal. `exec`
	// swaps the shell for buttons so the window closes when the TUI
	// exits rather than dropping back to a prompt.
	shellCmd := fmt.Sprintf("cd %s && exec %s board --embedded", shellQuote(cwd), shellQuote(exe))
	if initial != "" {
		shellCmd += " " + shellQuote(initial)
	}

	switch runtime.GOOS {
	case "darwin":
		return spawnDarwin(shellCmd)
	case "linux":
		return spawnLinux(shellCmd)
	case "windows":
		return spawnWindows(shellCmd)
	default:
		return fmt.Errorf("unsupported platform %q; use --inline", runtime.GOOS)
	}
}

func spawnDarwin(shellCmd string) error {
	// The shellCmd is the body of AppleScript's `do script`, a string
	// literal delimited by ". We need to escape \ and " for that
	// literal; shell itself is already happy with shellCmd as-is.
	appleLit := strings.ReplaceAll(shellCmd, `\`, `\\`)
	appleLit = strings.ReplaceAll(appleLit, `"`, `\"`)
	script := fmt.Sprintf(`tell application "Terminal"
		activate
		do script "%s"
	end tell`, appleLit)
	// #nosec G204 -- osascript args are program-controlled; shellCmd
	// is quoted via shellQuote + AppleScript escaping above, not
	// interpolated from user input.
	return exec.Command("osascript", "-e", script).Run()
}

func spawnLinux(shellCmd string) error {
	// Common Linux terminals accept different arg styles; we probe
	// in priority order. `-e` or `--` both expect a full command
	// string; passing through `sh -c` isolates us from each terminal's
	// argv idiosyncrasies.
	candidates := []struct {
		bin   string
		flags []string
	}{
		{"x-terminal-emulator", []string{"-e"}}, // Debian/Ubuntu alternatives
		{"gnome-terminal", []string{"--"}},
		{"konsole", []string{"-e"}},
		{"xfce4-terminal", []string{"-e"}},
		{"alacritty", []string{"-e"}},
		{"kitty", []string{}},
		{"wezterm", []string{"start", "--"}},
		{"xterm", []string{"-e"}},
	}
	for _, c := range candidates {
		bin, err := exec.LookPath(c.bin)
		if err != nil {
			continue
		}
		args := append([]string{}, c.flags...)
		args = append(args, "sh", "-c", shellCmd)
		// #nosec G204 -- bin resolved via exec.LookPath from a fixed
		// whitelist; shellCmd is quoted via shellQuote + passed to
		// sh -c which is the documented safe boundary.
		return exec.Command(bin, args...).Start()
	}
	return fmt.Errorf("no supported terminal emulator found on PATH")
}

func spawnWindows(shellCmd string) error {
	// Prefer Windows Terminal (`wt`) if present; fall back to cmd.
	if wt, err := exec.LookPath("wt"); err == nil {
		// #nosec G204 -- wt resolved via LookPath; shellCmd built from
		// quoted paths, passed to cmd /c which is the documented boundary.
		return exec.Command(wt, "cmd", "/c", shellCmd).Start()
	}
	// #nosec G204 -- same reasoning; cmd is the OS command processor.
	return exec.Command("cmd", "/c", "start", "cmd", "/k", shellCmd).Start()
}

// shellQuote wraps s in POSIX single quotes so the shell sees it as a
// literal string, with any embedded single quotes escaped via the
// '\''  trick (end quote, escaped quote, new start quote).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func init() {
	rootCmd.AddCommand(boardCmd)
	boardCmd.Flags().BoolVar(&boardInline, "inline", false, "render the board in the current terminal instead of spawning a new window")
	boardCmd.Flags().BoolVar(&boardEmbedded, "embedded", false, "internal: set by the window spawner; do not use directly")
	_ = boardCmd.Flags().MarkHidden("embedded")
}
