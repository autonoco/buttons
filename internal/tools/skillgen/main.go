// skillgen generates a single AI agent skill file from Cobra's command tree.
//
// Unlike docgen (which produces per-command pages for a docs site), skillgen
// produces ONE consolidated file optimized for LLM context windows. An agent
// that loads this skill can immediately create and press buttons without
// reading the full documentation.
//
// Usage:
//
//	go run ./internal/tools/skillgen -out ./SKILL.md
//
// The generated file follows the Claude Code / AgentSkills convention:
// structured markdown with commands, flags, examples, the JSON output
// contract, and common patterns — everything an agent needs in one read.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/autonoco/buttons/cmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func main() {
	out := flag.String("out", "./SKILL.md", "output file path")
	flag.Parse()

	root := cmd.Root()

	var b strings.Builder
	writeSkill(&b, root)

	// #nosec G306 -- generated skill file must be world-readable.
	if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("skill generated: %s\n", *out)
}

func writeSkill(b *strings.Builder, root *cobra.Command) {
	// YAML frontmatter per the AgentSkills specification
	// (https://agentskills.io/specification).
	b.WriteString("---\n")
	b.WriteString("name: buttons\n")
	b.WriteString("description: |\n")
	b.WriteString("  Deterministic workflow engine for AI agents. Create and press\n")
	b.WriteString("  reusable buttons (shell scripts, HTTP APIs, agent instructions)\n")
	b.WriteString("  with typed inputs and structured JSON output. Use when wrapping\n")
	b.WriteString("  repeatable actions, calling HTTP endpoints, or building multi-step\n")
	b.WriteString("  workflows where each step is a named, typed, pressable button.\n")
	b.WriteString("license: Apache-2.0\n")
	b.WriteString("compatibility: Requires the buttons CLI binary installed (go install github.com/autonoco/buttons@latest or curl installer).\n")
	b.WriteString("metadata:\n")
	b.WriteString("  author: autonoco\n")
	b.WriteString("  repository: https://github.com/autonoco/buttons\n")
	b.WriteString("---\n\n")

	b.WriteString("# Buttons CLI\n\n")
	b.WriteString("Deterministic workflow engine for AI agents. Create reusable, composable actions with typed inputs and structured JSON output.\n\n")

	// When to use
	b.WriteString("## When to use\n\n")
	b.WriteString("- Wrap a repeatable action (shell script, HTTP API call, agent instruction) as a named, typed, pressable button\n")
	b.WriteString("- Get structured JSON output from shell commands or HTTP endpoints\n")
	b.WriteString("- Create self-documenting actions that other agents can discover and press\n")
	b.WriteString("- Build multi-step workflows where each step is a button with typed args\n\n")

	// Quick reference
	b.WriteString("## Quick reference\n\n")
	b.WriteString("```bash\n")
	b.WriteString("# Create buttons\n")
	b.WriteString("buttons create greet --code 'echo \"Hello, $BUTTONS_ARG_NAME\"' --arg name:string:required\n")
	b.WriteString("buttons create weather --url 'https://wttr.in/{{city}}?format=j1' --arg city:string:required\n")
	b.WriteString("buttons create deploy-checklist --agent \"Verify: tests pass, staging green, team notified\"\n")
	b.WriteString("buttons create etl -f ./scripts/transform.sh --arg source:string:required\n\n")
	b.WriteString("# Press buttons\n")
	b.WriteString("buttons press weather --arg city=Miami\n")
	b.WriteString("buttons press weather --arg city=Miami --json\n")
	b.WriteString("buttons press deploy --dry-run\n\n")
	b.WriteString("# Discover and manage\n")
	b.WriteString("buttons list\n")
	b.WriteString("buttons weather              # detail view for a single button\n")
	b.WriteString("buttons history weather\n")
	b.WriteString("buttons delete weather\n")
	b.WriteString("buttons version --json\n")
	b.WriteString("```\n\n")

	// Commands reference (auto-generated from Cobra)
	b.WriteString("## Commands\n\n")
	writeCommand(b, root, 0)
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		writeCommand(b, c, 0)
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" {
				continue
			}
			writeCommand(b, sub, 1)
		}
	}

	// JSON output contract
	b.WriteString("## JSON output contract\n\n")
	b.WriteString("Every command supports `--json`. Piped/non-TTY output auto-detects and switches to JSON.\n\n")
	b.WriteString("Success:\n")
	b.WriteString("```json\n")
	b.WriteString("{\"ok\": true, \"data\": { ... }}\n")
	b.WriteString("```\n\n")
	b.WriteString("Error:\n")
	b.WriteString("```json\n")
	b.WriteString("{\"ok\": false, \"error\": {\"code\": \"NOT_FOUND\", \"message\": \"button not found: deploy\"}}\n")
	b.WriteString("```\n\n")
	b.WriteString("Error codes: `NOT_FOUND`, `MISSING_ARG`, `VALIDATION_ERROR`, `TIMEOUT`, `SCRIPT_ERROR`, `RUNTIME_MISSING`, `INTERNAL_ERROR`, `NOT_IMPLEMENTED`.\n\n")

	// Argument types
	b.WriteString("## Argument types\n\n")
	b.WriteString("Define at create time: `--arg name:type:required|optional`\n\n")
	b.WriteString("| Type | Values | Example |\n")
	b.WriteString("|------|--------|---------|\n")
	b.WriteString("| `string` | Any text | `--arg city:string:required` |\n")
	b.WriteString("| `int` | Integer | `--arg count:int:optional` |\n")
	b.WriteString("| `bool` | `true`/`false`/`1`/`0` | `--arg verbose:bool:optional` |\n\n")
	b.WriteString("Pass at press time: `--arg key=value`\n\n")
	b.WriteString("- **Code buttons:** args become `BUTTONS_ARG_<NAME>` environment variables\n")
	b.WriteString("- **HTTP buttons:** args substitute into `{{arg}}` URL/body templates (context-aware encoded)\n\n")

	// Button sources
	b.WriteString("## Button sources\n\n")
	b.WriteString("| Flag | Source | Runtime |\n")
	b.WriteString("|------|--------|--------|\n")
	b.WriteString("| `--code` | Inline script | `--runtime shell\\|python\\|node` (default: shell) |\n")
	b.WriteString("| `--code-stdin` | Piped script from stdin | Same as `--code` |\n")
	b.WriteString("| `-f`/`--file` | Existing script file (copied into button folder) | Detected from shebang |\n")
	b.WriteString("| `--url` | HTTP endpoint with `{{arg}}` templates | HTTP client |\n")
	b.WriteString("| `--agent` | Instruction for the consuming agent | Returns text, no execution |\n\n")
	b.WriteString("`--agent` can be combined with any other source as a modifier.\n\n")

	// Common patterns
	b.WriteString("## Common patterns\n\n")
	b.WriteString("### Create, press, inspect lifecycle\n")
	b.WriteString("```bash\n")
	b.WriteString("buttons create check-health --url 'https://api.example.com/health' -d \"Health check\"\n")
	b.WriteString("buttons press check-health --json\n")
	b.WriteString("buttons check-health         # detail view: args, last run, usage examples\n")
	b.WriteString("buttons history check-health  # all past runs\n")
	b.WriteString("```\n\n")

	b.WriteString("### Code button with agent context\n")
	b.WriteString("```bash\n")
	b.WriteString("buttons create check-logs \\\n")
	b.WriteString("  --code 'tail -100 /var/log/app.log' \\\n")
	b.WriteString("  --agent \"Summarize any errors or warnings from these logs\"\n")
	b.WriteString("```\n\n")
	b.WriteString("The `agent_prompt` field appears in `--json` output so the calling agent knows what to do with the stdout.\n\n")

	b.WriteString("### HTTP button hitting a local dev server\n")
	b.WriteString("```bash\n")
	b.WriteString("buttons create local-api \\\n")
	b.WriteString("  --url 'http://localhost:3000/api/{{endpoint}}' \\\n")
	b.WriteString("  --arg endpoint:string:required \\\n")
	b.WriteString("  --allow-private-networks\n")
	b.WriteString("```\n\n")
	b.WriteString("`--allow-private-networks` is required for localhost/private-IP targets (blocked by default for SSRF protection).\n\n")

	// Storage
	b.WriteString("## Storage\n\n")
	b.WriteString("All data lives under `~/.buttons/` (override with `BUTTONS_HOME`):\n\n")
	b.WriteString("```\n")
	b.WriteString("~/.buttons/buttons/<name>/\n")
	b.WriteString("  button.json     # spec (args, runtime, timeout)\n")
	b.WriteString("  main.sh         # code file (.sh, .py, .js)\n")
	b.WriteString("  AGENT.md        # agent instruction\n")
	b.WriteString("  pressed/        # run history as JSON files\n")
	b.WriteString("```\n")
}

func writeCommand(b *strings.Builder, c *cobra.Command, depth int) {
	prefix := strings.Repeat("#", 3+depth)
	b.WriteString(fmt.Sprintf("%s `%s`\n\n", prefix, c.CommandPath()))

	if c.Short != "" {
		b.WriteString(c.Short + "\n\n")
	}

	// Usage line
	if c.Use != "" {
		b.WriteString("```\n")
		b.WriteString(c.CommandPath())
		if c.HasAvailableSubCommands() {
			b.WriteString(" [command]")
		}
		if c.HasAvailableFlags() {
			b.WriteString(" [flags]")
		}
		b.WriteString("\n```\n\n")
	}

	// Flags (skip inherited/help)
	flags := collectFlags(c)
	if len(flags) > 0 {
		b.WriteString("| Flag | Type | Description |\n")
		b.WriteString("|------|------|-------------|\n")
		for _, f := range flags {
			b.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", flagName(f), f.Value.Type(), f.Usage))
		}
		b.WriteString("\n")
	}

	// Example
	if c.Example != "" {
		b.WriteString("```bash\n")
		b.WriteString(strings.TrimSpace(c.Example) + "\n")
		b.WriteString("```\n\n")
	}
}

func collectFlags(c *cobra.Command) []*pflag.Flag {
	var flags []*pflag.Flag
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		flags = append(flags, f)
	})
	return flags
}

func flagName(f *pflag.Flag) string {
	name := "--" + f.Name
	if f.Shorthand != "" {
		name = "-" + f.Shorthand + ", " + name
	}
	return name
}
