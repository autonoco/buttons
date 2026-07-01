package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/autonoco/buttons/internal/config"
)

type Lockfile struct {
	SchemaVersion int                  `json:"schema_version"`
	Dependencies  map[string]LockEntry `json:"dependencies"`
}

type LockEntry struct {
	Kind          string `json:"kind"`
	Requested     string `json:"requested"`
	Version       string `json:"version"`
	ContentHash   string `json:"content_hash"`
	InstalledName string `json:"installed_name"`
	ResolvedAt    string `json:"resolved_at"`
}

func NewLockfile() *Lockfile {
	return &Lockfile{SchemaVersion: SchemaVersion, Dependencies: map[string]LockEntry{}}
}

func LockPath() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "buttons-lock.json"), nil
}

func LoadLockfile() (*Lockfile, error) {
	path, err := LockPath()
	if err != nil {
		return nil, err
	}
	return LoadLockfilePath(path)
}

func LoadLockfilePath(path string) (*Lockfile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is resolved from Buttons data dir or a test path.
	if err != nil {
		if os.IsNotExist(err) {
			return NewLockfile(), nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var lock Lockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := lock.Validate(); err != nil {
		return nil, err
	}
	return &lock, nil
}

func SaveLockfile(lock *Lockfile) error {
	path, err := LockPath()
	if err != nil {
		return err
	}
	return SaveLockfilePath(path, lock)
}

func SaveLockfilePath(path string, lock *Lockfile) error {
	if lock == nil {
		lock = NewLockfile()
	}
	if err := lock.Validate(); err != nil {
		return err
	}
	lock.SchemaVersion = SchemaVersion
	if lock.Dependencies == nil {
		lock.Dependencies = map[string]LockEntry{}
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	data = append(data, '\n')
	return atomicWrite(path, data, 0o600)
}

func (l *Lockfile) Validate() error {
	if l.SchemaVersion == 0 {
		l.SchemaVersion = SchemaVersion
	}
	if l.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported buttons-lock.json schema_version %d", l.SchemaVersion)
	}
	if l.Dependencies == nil {
		l.Dependencies = map[string]LockEntry{}
	}
	for name, entry := range l.Dependencies {
		if err := ValidatePackageName(name); err != nil {
			return err
		}
		if err := ValidateRequest(entry.Requested); err != nil {
			return fmt.Errorf("%s requested: %w", name, err)
		}
		if entry.Version == "" || !exactVersionPattern.MatchString(entry.Version) {
			return fmt.Errorf("%s version must be exact semver, got %q", name, entry.Version)
		}
		if entry.Kind != "button" && entry.Kind != "drawer" {
			return fmt.Errorf("%s kind must be button or drawer, got %q", name, entry.Kind)
		}
		if entry.ContentHash == "" {
			return fmt.Errorf("%s content_hash is required", name)
		}
		if entry.InstalledName == "" {
			return fmt.Errorf("%s installed_name is required", name)
		}
		if entry.ResolvedAt == "" {
			return fmt.Errorf("%s resolved_at is required", name)
		}
	}
	return nil
}
