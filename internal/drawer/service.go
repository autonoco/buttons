package drawer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
)

// ServiceError mirrors button.ServiceError. Keeping a separate type
// here (rather than aliasing) so drawers can grow their own error
// codes without colliding with button validation errors.
type ServiceError struct {
	Code    string
	Message string
}

func (e *ServiceError) Error() string { return e.Message }

// Service is the CRUD layer for drawers. Mirrors button.Service
// shape: stateless, filesystem-backed, safe to instantiate freely.
type Service struct{}

func NewService() *Service { return &Service{} }

// Create writes an empty drawer to disk. Inputs can be supplied now
// (for pre-declared drawer inputs) or added later via UpdateInputs.
// Names follow the same validation rules as buttons (kebab-case, not
// reserved) so an agent can't create `buttons drawer list` and
// collide with the subcommand.
func (s *Service) Create(name string, description string, inputs []InputDef) (*Drawer, error) {
	if err := button.ValidateName(name); err != nil {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: err.Error()}
	}
	slug := button.Slugify(name)
	if slug == "" {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "drawer name is empty after slugification"}
	}

	dir, err := config.DrawerDir(slug)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dir); err == nil {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("drawer already exists: %s", slug)}
	}

	if err := os.MkdirAll(filepath.Join(dir, "pressed"), 0700); err != nil {
		return nil, fmt.Errorf("failed to create drawer directory: %w", err)
	}

	now := time.Now().UTC()
	d := &Drawer{
		SchemaVersion: SchemaVersion,
		Name:          slug,
		Description:   description,
		Inputs:        inputs,
		Steps:         []Step{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.save(d); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return d, nil
}

// Get returns the drawer by name. NOT_FOUND on missing.
func (s *Service) Get(name string) (*Drawer, error) {
	slug := button.Slugify(name)
	dir, err := config.DrawerDir(slug)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "drawer.json")
	// #nosec G304 -- dir is produced by config.DrawerDir which rejects
	// any path resolving outside DrawersDir; name is always slugified.
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ServiceError{Code: "NOT_FOUND", Message: fmt.Sprintf("drawer not found: %s", slug)}
		}
		return nil, fmt.Errorf("failed to read drawer spec: %w", err)
	}
	var d Drawer
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("failed to parse drawer spec: %w", err)
	}
	return &d, nil
}

// List returns every drawer on disk, sorted by name.
func (s *Service) List() ([]Drawer, error) {
	dir, err := config.DrawersDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Drawer{}, nil
		}
		return nil, fmt.Errorf("failed to read drawers directory: %w", err)
	}
	drawers := make([]Drawer, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "drawer.json")
		// #nosec G304 -- path is rooted in DrawersDir + a DirEntry name
		// we just enumerated from os.ReadDir.
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var d Drawer
		if err := json.Unmarshal(data, &d); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not parse %s: %v\n", path, err)
			continue
		}
		drawers = append(drawers, d)
	}
	sort.Slice(drawers, func(i, j int) bool { return drawers[i].Name < drawers[j].Name })
	return drawers, nil
}

// Remove deletes the drawer directory entirely (including pressed
// history). NOT_FOUND on missing.
func (s *Service) Remove(name string) error {
	slug := button.Slugify(name)
	dir, err := config.DrawerDir(slug)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return &ServiceError{Code: "NOT_FOUND", Message: fmt.Sprintf("drawer not found: %s", slug)}
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to remove drawer: %w", err)
	}
	return nil
}

// AddSteps appends one or more button steps to the drawer. Each
// button name is validated against the button service (error if the
// button doesn't exist). Step IDs default to the button name; if
// that's already taken we append "-2", "-3", etc. so agents can add
// the same button twice without fighting with ids.
func (s *Service) AddSteps(drawerName string, targets []string) (*Drawer, error) {
	d, err := s.Get(drawerName)
	if err != nil {
		return nil, err
	}

	btnSvc := button.NewService()
	taken := map[string]bool{}
	for _, st := range d.Steps {
		taken[st.ID] = true
	}

	for _, t := range targets {
		// `wait:DURATION` → kind=wait step with the given duration.
		// Most common shape; for `until`-form, use `add wait` then
		// `set wait.until=<RFC3339>`.
		if strings.HasPrefix(t, "wait:") {
			dur := strings.TrimPrefix(t, "wait:")
			if dur == "" {
				return nil, &ServiceError{
					Code:    "VALIDATION_ERROR",
					Message: "wait:DURATION requires a duration (e.g. wait:30s)",
				}
			}
			id := "wait"
			for n := 2; taken[id]; n++ {
				id = fmt.Sprintf("wait-%d", n)
			}
			taken[id] = true
			d.Steps = append(d.Steps, Step{
				ID:       id,
				Kind:     "wait",
				Duration: dur,
			})
			continue
		}
		// `for_each:BUTTON` → kind=for_each step wrapping one nested
		// button step. Minimal shorthand so agents can author simple
		// per-item loops without patching drawer.json directly.
		// Multi-step bodies still need file-level editing.
		if strings.HasPrefix(t, "for_each:") {
			inner := strings.TrimPrefix(t, "for_each:")
			slug := button.Slugify(inner)
			if _, err := btnSvc.Get(slug); err != nil {
				return nil, &ServiceError{
					Code:    "BUTTON_NOT_FOUND",
					Message: fmt.Sprintf("button %q does not exist (used inside for_each)", inner),
				}
			}
			id := "for_each-" + slug
			for n := 2; taken[id]; n++ {
				id = fmt.Sprintf("for_each-%s-%d", slug, n)
			}
			taken[id] = true
			d.Steps = append(d.Steps, Step{
				ID:   id,
				Kind: "for_each",
				As:   "item",
				Steps: []Step{
					{
						ID:     slug,
						Kind:   "button",
						Button: slug,
						Args:   map[string]any{},
					},
				},
			})
			continue
		}
		// `drawer/NAME` → kind=drawer sub-drawer step. Plain name →
		// kind=button step (existing path). No other prefixes.
		if strings.HasPrefix(t, "drawer/") {
			childName := button.Slugify(strings.TrimPrefix(t, "drawer/"))
			if childName == d.Name {
				return nil, &ServiceError{
					Code:    "VALIDATION_ERROR",
					Message: fmt.Sprintf("drawer %q cannot include itself as a sub-drawer", d.Name),
				}
			}
			if _, err := s.Get(childName); err != nil {
				return nil, &ServiceError{
					Code:    "DRAWER_NOT_FOUND",
					Message: fmt.Sprintf("drawer %q does not exist", childName),
				}
			}
			id := childName
			for n := 2; taken[id]; n++ {
				id = fmt.Sprintf("%s-%d", childName, n)
			}
			taken[id] = true
			d.Steps = append(d.Steps, Step{
				ID:     id,
				Kind:   "drawer",
				Drawer: childName,
				Args:   map[string]any{},
			})
			continue
		}
		slug := button.Slugify(t)
		if _, err := btnSvc.Get(slug); err != nil {
			return nil, &ServiceError{
				Code:    "BUTTON_NOT_FOUND",
				Message: fmt.Sprintf("button %q does not exist", t),
			}
		}
		id := slug
		for n := 2; taken[id]; n++ {
			id = fmt.Sprintf("%s-%d", slug, n)
		}
		taken[id] = true
		d.Steps = append(d.Steps, Step{
			ID:     id,
			Kind:   "button",
			Button: slug,
			Args:   map[string]any{},
		})
	}

	d.UpdatedAt = time.Now().UTC()
	if err := s.save(d); err != nil {
		return nil, err
	}
	return d, nil
}

// SetArg sets a single arg reference or literal on a specific step.
// Used by the `connect` CLI command after ref resolution. Overwrites
// any existing value for the same key.
func (s *Service) SetArg(drawerName, stepID, argName string, value any) (*Drawer, error) {
	d, err := s.Get(drawerName)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, st := range d.Steps {
		if st.ID == stepID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, &ServiceError{Code: "STEP_NOT_FOUND", Message: fmt.Sprintf("step %q not found in drawer %q", stepID, drawerName)}
	}
	if d.Steps[idx].Args == nil {
		d.Steps[idx].Args = map[string]any{}
	}
	d.Steps[idx].Args[argName] = value
	d.UpdatedAt = time.Now().UTC()
	if err := s.save(d); err != nil {
		return nil, err
	}
	return d, nil
}

// SetField sets a non-args field directly on a step — e.g. for_each's
// `over`/`as`/`on_item_failure`, switch's default branch, or
// aggregate's `from`/`pluck`. Values are always strings at the wire
// level for these fields. Unknown field names are rejected so typos
// surface as STEP_FIELD_UNKNOWN instead of being silently ignored.
func (s *Service) SetField(drawerName, stepID, field, value string) (*Drawer, error) {
	d, err := s.Get(drawerName)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, st := range d.Steps {
		if st.ID == stepID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, &ServiceError{Code: "STEP_NOT_FOUND", Message: fmt.Sprintf("step %q not found in drawer %q", stepID, drawerName)}
	}
	switch field {
	case "over":
		d.Steps[idx].Over = value
	case "as":
		d.Steps[idx].As = value
	case "on_item_failure":
		if value != "stop" && value != "continue" {
			return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "on_item_failure must be 'stop' or 'continue'"}
		}
		d.Steps[idx].OnItemFailure = value
	case "from":
		d.Steps[idx].From = value
	case "pluck":
		d.Steps[idx].Pluck = value
	case "button":
		d.Steps[idx].Button = value
	case "drawer":
		d.Steps[idx].Drawer = value
	case "duration":
		d.Steps[idx].Duration = value
	case "until":
		d.Steps[idx].Until = value
	default:
		return nil, &ServiceError{
			Code:    "STEP_FIELD_UNKNOWN",
			Message: fmt.Sprintf("unknown step field %q — allowed: over, as, on_item_failure, from, pluck, button, drawer, duration, until", field),
		}
	}
	d.UpdatedAt = time.Now().UTC()
	if err := s.save(d); err != nil {
		return nil, err
	}
	return d, nil
}

// SetNestedArg sets an arg on a nested step inside a for_each or
// switch body. Path form: <outer_id>.steps.<idx>.args.<field>=value.
// Nested-step addressing is intentionally indexed (not by id) because
// nested step ids may not be unique across branches and indices keep
// the target unambiguous.
func (s *Service) SetNestedArg(drawerName, outerID string, nestedIdx int, argName string, value any) (*Drawer, error) {
	d, err := s.Get(drawerName)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, st := range d.Steps {
		if st.ID == outerID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, &ServiceError{Code: "STEP_NOT_FOUND", Message: fmt.Sprintf("step %q not found in drawer %q", outerID, drawerName)}
	}
	if nestedIdx < 0 || nestedIdx >= len(d.Steps[idx].Steps) {
		return nil, &ServiceError{Code: "NESTED_STEP_NOT_FOUND", Message: fmt.Sprintf("step %q has no nested step at index %d (has %d)", outerID, nestedIdx, len(d.Steps[idx].Steps))}
	}
	if d.Steps[idx].Steps[nestedIdx].Args == nil {
		d.Steps[idx].Steps[nestedIdx].Args = map[string]any{}
	}
	d.Steps[idx].Steps[nestedIdx].Args[argName] = value
	d.UpdatedAt = time.Now().UTC()
	if err := s.save(d); err != nil {
		return nil, err
	}
	return d, nil
}

// save writes drawer.json to disk. Called by every mutation.
func (s *Service) save(d *Drawer) error {
	dir, err := config.DrawerDir(d.Name)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal drawer: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "drawer.json"), data, 0600); err != nil {
		return fmt.Errorf("failed to write drawer spec: %w", err)
	}
	return nil
}

// PressedDir returns the path to a drawer's pressed/ directory —
// parallels button.Service.PressedDir so the history package can
// write trace files there.
func (s *Service) PressedDir(name string) (string, error) {
	slug := button.Slugify(name)
	dir, err := config.DrawerDir(slug)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pressed"), nil
}
