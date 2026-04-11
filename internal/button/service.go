package button

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/config"
)

const maxCodeBytes = 65536 // 64KB

type CreateOpts struct {
	Name                 string
	FilePath             string
	Code                 string
	Runtime              string
	URL                  string
	Method               string
	Headers              map[string]string
	Body                 string
	Agent                string // Agent instruction/system prompt
	Description          string
	TimeoutSeconds       int
	MaxResponseBytes     int64 // URL buttons only; zero → DefaultMaxResponseBytes
	AllowPrivateNetworks bool  // URL buttons only; opt in to private network targets
	Args                 []ArgDef
}

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Create(opts CreateOpts) (*Button, error) {
	if err := ValidateName(opts.Name); err != nil {
		return nil, err
	}
	name := Slugify(opts.Name)
	if name == "" {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "button name is empty after slugification"}
	}

	// Validate sources: file, code, or url (--agent is a modifier, not a source)
	hasFile := opts.FilePath != ""
	hasCode := opts.Code != ""
	hasURL := opts.URL != ""
	hasAgent := opts.Agent != ""
	sources := 0
	if hasFile {
		sources++
	}
	if hasCode {
		sources++
	}
	if hasURL {
		sources++
	}
	if sources > 1 {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "only one of --file, --code, or --url can be provided"}
	}
	if sources == 0 && !hasAgent {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "must provide --file, --code, --url, or --agent"}
	}

	if hasCode && len(opts.Code) > maxCodeBytes {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "inline code exceeds 64KB limit"}
	}

	// Resolve runtime
	runtime := opts.Runtime
	if hasURL {
		runtime = "http"
	} else if sources == 0 && hasAgent {
		runtime = "agent" // standalone agent, no code/url/file
	} else if runtime == "" {
		runtime = "shell"
	}
	if opts.Runtime != "" && !hasCode {
		if hasFile {
			return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "--runtime is only valid with --code (file-based buttons use shebangs)"}
		}
		if hasURL {
			return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "--runtime is not valid with --url"}
		}
		if sources > 0 {
			return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "--runtime requires --code"}
		}
	}

	// Validate URL
	method := strings.ToUpper(opts.Method)
	if hasURL {
		if !strings.HasPrefix(opts.URL, "http://") && !strings.HasPrefix(opts.URL, "https://") {
			return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "URL must start with http:// or https://"}
		}
		if method == "" {
			method = "GET"
		}
	}

	// Validate --max-response-size is only meaningful for URL buttons.
	if opts.MaxResponseBytes != 0 && !hasURL {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "--max-response-size is only valid with --url"}
	}
	if opts.MaxResponseBytes < 0 {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "--max-response-size must be positive"}
	}
	if opts.MaxResponseBytes > MaxAllowedResponseBytes {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("--max-response-size exceeds maximum allowed (%s)", FormatSize(MaxAllowedResponseBytes))}
	}
	maxResponseBytes := opts.MaxResponseBytes
	if hasURL && maxResponseBytes == 0 {
		maxResponseBytes = DefaultMaxResponseBytes
	}

	// Validate --allow-private-networks is only meaningful for URL buttons.
	if opts.AllowPrivateNetworks && !hasURL {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "--allow-private-networks is only valid with --url"}
	}

	// Validate source file exists
	if hasFile {
		if _, err := os.Stat(opts.FilePath); os.IsNotExist(err) {
			return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("file does not exist: %s", opts.FilePath)}
		}
	}

	// Check button doesn't already exist
	btnDir, err := s.buttonDir(name)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(btnDir); err == nil {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("button already exists: %s", name)}
	}

	timeout := opts.TimeoutSeconds
	if timeout <= 0 {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "timeout must be greater than 0"}
	}

	// Create button directory structure
	if err := os.MkdirAll(filepath.Join(btnDir, "pressed"), 0700); err != nil {
		return nil, fmt.Errorf("failed to create button directory: %w", err)
	}

	now := time.Now().UTC()
	btn := &Button{
		SchemaVersion:        1,
		Name:                 name,
		Description:          opts.Description,
		Runtime:              runtime,
		URL:                  opts.URL,
		Method:               method,
		Headers:              opts.Headers,
		Body:                 opts.Body,
		Args:                 opts.Args,
		Env:                  map[string]string{},
		TimeoutSeconds:       timeout,
		MaxResponseBytes:     maxResponseBytes,
		AllowPrivateNetworks: opts.AllowPrivateNetworks,
		MCPEnabled:           false,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	// Write button.json
	specData, err := json.MarshalIndent(btn, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal button: %w", err)
	}
	if err := os.WriteFile(filepath.Join(btnDir, "button.json"), specData, 0600); err != nil {
		return nil, fmt.Errorf("failed to write button spec: %w", err)
	}

	// Write code file (for code and file-based buttons, not URL)
	if hasCode || hasFile {
		ext := ExtForRuntime(runtime)
		codePath := filepath.Join(btnDir, "main"+ext)

		if hasCode {
			// #nosec G306 -- code files require the exec bit to run via /bin/sh, python3, node;
			// 0700 keeps them user-private while allowing execution.
			if err := os.WriteFile(codePath, []byte(opts.Code), 0700); err != nil {
				return nil, fmt.Errorf("failed to write code file: %w", err)
			}
		} else {
			if err := copyFile(opts.FilePath, codePath); err != nil {
				return nil, fmt.Errorf("failed to copy file: %w", err)
			}
			// #nosec G302 -- code files require the exec bit; see rationale above.
			if err := os.Chmod(codePath, 0700); err != nil {
				return nil, fmt.Errorf("failed to set code file permissions: %w", err)
			}
		}
	}

	// Write AGENT.md
	var agentMD string
	if hasAgent {
		// Agent button: the instruction IS the AGENT.md
		agentMD = opts.Agent + "\n"
	} else {
		agentMD = fmt.Sprintf("# %s\n\n", name)
		if opts.Description != "" {
			agentMD += opts.Description + "\n\n"
		}
		agentMD += "## Notes\n\n_Add context about this button here: why it exists, gotchas, expected output format._\n"
	}
	if err := os.WriteFile(filepath.Join(btnDir, "AGENT.md"), []byte(agentMD), 0600); err != nil {
		// Best-effort cleanup of the partially created button directory.
		// Error is intentionally ignored: we are already returning a failure
		// and there is no useful recovery path if the cleanup itself fails.
		_ = os.RemoveAll(btnDir)
		return nil, fmt.Errorf("failed to write AGENT.md: %w", err)
	}

	return btn, nil
}

func (s *Service) List() ([]Button, error) {
	dir, err := config.ButtonsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Button{}, nil
		}
		return nil, fmt.Errorf("failed to read buttons directory: %w", err)
	}

	buttons := make([]Button, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		specPath := filepath.Join(dir, entry.Name(), "button.json")
		// #nosec G304 -- specPath is rooted in ButtonsDir + a DirEntry name
		// we just enumerated from os.ReadDir; no user input reaches this path.
		data, err := os.ReadFile(specPath)
		if err != nil {
			continue
		}
		var btn Button
		if err := json.Unmarshal(data, &btn); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not parse %s: %v\n", specPath, err)
			continue
		}
		buttons = append(buttons, btn)
	}

	sort.Slice(buttons, func(i, j int) bool {
		return buttons[i].Name < buttons[j].Name
	})

	return buttons, nil
}

func (s *Service) Get(name string) (*Button, error) {
	name = Slugify(name)
	btnDir, err := s.buttonDir(name)
	if err != nil {
		return nil, err
	}

	specPath := filepath.Join(btnDir, "button.json")
	// #nosec G304 -- btnDir is produced by buttonDir() which rejects any
	// path resolving outside ButtonsDir; name is always slugified first.
	data, err := os.ReadFile(specPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ServiceError{Code: "NOT_FOUND", Message: fmt.Sprintf("button not found: %s", name)}
		}
		return nil, fmt.Errorf("failed to read button spec: %w", err)
	}

	var btn Button
	if err := json.Unmarshal(data, &btn); err != nil {
		return nil, fmt.Errorf("failed to parse button spec: %w", err)
	}

	return &btn, nil
}

// CodePath returns the path to the button's code file.
func (s *Service) CodePath(name string) (string, error) {
	name = Slugify(name)
	btn, err := s.Get(name)
	if err != nil {
		return "", err
	}
	btnDir, err := s.buttonDir(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(btnDir, "main"+ExtForRuntime(btn.Runtime)), nil
}

// PressedDir returns the path to the button's pressed/ directory.
func (s *Service) PressedDir(name string) (string, error) {
	name = Slugify(name)
	btnDir, err := s.buttonDir(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(btnDir, "pressed"), nil
}

func (s *Service) Remove(name string) error {
	name = Slugify(name)
	btnDir, err := s.buttonDir(name)
	if err != nil {
		return err
	}

	if _, err := os.Stat(btnDir); os.IsNotExist(err) {
		return &ServiceError{Code: "NOT_FOUND", Message: fmt.Sprintf("button not found: %s", name)}
	}

	if err := os.RemoveAll(btnDir); err != nil {
		return fmt.Errorf("failed to remove button: %w", err)
	}

	return nil
}

func (s *Service) buttonDir(name string) (string, error) {
	dir, err := config.ButtonsDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	if !strings.HasPrefix(p, dir+string(filepath.Separator)) {
		return "", &ServiceError{Code: "VALIDATION_ERROR", Message: "button name resolves outside data directory"}
	}
	return p, nil
}

func copyFile(src, dst string) error {
	// #nosec G304 -- src is the path the user passed to --file at create time;
	// they are explicitly asking us to read it into their own button folder.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// #nosec G304 -- dst is inside btnDir which buttonDir() constrains to ButtonsDir.
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
