// docgen generates CLI reference documentation from Cobra's command tree.
//
// Usage:
//
//	go run ./internal/tools/docgen -out ./docs/cli -format markdown -frontmatter
//
// The generated markdown files are Mintlify-compatible: each file gets YAML
// frontmatter with title + description, and cross-command links use relative
// paths that match Mintlify's page routing.
//
// Run this after adding or changing any Cobra command/flag, then commit the
// updated docs alongside the code change.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/autonoco/buttons/cmd"
	"github.com/spf13/cobra/doc"
)

func main() {
	out := flag.String("out", "./docs/cli", "output directory for generated docs")
	format := flag.String("format", "markdown", "output format: markdown | man | rest")
	front := flag.Bool("frontmatter", true, "prepend Mintlify-compatible YAML front matter to markdown")
	flag.Parse()

	// #nosec G301 -- docs output directory needs to be world-readable for Mintlify and CI.
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}

	root := cmd.Root()
	root.DisableAutoGenTag = true

	switch *format {
	case "markdown":
		if *front {
			// Mintlify-compatible front matter: title + description.
			// The title is the command path with underscores replaced
			// by spaces (e.g. "buttons create"). The description pulls
			// from the command's Short field.
			prep := func(filename string) string {
				base := filepath.Base(filename)
				name := strings.TrimSuffix(base, filepath.Ext(base))
				title := strings.ReplaceAll(name, "_", " ")
				return fmt.Sprintf("---\ntitle: %q\ndescription: \"CLI reference for %s\"\n---\n\n", title, title)
			}

			// Link function: makes cross-references point to relative
			// paths within the same docs/cli/ directory, lowercased to
			// match Mintlify's URL convention.
			link := func(name string) string {
				return strings.ToLower(name)
			}

			if err := doc.GenMarkdownTreeCustom(root, *out, prep, link); err != nil {
				log.Fatal(err)
			}
		} else {
			if err := doc.GenMarkdownTree(root, *out); err != nil {
				log.Fatal(err)
			}
		}

	case "man":
		hdr := &doc.GenManHeader{
			Title:   strings.ToUpper(root.Name()),
			Section: "1",
		}
		if err := doc.GenManTree(root, hdr, *out); err != nil {
			log.Fatal(err)
		}

	case "rest":
		if err := doc.GenReSTTree(root, *out); err != nil {
			log.Fatal(err)
		}

	default:
		log.Fatalf("unknown format: %s", *format)
	}

	// Post-process markdown files for MDX compatibility. Cobra's doc
	// generator outputs Examples and literal synopsis blocks as indented
	// text (not fenced code blocks) and uses <placeholder> patterns in
	// prose. MDX parses {{ as JSX expressions and <placeholder> as HTML
	// tags, both of which break the build.
	if *format == "markdown" {
		if err := postProcessMDX(*out); err != nil {
			log.Fatalf("post-process failed: %v", err)
		}
	}

	fmt.Printf("docs generated in %s (%s format)\n", *out, *format)
}

// angleBracketPattern matches <placeholder> patterns in prose that MDX would
// interpret as unclosed HTML tags (e.g. <name>, <value>, BUTTONS_ARG_<NAME>).
var angleBracketPattern = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9_-]*)>`)

// postProcessMDX walks every .md file in dir and fixes two MDX-incompatible
// patterns that Cobra's doc generator produces:
//
//  1. Examples blocks: indented text containing {{arg}} templates and JSON
//     with curly braces. These get wrapped in fenced code blocks so MDX
//     treats them as literal text.
//  2. Other indented synopsis/literal blocks: these get fenced too so
//     route templates, JSON examples, and ${...} refs stay literal.
//  3. <placeholder> patterns in prose (e.g. BUTTONS_ARG_<NAME>): these get
//     wrapped in backtick inline code so MDX doesn't parse them as tags.
func postProcessMDX(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		// #nosec G304 -- path is constructed from the output directory we own
		// + a DirEntry name from os.ReadDir, not user input.
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		content := string(data)
		content = fenceExamples(content)
		content = fenceIndentedBlocks(content)
		content = escapeAngleBrackets(content)

		// #nosec G306 -- generated docs must be world-readable.
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// fenceExamples finds "Examples:" sections followed by indented lines and
// wraps the indented block in a ``` code fence. Cobra's doc generator
// outputs examples as 2-space-indented text which MDX tries to parse.
func fenceExamples(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	i := 0
	for i < len(lines) {
		line := lines[i]

		// Look for a line that is exactly "Examples:" or starts with "Examples:"
		// followed by indented example lines.
		if strings.TrimSpace(line) == "Examples:" {
			result = append(result, "**Examples:**")
			result = append(result, "")
			result = append(result, "```bash")
			i++
			// Consume all indented lines (2+ spaces) as the example block.
			for i < len(lines) && len(lines[i]) > 0 && (lines[i][0] == ' ' || lines[i][0] == '\t') {
				// Strip the leading 2-space indent Cobra adds.
				trimmed := strings.TrimPrefix(lines[i], "  ")
				result = append(result, trimmed)
				i++
			}
			result = append(result, "```")
			// Don't increment i — the current line is the first non-indented
			// line after the block and needs normal processing.
			continue
		}

		result = append(result, line)
		i++
	}
	return strings.Join(result, "\n")
}

// fenceIndentedBlocks wraps non-example literal blocks emitted by Cobra as
// 2-space-indented text. MDX otherwise tries to parse route placeholders
// (/api/buttons/{name}), JSON snippets, and ${...} examples as JSX.
func fenceIndentedBlocks(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	inFence := false
	inIndentedBlock := false

	closeBlock := func() {
		if inIndentedBlock {
			result = append(result, "```")
			inIndentedBlock = false
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			closeBlock()
			inFence = !inFence
			result = append(result, line)
			continue
		}
		if inFence {
			result = append(result, line)
			continue
		}

		indented := strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")
		if indented {
			if !inIndentedBlock {
				result = append(result, "")
				result = append(result, "```text")
				inIndentedBlock = true
			}
			result = append(result, line)
			continue
		}

		closeBlock()
		result = append(result, line)
	}
	closeBlock()
	return strings.Join(result, "\n")
}

// escapeAngleBrackets finds <placeholder> patterns in lines that are NOT inside
// fenced code blocks and wraps the surrounding token in backtick inline code.
// For example: BUTTONS_ARG_<NAME> → `BUTTONS_ARG_<NAME>`
func escapeAngleBrackets(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	inFence := false

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			result = append(result, line)
			continue
		}
		if inFence {
			result = append(result, line)
			continue
		}
		// Outside a code fence: escape <placeholder> by wrapping it in
		// backticks, but leave placeholders already inside inline code
		// alone.
		if angleBracketPattern.MatchString(line) {
			line = replaceAnglePlaceholdersOutsideInlineCode(line)
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

func replaceAnglePlaceholdersOutsideInlineCode(line string) string {
	matches := angleBracketPattern.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return line
	}

	var b strings.Builder
	last := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		if insideInlineCode(line, start) {
			continue
		}
		b.WriteString(line[last:start])
		b.WriteByte('`')
		b.WriteString(line[start:end])
		b.WriteByte('`')
		last = end
	}
	if last == 0 {
		return line
	}
	b.WriteString(line[last:])
	return b.String()
}

func insideInlineCode(line string, idx int) bool {
	count := 0
	for i := 0; i < idx && i < len(line); i++ {
		if line[i] == '`' {
			count++
		}
	}
	return count%2 == 1
}
