---
name: cobra
description: >
    Build Go CLI applications with Cobra framework. Covers command structure, flags (local/persistent/required),
    hooks (PreRun/PostRun), shell completions, Viper config integration, error handling, and doc generation.

    Use when: building Go CLIs, adding Cobra commands or subcommands, working with flags,
    generating shell completions, or troubleshooting "unknown command" or flag parsing errors.
---

Apply these patterns when building CLI applications with Cobra. For full examples and rationale, see [references/](references/).

## Setup

```bash
go get -u github.com/spf13/cobra@latest
go install github.com/spf13/cobra-cli@latest
cobra-cli init          # scaffold project
cobra-cli add serve     # add subcommand
```

## Command Structure

```go
var serveCmd = &cobra.Command{
    Use:     "serve [flags]",
    Short:   "Start the HTTP server",
    Long:    "Start the HTTP server with sensible defaults.\nUseful in development and production.",
    Example: `  myapp serve
  myapp serve --port 9090`,
    Aliases: []string{"s", "start"},
    GroupID: "server",                    // for grouped help output
    Args:    cobra.ExactArgs(0),          // argument validation
    RunE: func(cmd *cobra.Command, args []string) error {
        cmd.SilenceUsage = true           // don't show usage on runtime errors
        // ...
        return nil
    },
}

func init() {
    rootCmd.AddCommand(serveCmd)
}
```

### Critical Fields

| Field | Purpose |
|-------|---------|
| `Use` | Command name + usage pattern. First word is the command name. |
| `Short` | One-line description shown in parent's help |
| `Long` | Full description shown in command's own help |
| `Example` | Code examples (indent with 2 spaces) |
| `RunE` | Main execution (prefer over `Run` — returns errors) |
| `Args` | Argument validation function |
| `Aliases` | Alternative names |
| `GroupID` | Group in parent's help (set with `rootCmd.AddGroup()`) |

### Argument Validators

```go
cobra.NoArgs              // error if any args
cobra.ExactArgs(n)        // exactly n args
cobra.MinimumNArgs(n)     // at least n args
cobra.MaximumNArgs(n)     // at most n args
cobra.RangeArgs(min, max) // between min and max
cobra.ArbitraryArgs       // any args (default)
cobra.OnlyValidArgs       // only args listed in ValidArgs
```

Custom validator:
```go
Args: func(cmd *cobra.Command, args []string) error {
    if len(args) < 1 {
        return fmt.Errorf("requires a name argument")
    }
    return nil
},
```

## Flags

### Local vs Persistent

```go
// Local: only on this command
serveCmd.Flags().IntP("port", "p", 8080, "port to listen on")

// Persistent: inherited by all child commands
rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
```

**Rule**: Use local flags by default. Only use persistent for truly global concerns (verbose, config path, output format).

### Flag Types

```go
// Variable binding (access via variable)
var port int
serveCmd.Flags().IntVarP(&port, "port", "p", 8080, "port to listen on")

// On-demand reading (access in RunE)
port, _ := cmd.Flags().GetInt("port")

// Common types
cmd.Flags().String("name", "", "description")
cmd.Flags().StringP("name", "n", "", "description")     // with shorthand
cmd.Flags().Bool("force", false, "description")
cmd.Flags().Duration("timeout", 30*time.Second, "timeout")
cmd.Flags().StringSlice("tags", nil, "repeatable tags")  // --tags a --tags b
cmd.Flags().StringArray("env", nil, "env vars")           // no CSV splitting
```

### Required Flags

```go
serveCmd.Flags().StringVar(&addr, "addr", "", "listen address")
serveCmd.MarkFlagRequired("addr")
```

### Flag Groups

```go
// Mutually exclusive: only one allowed
serveCmd.MarkFlagsMutuallyExclusive("json", "yaml")

// Required together: all or none
serveCmd.MarkFlagsRequiredTogether("username", "password")

// One required: at least one
serveCmd.MarkFlagsOneRequired("json", "yaml", "text")
```

### Flag Validation in PreRunE

```go
PreRunE: func(cmd *cobra.Command, args []string) error {
    format, _ := cmd.Flags().GetString("format")
    switch format {
    case "json", "yaml":
    default:
        return fmt.Errorf("invalid --format: %s (want json|yaml)", format)
    }
    return nil
},
```

### Hidden and Deprecated Flags

```go
cmd.Flags().MarkHidden("internal-debug")
cmd.Flags().MarkDeprecated("colour", "use --color instead")
```

## Hooks (Execution Lifecycle)

Order: `PersistentPreRun` > `PreRun` > `Run` > `PostRun` > `PersistentPostRun`

```go
var deployCmd = &cobra.Command{
    Use: "deploy",
    PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
        // runs before this cmd AND all children
        return initConfig()
    },
    PreRunE: func(cmd *cobra.Command, args []string) error {
        // runs before this cmd only — good for validation
        return validateFlags(cmd)
    },
    RunE: func(cmd *cobra.Command, args []string) error {
        // main logic
        return doDeploy(args)
    },
    PostRunE: func(cmd *cobra.Command, args []string) error {
        // cleanup after this cmd
        return nil
    },
}
```

**Key rule**: Parent `PersistentPreRun` runs before child `PreRun`. If a child defines `PersistentPreRun`, it **overrides** the parent's (it does not chain). Call parent explicitly if needed.

## Error Handling

```go
RunE: func(cmd *cobra.Command, args []string) error {
    cmd.SilenceUsage = true    // don't print usage on runtime errors
    cmd.SilenceErrors = true   // suppress Cobra's own error printing

    if err := doWork(); err != nil {
        return fmt.Errorf("deploy failed: %w", err)
    }
    return nil
},
```

- `nil` return = exit code 0
- Non-nil error = message to stderr + non-zero exit
- Use `SilenceUsage = true` so runtime errors don't dump help text

## Shell Completions

### Generation Commands

Add a `completion` subcommand (Cobra can auto-generate this):

```go
// Cobra provides this automatically if you don't override it
// Users run: myapp completion bash|zsh|fish|powershell
```

### Custom Completions

```go
var statusCmd = &cobra.Command{
    Use:       "status [environment]",
    ValidArgs: []string{"dev", "staging", "production"},
    // OR dynamic:
    ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
        if len(args) != 0 {
            return nil, cobra.ShellCompDirectiveNoFileComp
        }
        return []string{"dev", "staging", "production"}, cobra.ShellCompDirectiveNoFileComp
    },
}
```

### Flag Completions

```go
cmd.RegisterFlagCompletionFunc("format", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
    return []string{"json", "yaml", "csv"}, cobra.ShellCompDirectiveNoFileComp
})

cmd.MarkFlagFilename("config", "yaml", "yml", "json")  // complete file paths
cmd.MarkFlagDirname("output")                            // complete directories
```

### Shell Completion Directives

```go
cobra.ShellCompDirectiveDefault       // default (file completion)
cobra.ShellCompDirectiveNoFileComp    // no file completion
cobra.ShellCompDirectiveNoSpace       // no space after completion
cobra.ShellCompDirectiveFilterDirs    // only directories
cobra.ShellCompDirectiveFilterFileExt // filter by extension
cobra.ShellCompDirectiveKeepOrder     // preserve suggestion order
cobra.ShellCompDirectiveError         // error occurred
```

## Viper Integration

```go
import "github.com/spf13/viper"

func initConfig() {
    viper.SetConfigName("config")
    viper.SetConfigType("yaml")
    viper.AddConfigPath(".")
    viper.AddConfigPath("$HOME/.myapp")

    viper.SetEnvPrefix("MYAPP")
    viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
    viper.AutomaticEnv()

    viper.ReadInConfig() // ignore error if no config file
}

func init() {
    cobra.OnInitialize(initConfig)

    rootCmd.PersistentFlags().Int("port", 8080, "port")
    viper.BindPFlag("port", rootCmd.PersistentFlags().Lookup("port"))
}
```

**Precedence**: flag > env var > config file > default

## Command Grouping

```go
func init() {
    rootCmd.AddGroup(
        &cobra.Group{ID: "core", Title: "Core Commands:"},
        &cobra.Group{ID: "admin", Title: "Admin Commands:"},
    )
    rootCmd.AddCommand(serveCmd)    // serveCmd.GroupID = "core"
    rootCmd.AddCommand(migrateCmd)  // migrateCmd.GroupID = "admin"
}
```

Introduce grouping around 8-10 subcommands or when you have natural categories.

## Documentation Generation

```go
import "github.com/spf13/cobra/doc"

// Markdown (one file per command)
doc.GenMarkdownTree(rootCmd, "./docs")

// Man pages
header := &doc.GenManHeader{Title: "MYAPP", Section: "1"}
doc.GenManTree(rootCmd, header, "./man")

// With custom frontmatter
doc.GenMarkdownTreeCustom(rootCmd, "./docs", prepender, linkHandler)
```

## Project Organization

### Simple (single cmd package)
```
cmd/
  root.go       // rootCmd + Execute()
  serve.go      // serveCmd
  deploy.go     // deployCmd
main.go          // calls cmd.Execute()
```

### Modular (feature packages)
```
cmd/
  root.go
internal/
  serve/
    command.go   // func NewCommand() *cobra.Command
  deploy/
    command.go
```

```go
// internal/serve/command.go
func NewCommand() *cobra.Command {
    return &cobra.Command{
        Use: "serve",
        RunE: func(cmd *cobra.Command, args []string) error { ... },
    }
}

// cmd/root.go
rootCmd.AddCommand(serve.NewCommand())
```

## Common Mistakes

- **Using `Run` instead of `RunE`**: Always prefer `RunE` so errors propagate correctly.
- **Forgetting `SilenceUsage`**: Without it, every error dumps the full usage text.
- **Overusing persistent flags**: Only for truly global concerns. Local flags are the default.
- **Not setting `DisableAutoGenTag`**: Generated docs get noisy timestamp comments.
- **Forgetting shorthand collisions**: Reserve `-h` for help, `-v` for version. Don't reuse across siblings.
