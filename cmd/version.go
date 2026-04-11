package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

// Build-time variables injected via `-ldflags -X`. See the `build`
// target in the Makefile for the wiring. The defaults here are the
// values you get when someone runs `go build .` without the Makefile —
// "dev" is intentionally more honest than a stale hardcoded version.
var (
	version = "dev"     // short version, typically a git tag or short SHA
	commit  = "unknown" // full git commit SHA
	date    = "unknown" // UTC build timestamp in ISO 8601
)

// VersionInfo is the structured form of `buttons version`. Exported so
// the same shape is usable elsewhere in the CLI (e.g. a future `update`
// command that needs to compare the running version with the latest
// release).
type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"go_version"`
	Os        string `json:"os"`
	Arch      string `json:"arch"`
}

// versionInfo returns the injected build info plus runtime details.
func versionInfo() VersionInfo {
	return VersionInfo{
		Version:   version,
		Commit:    commit,
		Date:      date,
		GoVersion: runtime.Version(),
		Os:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print build version, commit, and date",
	Long: `Print the build version, commit SHA, build date, Go toolchain,
and OS/architecture that this buttons binary was built with.

Examples:
  buttons version
  buttons version --json
  buttons --version        # terse, Cobra-builtin flag form`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		info := versionInfo()

		if jsonOutput {
			return config.WriteJSON(info)
		}

		// Human-readable form goes to stderr so `buttons version | grep`
		// patterns stay consistent with the rest of the CLI (stdout is
		// reserved for data/output; stderr for operator messaging).
		fmt.Fprintf(os.Stderr, "buttons %s\n", info.Version)
		fmt.Fprintf(os.Stderr, "  commit:  %s\n", info.Commit)
		fmt.Fprintf(os.Stderr, "  built:   %s\n", info.Date)
		fmt.Fprintf(os.Stderr, "  go:      %s\n", info.GoVersion)
		fmt.Fprintf(os.Stderr, "  os/arch: %s/%s\n", info.Os, info.Arch)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
