package button

import (
	"encoding/json"
	"fmt"
	"io"
	neturl "net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/config"
)

// extractLockedHost parses the scheme + authority portion of an HTTP
// button URL template and returns a lowercased host (including port
// when present). The template may contain {{arg}} placeholders in its
// path, query, or fragment — those are tolerated — but placeholders
// in the scheme or host are rejected so the URL is bound to a concrete
// destination at create time.
//
// Existing patterns that stay valid:
//   https://api.example.com/users/{{user}}
//   https://api.example.com/things?filter={{filter}}
//
// Patterns rejected (with remediation):
//   https://{{host}}.example.com/x      // host templating
//   {{scheme}}://api.example.com/x      // scheme templating
//   https://api.{{tenant}}/foo          // mid-host templating
//
// Returns the literal lowercased host on success. "*" as the whole
// URL is reserved — not used here.
func extractLockedHost(rawTemplate string) (string, error) {
	// Split off path/query/fragment — everything after the first '/'
	// following the scheme. What's left is scheme://host[:port].
	schemeEnd := strings.Index(rawTemplate, "://")
	if schemeEnd < 0 {
		return "", fmt.Errorf("URL must start with http:// or https://")
	}
	rest := rawTemplate[schemeEnd+3:]
	hostPart := rest
	if slash := strings.IndexAny(rest, "/?#"); slash >= 0 {
		hostPart = rest[:slash]
	}
	scheme := rawTemplate[:schemeEnd]
	if strings.Contains(scheme, "{{") || strings.Contains(scheme, "}}") {
		return "", fmt.Errorf("URL scheme cannot contain {{arg}} placeholders; use a literal http:// or https:// prefix")
	}
	if strings.Contains(hostPart, "{{") || strings.Contains(hostPart, "}}") {
		return "", fmt.Errorf("URL host cannot contain {{arg}} placeholders (got %q); put variable parts in the path/query instead, or create a separate button per host", hostPart)
	}
	// Sanity-check parseability now that we know scheme+host are
	// literal — catches malformed authority sections (e.g. missing
	// host after scheme).
	parsed, err := neturl.Parse(scheme + "://" + hostPart)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("URL is missing a host")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("URL with embedded user:pass is not allowed; put credentials in --header")
	}
	return strings.ToLower(parsed.Host), nil
}

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
	Prompt               string // Prompt/instruction written to AGENT.md
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

	// Validate sources: file, code, or url (--prompt is a modifier, not a source)
	hasFile := opts.FilePath != ""
	hasCode := opts.Code != ""
	hasURL := opts.URL != ""
	hasPrompt := opts.Prompt != ""
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

	if hasCode && len(opts.Code) > maxCodeBytes {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "inline code exceeds 64KB limit"}
	}

	// Resolve runtime. URL → http, prompt-only (no sources) → prompt,
	// otherwise honor --runtime or default to shell. Bare `buttons create foo`
	// scaffolds a shell button with a placeholder main.sh the agent can edit.
	runtime := opts.Runtime
	if hasURL {
		runtime = "http"
	} else if sources == 0 && hasPrompt {
		runtime = "prompt" // standalone prompt button, no code file
	} else if runtime == "" {
		runtime = "shell"
	}
	// --runtime applies to code buttons written by buttons (scaffold or --code).
	// --file uses the imported script's shebang, --url is HTTP, --prompt-only
	// is text — all three ignore the runtime concept.
	if opts.Runtime != "" {
		if hasURL {
			return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "--runtime is not valid with --url"}
		}
		if hasFile {
			return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "--runtime is not valid with --file (file-based buttons use their own shebang)"}
		}
		if sources == 0 && hasPrompt {
			return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "--runtime is not valid with a prompt-only button"}
		}
	}

	// Validate URL
	method := strings.ToUpper(opts.Method)
	var allowedHost string
	if hasURL {
		if !strings.HasPrefix(opts.URL, "http://") && !strings.HasPrefix(opts.URL, "https://") {
			return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "URL must start with http:// or https://"}
		}
		// Lock scheme + host at create time so {{arg}} substitution
		// later can only touch path/query/fragment. Parsing out the
		// host without resolving template variables means literal
		// URLs auto-derive their AllowedHost here.
		host, hostErr := extractLockedHost(opts.URL)
		if hostErr != nil {
			return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: hostErr.Error()}
		}
		allowedHost = host
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
		SchemaVersion:        2,
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
		AllowedHost:          allowedHost,
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

	// Write code file for every code-based button. HTTP buttons (--url)
	// and standalone prompt buttons (--prompt with no source) do not get
	// a main.* file. Source priority: --code > --file > scaffold placeholder.
	writeCodeFile := !hasURL && !(sources == 0 && hasPrompt)
	if writeCodeFile {
		ext := ExtForRuntime(runtime)
		codePath := filepath.Join(btnDir, "main"+ext)

		switch {
		case hasCode:
			// #nosec G306 -- code files require the exec bit to run via /bin/sh, python3, node;
			// 0700 keeps them user-private while allowing execution.
			if err := os.WriteFile(codePath, []byte(opts.Code), 0700); err != nil {
				return nil, fmt.Errorf("failed to write code file: %w", err)
			}
		case hasFile:
			if err := copyFile(opts.FilePath, codePath); err != nil {
				return nil, fmt.Errorf("failed to copy file: %w", err)
			}
			// #nosec G302 -- code files require the exec bit; see rationale above.
			if err := os.Chmod(codePath, 0700); err != nil {
				return nil, fmt.Errorf("failed to set code file permissions: %w", err)
			}
		default:
			// Scaffold: write a placeholder the agent can edit. Shebang + TODO
			// so opening the file reveals the runtime and what to do next.
			// #nosec G306 -- see rationale above.
			if err := os.WriteFile(codePath, []byte(scaffoldFor(runtime)), 0700); err != nil {
				return nil, fmt.Errorf("failed to write scaffold: %w", err)
			}
		}
	}

	// Write AGENT.md
	var promptMD string
	if hasPrompt {
		// Prompt button: the instruction IS the AGENT.md
		promptMD = opts.Prompt + "\n"
	} else {
		promptMD = fmt.Sprintf("# %s\n\n", name)
		if opts.Description != "" {
			promptMD += opts.Description + "\n\n"
		}
		promptMD += "## Notes\n\n_Add context about this button here: why it exists, gotchas, expected output format._\n"
	}
	if err := os.WriteFile(filepath.Join(btnDir, "AGENT.md"), []byte(promptMD), 0600); err != nil {
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
