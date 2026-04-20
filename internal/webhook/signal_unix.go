//go:build !windows

package webhook

import (
	"os"
	"syscall"
)

var signalTerm os.Signal = syscall.SIGTERM
