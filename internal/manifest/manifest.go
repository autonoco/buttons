package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/autonoco/buttons/internal/config"
)

const SchemaVersion = 1

var exactVersionPattern = regexp.MustCompile(`^[0-9]+$`)

type Manifest struct {
	SchemaVersion int               `json:"schema_version"`
	Dependencies  map[string]string `json:"dependencies"`
}

func New() *Manifest {
	return &Manifest{SchemaVersion: SchemaVersion, Dependencies: map[string]string{}}
}

func Path() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "buttons.json"), nil
}

func Load() (*Manifest, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return LoadPath(path)
}

func LoadPath(path string) (*Manifest, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is resolved from Buttons data dir or a test path.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func Save(m *Manifest) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return SavePath(path, m)
}

func SavePath(path string, m *Manifest) error {
	if m == nil {
		m = New()
	}
	if err := m.Validate(); err != nil {
		return err
	}
	m.SchemaVersion = SchemaVersion
	if m.Dependencies == nil {
		m.Dependencies = map[string]string{}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	return atomicWrite(path, data, 0o600)
}

func (m *Manifest) Validate() error {
	if m.SchemaVersion == 0 {
		m.SchemaVersion = SchemaVersion
	}
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported buttons.json schema_version %d", m.SchemaVersion)
	}
	if m.Dependencies == nil {
		m.Dependencies = map[string]string{}
	}
	for name, requested := range m.Dependencies {
		if err := ValidatePackageName(name); err != nil {
			return err
		}
		if err := ValidateRequest(requested); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func ValidatePackageName(name string) error {
	bad := fmt.Errorf("package name must be scoped like @desk/name, got %q", name)
	if !strings.HasPrefix(name, "@") {
		return bad
	}
	parts := strings.Split(name, "/")
	if len(parts) != 2 || len(parts[0]) <= 1 || parts[1] == "" {
		return bad
	}
	if strings.ContainsAny(parts[0][1:], `\`) || strings.ContainsAny(parts[1], `/\`) {
		return bad
	}
	return nil
}

func ValidateRequest(requested string) error {
	if requested == "latest" || exactVersionPattern.MatchString(requested) {
		return nil
	}
	return fmt.Errorf("version must be latest or an exact number like 1, got %q", requested)
}

func IsFloating(requested string) bool {
	return requested == "latest"
}

func ParsePackageSpec(spec string) (name, requested string, err error) {
	if spec == "" {
		return "", "", fmt.Errorf("package name cannot be empty")
	}
	i := strings.LastIndex(spec, "@")
	explicitVersion := false
	if strings.HasPrefix(spec, "@") && i > 0 {
		name, requested = spec[:i], spec[i+1:]
		explicitVersion = true
	} else {
		name, requested = spec, "latest"
	}
	if err := ValidatePackageName(name); err != nil {
		return "", "", err
	}
	if explicitVersion && requested == "latest" {
		return "", "", fmt.Errorf("use %q for latest; explicit @version must be an exact version", name)
	}
	if err := ValidateRequest(requested); err != nil {
		return "", "", err
	}
	return name, requested, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
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
