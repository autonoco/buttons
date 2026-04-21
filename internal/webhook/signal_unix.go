//go:build !windows

package webhook

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

var signalTerm os.Signal = syscall.SIGTERM

// setProcessGroup puts the child in its own process group so we can
// signal the whole group (cloudflared + any helpers it spawns) on
// shutdown. Mirrors internal/engine/execute.go.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// stopProcessGroup sends SIGTERM to the child's process group, waits up
// to `grace` for done to fire, then SIGKILLs the group. Mirrors
// engine.killProcessGroup so stale cloudflared parents (or any
// subprocess they forked) can't outlive the tunnel's lifecycle on
// macOS/Linux.
func stopProcessGroup(cmd *exec.Cmd, done <-chan struct{}, grace time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// Fell back to direct PID — still better than nothing.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(grace):
			_ = cmd.Process.Kill()
		}
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(grace):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}
