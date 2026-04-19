package engine

import (
	"path/filepath"
	"time"

	"github.com/autonoco/buttons/internal/button"
)

// defaultProgressPath returns the JSONL progress file path for a
// press. One file per run, co-located with the history JSON so
// `buttons history` can surface progress hints alongside the
// final result.
//
// Returns "" if we can't resolve the button's pressed dir (e.g.
// tests running without a data dir); caller treats that as "no
// progress streaming" and skips setting the env var.
func defaultProgressPath(buttonName string, started time.Time) string {
	svc := button.NewService()
	dir, err := svc.PressedDir(buttonName)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, started.UTC().Format("2006-01-02T15-04-05")+".progress.jsonl")
}

// filepathDir wraps filepath.Dir so tests (and the execute.go
// caller) don't need to import path/filepath just for this.
func filepathDir(p string) string { return filepath.Dir(p) }
