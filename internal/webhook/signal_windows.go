//go:build windows

package webhook

import (
	"os"
	"os/exec"
	"time"
)

// Windows has no SIGTERM; cmd.Process.Kill() is the only option, which
// Stop() falls back to after the grace period. os.Interrupt is
// translated by Go into a best-effort CTRL_BREAK on Windows.
var signalTerm os.Signal = os.Interrupt

// setProcessGroup is a no-op on Windows — we rely on os.Interrupt +
// Kill() for cloudflared teardown. Windows has no Setpgid equivalent
// exposed through os/exec.
func setProcessGroup(cmd *exec.Cmd) {}

// stopProcessGroup signals Interrupt, waits up to `grace`, then Kills.
// Matches the Unix semantics from the caller's perspective without the
// process-group machinery.
func stopProcessGroup(cmd *exec.Cmd, done <-chan struct{}, grace time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(grace):
		_ = cmd.Process.Kill()
	}
}
