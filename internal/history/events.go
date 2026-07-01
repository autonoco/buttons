package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/autonoco/buttons/internal/config"
)

const lifecycleSchemaVersion = 1

type LifecycleLog struct {
	SchemaVersion int              `json:"schema_version"`
	Events        []LifecycleEvent `json:"events"`
}

type LifecycleEvent struct {
	Action       string                `json:"action"`
	OccurredAt   time.Time             `json:"occurred_at"`
	PackageName  string                `json:"package_name,omitempty"`
	Requested    string                `json:"requested,omitempty"`
	Installed    []string              `json:"installed,omitempty"`
	Dependencies []LifecycleDependency `json:"dependencies,omitempty"`
}

type LifecycleDependency struct {
	Name          string `json:"name"`
	Kind          string `json:"kind,omitempty"`
	Requested     string `json:"requested,omitempty"`
	Version       string `json:"version,omitempty"`
	ContentHash   string `json:"content_hash,omitempty"`
	InstalledName string `json:"installed_name,omitempty"`
}

func LifecyclePath() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.json"), nil
}

func LoadLifecycleLog() (*LifecycleLog, error) {
	path, err := LifecyclePath()
	if err != nil {
		return nil, err
	}
	return LoadLifecycleLogPath(path)
}

func LoadLifecycleLogPath(path string) (*LifecycleLog, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is resolved from Buttons data dir or a test path.
	if err != nil {
		if os.IsNotExist(err) {
			return &LifecycleLog{SchemaVersion: lifecycleSchemaVersion}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var log LifecycleLog
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if log.SchemaVersion == 0 {
		log.SchemaVersion = lifecycleSchemaVersion
	}
	if log.SchemaVersion != lifecycleSchemaVersion {
		return nil, fmt.Errorf("unsupported history.json schema_version %d", log.SchemaVersion)
	}
	return &log, nil
}

func RecordLifecycleEvent(event LifecycleEvent) error {
	event.OccurredAt = time.Now().UTC()
	return RecordLifecycleEventAt(event)
}

func RecordLifecycleEventAt(event LifecycleEvent) error {
	if event.Action == "" {
		return fmt.Errorf("history event action is required")
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	} else {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	sort.Slice(event.Dependencies, func(i, j int) bool {
		return event.Dependencies[i].Name < event.Dependencies[j].Name
	})

	path, err := LifecyclePath()
	if err != nil {
		return err
	}
	log, err := LoadLifecycleLogPath(path)
	if err != nil {
		return err
	}
	log.SchemaVersion = lifecycleSchemaVersion
	log.Events = append(log.Events, event)

	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal history log: %w", err)
	}
	data = append(data, '\n')
	return atomicWriteLifecycle(path, data, 0o600)
}

func atomicWriteLifecycle(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create parent of %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp for %s: %w", path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
