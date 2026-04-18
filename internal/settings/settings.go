// Package settings stores per-user defaults that apply across every
// project. Single file at ~/.buttons/settings.json (mode 0600), nested
// under a "defaults" block so new settings can land without a schema
// bump.
//
// Why global-only: settings capture personal preference (how patient
// are you; which runtime do you usually reach for). Making them
// project-local would force every contributor to inherit another
// person's taste, which isn't what anyone wants. Project-level knobs
// exist at the per-button level (`buttons create --timeout N`).
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const schemaVersion = 1

// Settings is the on-disk shape. Defaults is nested so adding sibling
// blocks later (e.g. "ui", "history") doesn't require a v2 schema.
type Settings struct {
	SchemaVersion int      `json:"schema_version"`
	Defaults      Defaults `json:"defaults"`
}

// Defaults collects every "used when the command didn't override"
// knob. Pointer fields so unset keys can be distinguished from
// zero-valued keys — we never want to apply a 0-second timeout
// silently because someone set the key and then blanked the value.
type Defaults struct {
	TimeoutSeconds *int `json:"timeout_seconds,omitempty"`
}

// Service is the file-backed settings store. One file, one instance.
type Service struct {
	path string
}

// NewService constructs a Service with the settings file anchored at
// globalDir/settings.json. globalDir should be the per-user buttons
// home (normally ~/.buttons, or $BUTTONS_HOME when set for tests).
func NewService(globalDir string) *Service {
	return &Service{path: filepath.Join(globalDir, "settings.json")}
}

// NewServiceFromEnv resolves the settings path the same way the CLI
// does so tests and production share one code path. $BUTTONS_HOME
// overrides; otherwise ~/.buttons.
func NewServiceFromEnv() (*Service, error) {
	globalDir := os.Getenv("BUTTONS_HOME")
	if globalDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("could not determine home directory: %w", err)
		}
		globalDir = filepath.Join(home, ".buttons")
	}
	return NewService(globalDir), nil
}

// ErrUnknownKey is returned when Set / Unset sees a key this build
// doesn't recognise — better than silently writing a key nothing
// reads, which is confusing when settings seem to have "no effect."
var ErrUnknownKey = errors.New("unknown settings key")

// Known flat keys. Kept in one place so the CLI, Get, and Set agree
// on what exists.
const (
	KeyDefaultTimeout = "default-timeout"
)

// Load reads the settings file. A missing file returns a zero-value
// Settings (with schema_version set) so callers don't have to
// special-case first-run. A malformed file surfaces as an error —
// silent reset would lose configuration.
func (s *Service) Load() (*Settings, error) {
	data, err := os.ReadFile(s.path) // #nosec G304 -- path fixed to settings.json under globalDir
	if err != nil {
		if os.IsNotExist(err) {
			return &Settings{SchemaVersion: schemaVersion}, nil
		}
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}
	var st Settings
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	if st.SchemaVersion == 0 {
		st.SchemaVersion = schemaVersion
	}
	return &st, nil
}

// Save writes the settings file with 0o600 perms, creating the parent
// directory if necessary. Always refreshes schema_version so a hand-
// edited file can't accidentally downgrade.
func (s *Service) Save(st *Settings) error {
	st.SchemaVersion = schemaVersion
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create parent of %s: %w", s.path, err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	return nil
}

// Set updates a known key with a string value (parsed per the key's
// type) and persists immediately. Unknown keys return ErrUnknownKey.
func (s *Service) Set(key, value string) error {
	st, err := s.Load()
	if err != nil {
		return err
	}
	switch key {
	case KeyDefaultTimeout:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s expects an integer, got %q", key, value)
		}
		if n <= 0 {
			return fmt.Errorf("%s must be > 0, got %d", key, n)
		}
		st.Defaults.TimeoutSeconds = &n
	default:
		return fmt.Errorf("%w: %q", ErrUnknownKey, key)
	}
	return s.Save(st)
}

// Unset clears a known key and persists. No-op if the key wasn't set.
func (s *Service) Unset(key string) error {
	st, err := s.Load()
	if err != nil {
		return err
	}
	switch key {
	case KeyDefaultTimeout:
		st.Defaults.TimeoutSeconds = nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownKey, key)
	}
	return s.Save(st)
}

// DefaultTimeout returns (value, true) when the user has set one, else
// (0, false). Caller supplies its own fallback so this package doesn't
// have to agree with every caller on what "fallback when unset" is.
func (st *Settings) DefaultTimeout() (int, bool) {
	if st == nil || st.Defaults.TimeoutSeconds == nil {
		return 0, false
	}
	return *st.Defaults.TimeoutSeconds, true
}
