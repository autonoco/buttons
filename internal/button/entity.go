package button

import (
	"encoding/json"
	"time"
)

type Button struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	// Kind distinguishes a regular button ("" or "button") from an app ("app").
	// App buttons live under apps/ and are served (`buttons serve`), not pressed.
	Kind string `json:"kind,omitempty"`
	// Serve declares how an app-kind button builds and runs when exposed at a URL.
	// Omitted for regular buttons.
	Serve *ServeSpec `json:"serve,omitempty"`
	// Tags group/categorize a button for discovery and install-source filtering
	// (e.g. "finance", "sync"). Omitted when empty so existing buttons
	// don't grow a noisy field.
	Tags []string `json:"tags,omitempty"`
	// Version is the button's content release (semver), distinct from
	// SchemaVersion (the on-disk format counter). Published and installed
	// registry buttons must carry an exact immutable version.
	// Empty for hand-authored local buttons (treated as "unversioned").
	Version              string            `json:"version,omitempty"`
	Runtime              string            `json:"runtime"`
	URL                  string            `json:"url,omitempty"`
	Method               string            `json:"method,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
	Body                 string            `json:"body,omitempty"`
	Args                 []ArgDef          `json:"args,omitempty"`
	Env                  map[string]string `json:"env"`
	TimeoutSeconds       int               `json:"timeout_seconds"`
	MaxResponseBytes     int64             `json:"max_response_bytes,omitempty"`
	AllowPrivateNetworks bool              `json:"allow_private_networks,omitempty"`
	// AllowedHost locks the scheme+host portion of an HTTP button's
	// URL at create time. {{arg}} substitution never modifies scheme
	// or host — only path, query, and fragment. Before dispatch,
	// validateHTTPTarget enforces that the final URL's host matches
	// this value.
	//
	// Auto-derived from URL when the scheme and host have no {{}}
	// placeholders. A literal "*" opts out (requires
	// BUTTONS_ALLOW_ANY_HOST=1 at press time to actually fire) for
	// legacy buttons that genuinely need host templating.
	//
	// Defends against {{arg}} values from remote sources (webhook
	// bodies) being used to redirect the button to an attacker-
	// controlled host — a real exfil vector the private-network
	// SSRF block didn't cover.
	AllowedHost string `json:"allowed_host,omitempty"`
	MCPEnabled  bool   `json:"mcp_enabled"`
	// Pinned buttons render as large, clickable cards at the top of the
	// `buttons board` TUI. Omitted from JSON when false to avoid polluting
	// every existing button.json — most buttons aren't pinned.
	Pinned bool `json:"pinned,omitempty"`
	// OutputSchema describes the shape of the button's stdout-as-JSON
	// output. JSON Schema Draft 2020-12. Optional — when present,
	// drawers can statically type-check references like
	// ${step_id.output.field} against this schema at connect time.
	// Stored verbatim as raw JSON so we don't force a specific schema
	// library dependency on every button consumer.
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	// Queue optionally caps concurrent presses. Buttons sharing the
	// same Queue.Name share a slot pool. Queue.Key (e.g. "${inputs.user_id}")
	// scopes the pool by dimension so "openai, 3 concurrent per user"
	// is expressible without per-user buttons. Omitted → no limit.
	Queue *QueueConfig `json:"queue,omitempty"`
	// Requires names package dependencies this button needs installed
	// alongside it. Keys are scoped registry names ("@desk/name"); values
	// are "latest" or an exact version.
	Requires map[string]string `json:"requires,omitempty"`
	// RequiresBatteries names the secret keys this button needs (e.g.
	// "PIPEDREAM_CLIENT_ID"). An install source ships requirement NAMES, never
	// values; `store install` prompts for them via `buttons batteries`.
	RequiresBatteries []string `json:"requires_batteries,omitempty"`
	// Triggers fire this button on an event — a cron schedule, a watched
	// file change, or a webhook POST (served by `buttons serve`). Managed
	// via `buttons trigger`. Omitted when empty.
	Triggers  []Trigger `json:"triggers,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Trigger declares an event that presses this button automatically. The engine
// runs under `buttons serve`: cron schedules tick in-process, watch triggers
// poll a file, and webhook triggers mount an HTTP route on the serve listener.
type Trigger struct {
	ID   string `json:"id"`   // short stable id (for `trigger rm`)
	Kind string `json:"kind"` // "cron" | "watch" | "webhook"
	// Schedule is a 5-field cron expression (kind=cron), e.g. "0 */6 * * *".
	Schedule string `json:"schedule,omitempty"`
	// Path is the watched file (kind=watch) or the webhook URL path
	// (kind=webhook), e.g. "/hooks/health".
	Path string `json:"path,omitempty"`
	// Token, when set on a webhook trigger, must match the X-Buttons-Token
	// header (or ?token= query) or the request is rejected 401.
	Token string `json:"token,omitempty"`
	// Args are passed to the press when the trigger fires.
	Args      map[string]string `json:"args,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// QueueConfig declares per-button concurrency limits. Enforced at
// press time via internal/queue's file-lock semaphore.
type QueueConfig struct {
	Name        string `json:"name"`
	Concurrency int    `json:"concurrency,omitempty"`
	// Key is a CEL-style reference resolved per-press. Value is
	// appended to the queue name so distinct keys get distinct
	// slot pools.
	Key string `json:"key,omitempty"`
}

// ServeSpec is how an app-kind button is built and run when `buttons serve`
// exposes it at a URL behind the tunnel. Static = build then serve the output
// dir; dynamic = run a long-lived server on Port, proxied via the listener.
type ServeSpec struct {
	Type   string `json:"type"`             // "static" | "dynamic"
	Build  string `json:"build,omitempty"`  // e.g. "pnpm install && pnpm build"
	Run    string `json:"run,omitempty"`    // dynamic: long-running server on $PORT
	Output string `json:"output,omitempty"` // static: built dir to serve
	Port   int    `json:"port,omitempty"`   // dynamic: local port
}

type ArgDef struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`

	// Values is the allowed value set for Type == "enum". Empty for
	// other types. Stored in JSON as `values` (omitempty so existing
	// non-enum buttons don't grow a noisy empty-array field).
	Values []string `json:"values,omitempty"`
}

// ExtForRuntime returns the code file extension for a given runtime.
func ExtForRuntime(runtime string) string {
	switch runtime {
	case "python", "python3":
		return ".py"
	case "node", "javascript", "js":
		return ".js"
	default:
		return ".sh"
	}
}

// scaffoldFor returns the placeholder body written to main.<ext> when a
// button is created without --code or --file. The shebang makes the runtime
// obvious when the agent opens the file; the TODO comment signals where to
// put the real implementation.
func scaffoldFor(runtime string) string {
	switch runtime {
	case "python", "python3":
		return "#!/usr/bin/env python3\n# TODO: add your code here\n# Args arrive as os.environ[\"BUTTONS_ARG_<NAME>\"]\n"
	case "node", "javascript", "js":
		return "#!/usr/bin/env node\n// TODO: add your code here\n// Args arrive as process.env.BUTTONS_ARG_<NAME>\n"
	default:
		return "#!/bin/sh\nset -eu\n\n# TODO: add your command here\n# Args arrive as $BUTTONS_ARG_<NAME>\n"
	}
}
