# Cobra CLI Framework — Full Reference

Source: [cobra.dev](https://cobra.dev/) — Build Modern CLIs That Users Love

Used by: Kubernetes (kubectl), Docker, GitHub CLI, Hugo, Helm, Istio, Prometheus, and 173,000+ projects.

---

## Philosophy

### 1. CLI as a User Interface

The CLI is a user experience. Applications should be intuitive, discoverable, and helpful. Users explore through help commands, tab completion, and logical hierarchies. Commands need both `Short` descriptions and comprehensive `Long` documentation. Consistent patterns in naming, flag usage, and output formatting reduce cognitive load.

### 2. Convention Over Configuration

Sensible defaults minimize decision fatigue while preserving customization. Cobra provides automatic help generation, argument validation, error handling, and parent command integration out of the box.

**Configuration hierarchy**: Flags > Environment variables > Config files > Defaults

### 3. Batteries Included, But Swappable

Built-in: command parsing, flag handling, shell completion (bash/zsh/fish/PowerShell), man page generation, markdown doc generation. All customizable via `SetHelpCommand()`, `SetUsageTemplate()`, custom validators, hooks, and completion functions.

---

## Command Pattern

```
appName command [arguments] --flag value
```

### Hook Execution Order

1. `PersistentPreRun` (inherited from parent)
2. `PreRun` (this command only)
3. `Run`/`RunE` (main execution)
4. `PostRun` (this command only)
5. `PersistentPostRun` (inherited from parent)

---

## First CLI Tutorial

```bash
mkdir my-cli && cd my-cli
go mod init my-cli
go get -u github.com/spf13/cobra@latest
go install github.com/spf13/cobra-cli@latest
cobra-cli init
cobra-cli add serve
go build -o my-cli
./my-cli serve
```

---

## Working with Commands

### Basic Command

```go
var greetCmd = &cobra.Command{
    Use:   "greet [name]",
    Short: "Greet someone",
    Long:  "Greet someone with a friendly message.",
    Example: `  myapp greet World
  myapp greet --uppercase Alice`,
    Args: cobra.MaximumNArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        name := "World"
        if len(args) > 0 {
            name = args[0]
        }
        fmt.Printf("Hello, %s!\n", name)
        return nil
    },
}

func init() {
    rootCmd.AddCommand(greetCmd)
}
```

### Aliases

```go
var installCmd = &cobra.Command{
    Use:     "install [packages]",
    Aliases: []string{"i", "add"},
    // ...
}
```

### Error Handling with RunE

Prefer `RunE` over `Run`:
- `cmd.SilenceUsage = true` prevents usage display for runtime errors
- `cmd.SilenceErrors = true` suppresses Cobra's error printing
- `nil` returns exit code 0; errors trigger stderr + non-zero exit

### Command Grouping

```go
rootCmd.AddGroup(
    &cobra.Group{ID: "core", Title: "Core Commands:"},
    &cobra.Group{ID: "admin", Title: "Admin Commands:"},
)
```

Introduce grouping around 8-10 subcommands or when natural categories emerge.

### Organization: Simple Layout

Single `cmd` package with one file per command.

### Organization: Modular Layout

Feature packages that return `*cobra.Command` via constructor functions (`NewCommand()`). Isolates dependencies — serve features won't import build feature code.

---

## Working with Flags

### Local Flags

Defined on a single command, don't inherit to children. Use as default.

```go
serveCmd.Flags().IntP("port", "p", 8080, "port to listen on")
```

### Persistent Flags

Defined on parent, available to all descendants unless shadowed. Use sparingly.

```go
rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
```

### Flag Types

- `String`, `StringP`, `StringVar`, `StringVarP`
- `Int`, `IntP`, `IntVar`, `IntVarP`
- `Bool`, `BoolP`, `BoolVar`, `BoolVarP`
- `Duration` (parses `300ms`, `5s`, `1m`)
- `StringSlice` / `StringArray` (repeatable values)

### Reading Patterns

**On-demand** in RunE:
```go
port, err := cmd.Flags().GetInt("port")
```

**Variable binding** at init:
```go
var port int
serveCmd.Flags().IntVarP(&port, "port", "p", 8080, "port")
```

### Shorthand Flags

Use the `P` variant methods. Avoid collisions across siblings. Reserve `-h` for help.

### Required Flags

```go
loginCmd.Flags().StringVar(&user, "username", "", "username")
loginCmd.MarkFlagRequired("username")
```

### Flag Validation in PreRunE

```go
PreRunE: func(cmd *cobra.Command, args []string) error {
    if stdout && outPath != "" {
        return fmt.Errorf("--stdout and --out are mutually exclusive")
    }
    switch format {
    case "json", "yaml":
    default:
        return fmt.Errorf("invalid --format: %s (want json|yaml)", format)
    }
    return nil
},
```

### Hidden & Deprecated Flags

```go
rootCmd.PersistentFlags().MarkHidden("config")
rootCmd.PersistentFlags().MarkDeprecated("colour", "use --color instead")
```

Keep deprecated flags active for at least one minor release with clear messaging.

### Viper Integration

```go
rootCmd.PersistentFlags().Int("port", 8080, "port to listen on")
viper.BindPFlag("port", rootCmd.PersistentFlags().Lookup("port"))

viper.SetEnvPrefix("flagsapp")
viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
viper.AutomaticEnv()
```

Precedence: flag > env > config > default

### Complete Flag Example

```go
var (
    outPath string
    stdout  bool
    format  string
    tags    []string
)

var exportCmd = &cobra.Command{
    Use: "export",
    PreRunE: func(cmd *cobra.Command, args []string) error {
        if stdout && outPath != "" {
            return fmt.Errorf("--stdout and --out are mutually exclusive")
        }
        switch format {
        case "json", "yaml":
        default:
            return fmt.Errorf("invalid --format: %s (want json|yaml)", format)
        }
        return nil
    },
    RunE: func(cmd *cobra.Command, args []string) error {
        fmt.Printf("Output: %s, Format: %s, Tags: %v\n", outPath, format, tags)
        return nil
    },
}

func init() {
    exportCmd.Flags().StringVar(&outPath, "out", "", "write to file path")
    exportCmd.Flags().BoolVar(&stdout, "stdout", false, "write to stdout")
    exportCmd.Flags().StringVar(&format, "format", "json", "output format")
    exportCmd.Flags().StringSliceVar(&tags, "tag", nil, "add tag (repeatable)")
    rootCmd.AddCommand(exportCmd)
}
```

---

## Shell Completions

### Generation

```bash
# Bash
myapp completion bash > myapp-completion.bash
sudo cp myapp-completion.bash /etc/bash_completion.d/

# Zsh
myapp completion zsh > _myapp
sudo cp _myapp /usr/local/share/zsh/site-functions/

# Fish
myapp completion fish > ~/.config/fish/completions/myapp.fish

# PowerShell
myapp completion powershell > myapp.ps1
Add-Content $PROFILE ". /path/to/myapp.ps1"
```

### Custom Completions

**Static:**
```go
ValidArgs: []string{"dev", "staging", "production"},
```

**Dynamic:**
```go
ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
    return []string{"dev", "staging", "production"}, cobra.ShellCompDirectiveNoFileComp
},
```

### Flag Completions

```go
cmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
    return []string{"json", "yaml", "csv"}, cobra.ShellCompDirectiveNoFileComp
})

cmd.MarkFlagFilename("config", "yaml", "yml", "json")
cmd.MarkFlagDirname("output")
```

### Directives

| Directive | Behavior |
|-----------|----------|
| `ShellCompDirectiveDefault` | Default file completion |
| `ShellCompDirectiveNoFileComp` | No file completion |
| `ShellCompDirectiveNoSpace` | No space after completion |
| `ShellCompDirectiveFilterDirs` | Only directories |
| `ShellCompDirectiveFilterFileExt` | Filter by extension |
| `ShellCompDirectiveKeepOrder` | Preserve order |
| `ShellCompDirectiveError` | Error occurred |

---

## Documentation Generation

```go
import "github.com/spf13/cobra/doc"

// Markdown
doc.GenMarkdownTree(rootCmd, "./docs")

// With frontmatter
doc.GenMarkdownTreeCustom(rootCmd, "./docs", prepender, linkHandler)

// Man pages
header := &doc.GenManHeader{Title: "MYAPP", Section: "1"}
doc.GenManTree(rootCmd, header, "./man")

// reStructuredText
doc.GenReSTTree(rootCmd, "./docs")
```

Set `rootCmd.DisableAutoGenTag = true` to prevent timestamp churn.

### Doc Generator Tool

```go
// internal/tools/docgen/main.go
package main

import (
    "flag"
    "log"
    "os"
    "github.com/spf13/cobra/doc"
    "example.com/myapp/cmd"
)

func main() {
    out := flag.String("out", "./docs/cli", "output directory")
    flag.Parse()

    os.MkdirAll(*out, 0o755)
    root := cmd.Root()
    root.DisableAutoGenTag = true

    if err := doc.GenMarkdownTree(root, *out); err != nil {
        log.Fatal(err)
    }
}
```

### Optimizing for LLMs

- One command per file (Cobra's generators do this by default)
- Populate substantial `Example` sections on each command
- Craft detailed `Long` descriptions
- Set `DisableAutoGenTag`
- Assign `GroupID` for organized help

---

## Notable Projects Using Cobra

| Project | Category |
|---------|----------|
| Kubernetes (kubectl) | Infrastructure |
| Docker | Infrastructure |
| GitHub CLI (gh) | Developer Tools |
| Hugo | Static Site Gen |
| Helm | Package Manager |
| Istio | Service Mesh |
| Prometheus | Monitoring |
| Jaeger | Tracing |
| Linkerd | Service Mesh |
| Flux | GitOps |
| Viper | Configuration |

---

## Recommended Libraries

- **Cobra**: CLI framework — `github.com/spf13/cobra`
- **Viper**: Configuration — `github.com/spf13/viper`
- **Bubble Tea**: TUI framework — `github.com/charmbracelet/bubbletea`
- **Lip Gloss**: Styling — `github.com/charmbracelet/lipgloss`
- **Huh**: Forms/prompts — `github.com/charmbracelet/huh`
