// Package queue provides file-lock-based concurrency limits for
// button presses. Prevents an agent from hammering a rate-limited
// API by spawning N parallel presses of the same button — the
// button can declare a queue with a concurrency cap, and every
// press acquires a lock slot before executing.
//
// Storage: one directory per queue at ~/.buttons/queues/<name>/,
// with one lock file per slot (slot-0, slot-1, ...). Acquisition
// tries each slot in turn; the first one that succeeds with a
// non-blocking flock wins. Release is via close() on the file.
//
// The file-lock approach survives across CLI invocations — two
// `buttons press` processes running concurrently see each other.
// No daemon needed.
package queue

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/autonoco/buttons/internal/config"
)

// Config is the queue declaration from button.json.
type Config struct {
	Name        string `json:"name"`                  // queue id
	Concurrency int    `json:"concurrency,omitempty"` // default 1
	Key         string `json:"key,omitempty"`         // optional sub-key (e.g. per-tenant)
}

// Lock represents one acquired queue slot. Callers MUST Release()
// it when the press finishes so the slot frees for the next waiter.
type Lock struct {
	file *os.File
}

// Release frees the lock. Safe to call on a nil receiver.
func (l *Lock) Release() {
	if l == nil || l.file == nil {
		return
	}
	// Unlock + close. The OS releases the flock automatically on
	// close, so the unlock call is belt-and-suspenders.
	// #nosec G115 -- os.File.Fd() returns uintptr but syscall.Flock
	// takes int; this is the idiomatic Go pattern. File descriptor
	// values are always small positive integers (< 2^31), so the
	// conversion can't overflow in practice.
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
	// Keep the slot file on disk — it's a permanent slot, not a
	// per-lock artifact. Removing it would race with concurrent
	// acquisitions.
}

// Acquire blocks until a slot opens in the queue, polling every
// pollInterval. If the context is cancelled first, returns
// context.Cancelled without acquiring. Concurrency <= 0 is
// treated as 1.
func Acquire(cfg Config, pollInterval time.Duration, deadline time.Time) (*Lock, error) {
	if cfg.Name == "" {
		return nil, nil // no queue declared; caller runs unthrottled
	}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	dir, err := queueDir(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create queue dir: %w", err)
	}

	for {
		for slot := 0; slot < concurrency; slot++ {
			lock, err := trySlot(dir, slot)
			if err != nil {
				return nil, err
			}
			if lock != nil {
				return lock, nil
			}
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return nil, errors.New("queue acquire timeout")
		}
		time.Sleep(pollInterval)
	}
}

// trySlot attempts a non-blocking flock on slot N. Returns
// (nil, nil) on contention so the caller retries the next slot.
func trySlot(dir string, slot int) (*Lock, error) {
	path := filepath.Join(dir, fmt.Sprintf("slot-%d.lock", slot))
	// #nosec G304 -- path is rooted in QueuesDir/<queue>/; no user
	// input reaches the raw filename beyond the queue name which is
	// sanitized by config.QueuesDir + the slot integer.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open slot: %w", err)
	}
	// #nosec G115 -- see Release() for rationale; fd values are
	// small positive ints, uintptr→int conversion is safe.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, fmt.Errorf("flock: %w", err)
	}
	return &Lock{file: f}, nil
}

func queueDir(cfg Config) (string, error) {
	base, err := config.QueuesDir()
	if err != nil {
		return "", err
	}
	name := cfg.Name
	if cfg.Key != "" {
		// Scope queues by key so "openai concurrency 3 per user_id"
		// works: each user_id gets its own slot pool.
		name = cfg.Name + "__" + sanitize(cfg.Key)
	}
	return filepath.Join(base, sanitize(name)), nil
}

// sanitize keeps only characters safe for a directory name.
// Anything outside [A-Za-z0-9_-] becomes _.
func sanitize(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b = append(b, c)
		} else {
			b = append(b, '_')
		}
	}
	return string(b)
}
