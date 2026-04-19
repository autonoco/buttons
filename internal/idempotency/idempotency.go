// Package idempotency is a TTL'd cache of press results keyed by
// the user-supplied idempotency key. Protects retried presses from
// producing duplicate side effects (double charges, double sends,
// double publishes).
//
// Storage: one JSON file per key hash at ~/.buttons/idempotency/<hash>.json.
// No database, no daemon — a failed read just means cache miss and
// we execute normally, which is the safe degradation path.
//
// Agents opt in per-press via `--idempotency-key X --idempotency-ttl DUR`.
// Without a key, every press is treated as unique (the v1 auto-keys
// inside a single drawer run still apply — this package extends
// that to cross-run, cross-process scope).
package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/autonoco/buttons/internal/config"
)

// Entry is one cached result. Stored verbatim so replays are
// indistinguishable from the original press — same JSON shape,
// same fields.
type Entry struct {
	Key       string          `json:"key"`
	StoredAt  time.Time       `json:"stored_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	Result    json.RawMessage `json:"result"`
}

// Expired returns true if the entry is past its TTL. Callers treat
// expired entries as cache misses.
func (e *Entry) Expired() bool {
	return time.Now().After(e.ExpiresAt)
}

// Lookup returns the cached entry for a key, or nil on cache miss.
// Expired entries return nil and are reaped as a side effect so
// callers don't carry that burden.
func Lookup(key string) (*Entry, error) {
	if key == "" {
		return nil, nil
	}
	path, err := pathFor(key)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- path built from sha256(key) under IdempotencyDir;
	// no user input reaches the raw filename.
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		// Corrupt cache file — treat as miss and unlink so the next
		// write can land cleanly.
		_ = os.Remove(path)
		return nil, nil
	}
	if e.Expired() {
		_ = os.Remove(path)
		return nil, nil
	}
	return &e, nil
}

// Store writes a result under the given key with the given TTL.
// A nil or empty key is a no-op so callers don't need to branch.
func Store(key string, ttl time.Duration, result any) error {
	if key == "" {
		return nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	now := time.Now().UTC()
	e := Entry{
		Key:       key,
		StoredAt:  now,
		ExpiresAt: now.Add(ttl),
		Result:    raw,
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	path, err := pathFor(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir idempotency dir: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// Delete removes a cache entry. Used by `buttons dlq replay` and
// similar tools that want to force re-execution.
func Delete(key string) error {
	if key == "" {
		return nil
	}
	path, err := pathFor(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Reap scans the idempotency dir and deletes expired entries. Not
// called automatically anywhere in the hot path; exposed so a
// future `buttons gc` or scheduled job can sweep. Lookup() already
// reaps on read so this is only for entries that are never read again.
func Reap() (int, error) {
	dir, err := config.IdempotencyDir()
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, de := range entries {
		if de.IsDir() || filepath.Ext(de.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, de.Name())
		// #nosec G304 -- path is rooted in IdempotencyDir + a
		// DirEntry name we just enumerated from os.ReadDir.
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		if e.Expired() {
			if err := os.Remove(path); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}

func pathFor(key string) (string, error) {
	dir, err := config.IdempotencyDir()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(h[:]) + ".json"
	return filepath.Join(dir, name), nil
}
