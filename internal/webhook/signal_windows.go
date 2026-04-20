//go:build windows

package webhook

import "os"

// Windows has no SIGTERM; cmd.Process.Kill() is the only option, which
// Stop() falls back to after the grace period.
var signalTerm os.Signal = os.Interrupt
