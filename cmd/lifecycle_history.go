package cmd

import (
	"fmt"
	"os"

	"github.com/autonoco/buttons/internal/history"
	"github.com/autonoco/buttons/internal/manifest"
)

func recordLifecycleHistory(action, packageName, requested string, installed []string, lock *manifest.Lockfile) {
	if lock == nil {
		lock = manifest.NewLockfile()
	}
	deps := make([]history.LifecycleDependency, 0, len(lock.Dependencies))
	for name, entry := range lock.Dependencies {
		deps = append(deps, history.LifecycleDependency{
			Name:          name,
			Kind:          entry.Kind,
			Requested:     entry.Requested,
			Version:       entry.Version,
			ContentHash:   entry.ContentHash,
			InstalledName: entry.InstalledName,
		})
	}
	err := history.RecordLifecycleEvent(history.LifecycleEvent{
		Action:       action,
		PackageName:  packageName,
		Requested:    requested,
		Installed:    append([]string(nil), installed...),
		Dependencies: deps,
	})
	if err != nil && !jsonOutput {
		fmt.Fprintf(os.Stderr, "warning: failed to write history: %v\n", err)
	}
}
