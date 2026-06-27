// Package importer turns external sources into buttons (#277): a script file
// (code), an AgentSkills skill directory (skill), or a button spec at a URL.
// Each adapter produces a Plan the caller can show + confirm before Apply
// actually creates the buttons via button.Service.
package importer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/autonoco/buttons/internal/button"
)

const (
	maxImportBytes = 64 * 1024 // matches the create inline-code limit
	defaultTimeout = 300       // seconds; mirrors `buttons create`'s default
)

// Planned is one button an import would create.
type Planned struct {
	Name        string `json:"name"`
	Runtime     string `json:"runtime"`
	Source      string `json:"source"`
	Description string `json:"description,omitempty"`
	opts        button.CreateOpts
}

// Plan is the full set an import would create, with its kind for display.
type Plan struct {
	Kind  string    `json:"kind"`
	Items []Planned `json:"items"`
}

// Result reports what Apply created and any per-item failures.
type Result struct {
	Created []string          `json:"created"`
	Errors  map[string]string `json:"errors,omitempty"`
}

// Apply creates every planned button. A failure on one item is recorded and
// the rest proceed, so a single bad script doesn't abort a skill import.
func Apply(svc *button.Service, plan *Plan) *Result {
	res := &Result{Errors: map[string]string{}}
	for _, it := range plan.Items {
		if _, err := svc.Create(it.opts); err != nil {
			res.Errors[it.Name] = err.Error()
			continue
		}
		res.Created = append(res.Created, it.Name)
	}
	if len(res.Errors) == 0 {
		res.Errors = nil
	}
	return res
}

// PlanCode wraps a single script file as a button, inferring runtime from the
// extension then the shebang. nameOverride wins when set.
func PlanCode(file, nameOverride string) (*Plan, error) {
	data, err := readFileLimited(file)
	if err != nil {
		return nil, err
	}
	name := nameOverride
	if name == "" {
		name = baseName(file)
	}
	rt := inferRuntime(file, string(data))
	return &Plan{Kind: "code", Items: []Planned{{
		Name:    name,
		Runtime: rt,
		Source:  file,
		opts:    button.CreateOpts{Name: name, Code: string(data), Runtime: rt, TimeoutSeconds: defaultTimeout},
	}}}, nil
}

// PlanSkill reads an AgentSkills skill directory. When a scripts/ dir exists,
// each script becomes a button (named <skill>-<script>); otherwise the skill's
// SKILL.md becomes a single prompt button.
func PlanSkill(dir string) (*Plan, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("not a skill directory: %s", dir)
	}
	skill := baseName(dir)
	desc := skillDescription(dir)

	scriptsDir := filepath.Join(dir, "scripts")
	plan := &Plan{Kind: "skill"}
	if entries, err := os.ReadDir(scriptsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join(scriptsDir, e.Name())
			data, err := readFileLimited(path)
			if err != nil {
				continue // skip oversized/unreadable scripts; Apply reports nothing for them
			}
			name := skill + "-" + baseName(path)
			rt := inferRuntime(path, string(data))
			plan.Items = append(plan.Items, Planned{
				Name:        name,
				Runtime:     rt,
				Source:      path,
				Description: desc,
				opts:        button.CreateOpts{Name: name, Code: string(data), Runtime: rt, Description: desc, TimeoutSeconds: defaultTimeout},
			})
		}
	}
	if len(plan.Items) == 0 {
		// No scripts/ — treat SKILL.md as a prompt button.
		md, err := readSkillMD(dir)
		if err != nil {
			return nil, fmt.Errorf("skill %q has no scripts/ and no SKILL.md", skill)
		}
		plan.Items = append(plan.Items, Planned{
			Name:        skill,
			Runtime:     "prompt",
			Source:      filepath.Join(dir, "SKILL.md"),
			Description: desc,
			opts:        button.CreateOpts{Name: skill, Prompt: md, Description: desc},
		})
	}
	return plan, nil
}

// PlanURL fetches a button spec from a URL. Supports self-contained HTTP/API
// button specs (the kind `create --url` produces); code/prompt buttons need
// their bundle, so use `buttons install` (the registry) for those.
func PlanURL(url, nameOverride string) (*Plan, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("url must start with http:// or https://")
	}
	resp, err := http.Get(url) // #nosec G107 -- user-supplied import URL, fetched read-only and size-capped
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImportBytes))
	if err != nil {
		return nil, err
	}
	var spec button.Button
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("not a button spec (invalid JSON): %w", err)
	}
	name := nameOverride
	if name == "" {
		name = spec.Name
	}
	if name == "" {
		return nil, fmt.Errorf("spec has no name; pass --name")
	}
	if spec.URL == "" {
		return nil, fmt.Errorf("url import supports HTTP/API button specs (spec.url set); for code/prompt buttons use `buttons install`")
	}
	return &Plan{Kind: "url", Items: []Planned{{
		Name:        name,
		Runtime:     "http",
		Source:      url,
		Description: spec.Description,
		opts: button.CreateOpts{
			Name: name, URL: spec.URL, Method: spec.Method, Headers: spec.Headers,
			Body: spec.Body, Description: spec.Description, Args: spec.Args, TimeoutSeconds: defaultTimeout,
		},
	}}}, nil
}

// inferRuntime maps a script to a supported runtime (shell|python|node) by
// extension first, then shebang, defaulting to shell.
func inferRuntime(path, content string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "node"
	case ".sh", ".bash":
		return "shell"
	}
	first := content
	if i := strings.IndexByte(content, '\n'); i >= 0 {
		first = content[:i]
	}
	first = strings.ToLower(first)
	switch {
	case strings.Contains(first, "python"):
		return "python"
	case strings.Contains(first, "node"):
		return "node"
	default:
		return "shell"
	}
}

func readFileLimited(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	if info.Size() > maxImportBytes {
		return nil, fmt.Errorf("%s exceeds the %dKB import limit", path, maxImportBytes/1024)
	}
	// #nosec G304 -- path is a user-specified import target, read-only.
	return os.ReadFile(path)
}

// baseName returns the filename without directory or extension.
func baseName(path string) string {
	b := filepath.Base(path)
	return strings.TrimSuffix(b, filepath.Ext(b))
}

// skillDescription returns a one-line description from SKILL.md's first heading
// or first non-empty line, falling back to the skill directory name.
func skillDescription(dir string) string {
	md, err := readSkillMD(dir)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return strings.TrimSpace(strings.TrimLeft(line, "# "))
	}
	return ""
}

func readSkillMD(dir string) (string, error) {
	for _, name := range []string{"SKILL.md", "skill.md", "Skill.md"} {
		// #nosec G304 -- dir is a user-specified skill path; reading a known filename within it.
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return string(data), nil
		}
	}
	return "", fmt.Errorf("no SKILL.md in %s", dir)
}
