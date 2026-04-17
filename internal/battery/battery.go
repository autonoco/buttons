// Package battery stores per-user and per-project environment variables
// and secrets. Values are injected into button presses as
// BUTTONS_BAT_<KEY>=<value> so shell / code buttons can read
// `$BUTTONS_BAT_APIFY_TOKEN` without the token being baked into the
// script file.
//
// Storage:
//
//	Global batteries  ~/.buttons/batteries.json
//	Project batteries <project>/.buttons/batteries.json
//
// Both files are JSON maps of KEY → VALUE, wrapped in a small envelope
// that carries a schema_version for future migrations. When a press
// runs, the active environment is: global batteries + project batteries
// (project overrides on key collision). Buttons don't see the scope
// the value came from — only its KEY and VALUE.
package battery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// envelopeSchemaVersion is the top-level version tag stored in every
// batteries.json file. Bump this when the on-disk shape changes.
const envelopeSchemaVersion = 1

// envelope is the on-disk shape of batteries.json. Storing batteries
// under a nested key leaves room to add sibling fields later without a
// v2 migration.
type envelope struct {
	SchemaVersion int               `json:"schema_version"`
	Batteries     map[string]string `json:"batteries"`
}

// Scope names where a battery lives. A Scope is both an input (which
// file to write) and an output (which file a list entry came from).
type Scope string

const (
	// ScopeGlobal is ~/.buttons/batteries.json. Available in every press.
	ScopeGlobal Scope = "global"
	// ScopeLocal is the project .buttons/batteries.json for the active
	// project directory. Only meaningful when running from inside a
	// project tree; overrides ScopeGlobal on key collision.
	ScopeLocal Scope = "local"
)

// keyPattern enforces POSIX env-var-friendly names. A malformed key
// can't be exported to an exec.Cmd reliably and tends to indicate the
// caller confused KEY and VALUE argument order.
var keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// Entry is one battery as surfaced by List. Value is the raw stored
// string; redaction is the CLI's job, not the store's.
type Entry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Scope Scope  `json:"scope"`
}

// Service is the file-backed battery store. Paths are resolved once at
// construction; constructing a new Service is the way to react to a
// cwd change mid-process (none of the CLI commands do that today).
type Service struct {
	globalPath string
	// localPath is "" when the caller isn't inside a project tree. Set
	// / rm operations that target local in that case return an error
	// rather than silently falling back.
	localPath string
}

// NewService constructs a Service with global + optional local paths.
// globalDir is ~/.buttons (or $BUTTONS_HOME when set to its equivalent
// per-user dir). localDir is the active project .buttons/ or "" if no
// project is in scope.
func NewService(globalDir, localDir string) *Service {
	s := &Service{
		globalPath: filepath.Join(globalDir, "batteries.json"),
	}
	if localDir != "" {
		s.localPath = filepath.Join(localDir, "batteries.json")
	}
	return s
}

// NewServiceFromEnv resolves both paths from the environment in the
// same way the CLI does, so press (cmd) and the TUI (internal/tui) can
// share one implementation. Rules:
//
//   - $BUTTONS_HOME, if set, is treated as the global dir and no local
//     layering is applied (tests and CI rely on this for determinism).
//   - Otherwise global is ~/.buttons.
//   - Local is the project-local .buttons/ walked up from
//     projectDiscoverer (or nil to skip local layering entirely).
//
// projectDiscoverer is a function you pass in — typically
// config.IsProjectLocal / config.DataDir — so this package stays
// independent of the config package.
func NewServiceFromEnv(projectDiscoverer func() (localDir string, ok bool)) (*Service, error) {
	globalDir := os.Getenv("BUTTONS_HOME")
	if globalDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("could not determine home directory: %w", err)
		}
		globalDir = filepath.Join(home, ".buttons")
	}

	localDir := ""
	if os.Getenv("BUTTONS_HOME") == "" && projectDiscoverer != nil {
		if dir, ok := projectDiscoverer(); ok && dir != globalDir {
			localDir = dir
		}
	}

	return NewService(globalDir, localDir), nil
}

// ErrLocalUnavailable is returned when an operation targets ScopeLocal
// but no project directory is active.
var ErrLocalUnavailable = errors.New("no project-local .buttons/ in scope; run from inside a project or use --global")

// ErrNotFound is returned by Get / Delete when the key is absent.
var ErrNotFound = errors.New("battery not found")

// ValidateKey reports whether key matches the KEY convention. Exposed
// so callers can validate before writing without reaching into the
// store (useful for better error messages in the CLI).
func ValidateKey(key string) error {
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("invalid key %q: must match %s", key, keyPattern.String())
	}
	return nil
}

// ResolveDefaultScope picks ScopeLocal when a project is in scope, else
// ScopeGlobal. Matches the "inside-a-project most people mean local"
// intuition; the CLI's --global / --local flags override it.
func (s *Service) ResolveDefaultScope() Scope {
	if s.localPath != "" {
		return ScopeLocal
	}
	return ScopeGlobal
}

// Set writes (or overwrites) a battery in the given scope.
func (s *Service) Set(key, value string, scope Scope) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	path, err := s.pathFor(scope)
	if err != nil {
		return err
	}
	env, err := readEnvelope(path)
	if err != nil {
		return err
	}
	if env.Batteries == nil {
		env.Batteries = map[string]string{}
	}
	env.Batteries[key] = value
	return writeEnvelope(path, env)
}

// Get returns the raw value for key, preferring ScopeLocal when both
// scopes have it. The returned Scope reports which file the value came
// from — useful for CLI messaging.
func (s *Service) Get(key string) (string, Scope, error) {
	if err := ValidateKey(key); err != nil {
		return "", "", err
	}
	if s.localPath != "" {
		env, err := readEnvelope(s.localPath)
		if err != nil {
			return "", "", err
		}
		if v, ok := env.Batteries[key]; ok {
			return v, ScopeLocal, nil
		}
	}
	env, err := readEnvelope(s.globalPath)
	if err != nil {
		return "", "", err
	}
	if v, ok := env.Batteries[key]; ok {
		return v, ScopeGlobal, nil
	}
	return "", "", ErrNotFound
}

// Delete removes key from the given scope.
func (s *Service) Delete(key string, scope Scope) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	path, err := s.pathFor(scope)
	if err != nil {
		return err
	}
	env, err := readEnvelope(path)
	if err != nil {
		return err
	}
	if _, ok := env.Batteries[key]; !ok {
		return ErrNotFound
	}
	delete(env.Batteries, key)
	return writeEnvelope(path, env)
}

// List returns every battery from every scope in scope order (global
// first, then local), each scope sorted by key. Local entries that
// shadow global entries appear twice — CLI output can flag the shadow;
// the Env method resolves the conflict.
func (s *Service) List() ([]Entry, error) {
	out := []Entry{}

	globalEnv, err := readEnvelope(s.globalPath)
	if err != nil {
		return nil, err
	}
	for _, k := range sortedKeys(globalEnv.Batteries) {
		out = append(out, Entry{Key: k, Value: globalEnv.Batteries[k], Scope: ScopeGlobal})
	}

	if s.localPath != "" {
		localEnv, err := readEnvelope(s.localPath)
		if err != nil {
			return nil, err
		}
		for _, k := range sortedKeys(localEnv.Batteries) {
			out = append(out, Entry{Key: k, Value: localEnv.Batteries[k], Scope: ScopeLocal})
		}
	}

	return out, nil
}

// Env returns the flattened environment map used at press time: global
// batteries merged with local batteries, local wins on collision. Keys
// and values are returned raw — prefixing with BUTTONS_BAT_ is the
// caller's responsibility so tests can inspect unprefixed values.
func (s *Service) Env() (map[string]string, error) {
	merged := map[string]string{}

	globalEnv, err := readEnvelope(s.globalPath)
	if err != nil {
		return nil, err
	}
	for k, v := range globalEnv.Batteries {
		merged[k] = v
	}

	if s.localPath != "" {
		localEnv, err := readEnvelope(s.localPath)
		if err != nil {
			return nil, err
		}
		for k, v := range localEnv.Batteries {
			merged[k] = v
		}
	}

	return merged, nil
}

// Redact returns a display-safe rendering of value. Short values are
// fully masked; longer ones keep the last 4 characters so an operator
// can distinguish two similar secrets without leaking them wholesale.
func Redact(value string) string {
	if len(value) <= 4 {
		return strings.Repeat("•", len(value))
	}
	return strings.Repeat("•", len(value)-4) + value[len(value)-4:]
}

// ------------------------------------------------------------------
// Internal helpers
// ------------------------------------------------------------------

func (s *Service) pathFor(scope Scope) (string, error) {
	switch scope {
	case ScopeGlobal:
		return s.globalPath, nil
	case ScopeLocal:
		if s.localPath == "" {
			return "", ErrLocalUnavailable
		}
		return s.localPath, nil
	default:
		return "", fmt.Errorf("unknown scope %q", scope)
	}
}

// readEnvelope loads an envelope from disk. A missing file is treated
// as an empty envelope so callers don't have to special-case first-run.
// A malformed file surfaces as an error — silent reset would lose data.
func readEnvelope(path string) (envelope, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path fixed to batteries.json under DataDir
	if err != nil {
		if os.IsNotExist(err) {
			return envelope{SchemaVersion: envelopeSchemaVersion, Batteries: map[string]string{}}, nil
		}
		return envelope{}, fmt.Errorf("read %s: %w", path, err)
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return envelope{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if env.Batteries == nil {
		env.Batteries = map[string]string{}
	}
	return env, nil
}

// writeEnvelope serializes env to path with 0o600 perms, creating the
// parent directory if needed (global path under ~/.buttons/ should
// always exist, but the project path can race a project-dir deletion).
func writeEnvelope(path string, env envelope) error {
	env.SchemaVersion = envelopeSchemaVersion
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create parent of %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
