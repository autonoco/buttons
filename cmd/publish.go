package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/store"
	"github.com/spf13/cobra"
)

var (
	publishSource string
	publishKind   string
)

var publishCmd = &cobra.Command{
	Use:   "publish <name | @desk/name>",
	Short: "Publish a local button to the registry (or a local source)",
	Long: `Publish a button — the inverse of 'buttons install'. The button folder
(button.json + code + AGENTS.md, never its run history) is content-hashed and
uploaded so others can 'buttons install' it.

Targets, in order:
  --source <dir> / $BUTTONS_SOURCE   a local source directory (dev round-trip)
  $BUTTONS_REGISTRY_URL              the hosted registry (publish @desk/name)

A registry publish takes a scoped name (@desk/name): the on-disk button is found
by its bare name, and @desk is its registry namespace. The registry pins
immutable versions, so button.json must carry a "version". Auth uses the *write*
key (REGISTRY_WRITE_KEY battery or $BUTTONS_BAT_REGISTRY_WRITE_KEY) — distinct
from the read key install uses.

Examples:
  BUTTONS_REGISTRY_URL=https://registry.example buttons publish @your-desk/hello
  buttons publish deploy --source ./pack`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		// A local source (--source / $BUTTONS_SOURCE) wins, mirroring install.
		srcDir := publishSource
		if srcDir == "" {
			srcDir = os.Getenv("BUTTONS_SOURCE")
		}
		if srcDir != "" {
			return renderPublish(func() (*store.PublishResult, error) {
				return store.Publish(&store.LocalSource{Root: srcDir}, name)
			}, "to "+srcDir, " --source "+srcDir)
		}

		// Otherwise the hosted registry.
		if reg := strings.TrimRight(os.Getenv("BUTTONS_REGISTRY_URL"), "/"); reg != "" {
			key := registryWriteKey()
			if key == "" {
				return publishConfigError("registry write key not set: run `buttons batteries set REGISTRY_WRITE_KEY <key>` (or set $BUTTONS_BAT_REGISTRY_WRITE_KEY)")
			}
			pub := &store.HTTPPublisher{BaseURL: reg, Key: key, Kind: publishKind}
			return renderPublish(func() (*store.PublishResult, error) {
				return store.PublishToRegistry(pub, name)
			}, "to "+reg, "")
		}

		return publishConfigError("no publish target: set $BUTTONS_REGISTRY_URL (+ REGISTRY_WRITE_KEY) for the registry, or pass --source <dir> / $BUTTONS_SOURCE for a local source")
	},
}

// renderPublish runs a publish closure and renders the shared success/error
// output. dest describes where it went; installSuffix is appended to the
// "buttons install <name>" next-step hint (e.g. " --source ./pack").
func renderPublish(do func() (*store.PublishResult, error), dest, installSuffix string) error {
	res, err := do()
	if err != nil {
		if jsonOutput {
			_ = config.WriteJSONError("PUBLISH_ERROR", err.Error())
			return errSilent
		}
		return err
	}
	if jsonOutput {
		return config.WriteJSON(res)
	}
	v := res.Version
	if v != "" {
		v = "@" + v
	}
	fmt.Fprintf(os.Stderr, "Published %s%s (%d files, sha256 %s) %s\n", res.Name, v, res.Files, res.SHA256[:12], dest)
	printNextHint("buttons install %s%s", res.Name, installSuffix)
	return nil
}

// publishConfigError renders a pre-flight validation error in the active format.
func publishConfigError(msg string) error {
	if jsonOutput {
		_ = config.WriteJSONError("VALIDATION_ERROR", msg)
		return errSilent
	}
	return fmt.Errorf("%s", msg)
}

// registryWriteKey resolves the registry *write* bearer key — the
// BUTTONS_BAT_REGISTRY_WRITE_KEY env (injected during a press) wins, else the
// REGISTRY_WRITE_KEY battery.
func registryWriteKey() string {
	if k := os.Getenv("BUTTONS_BAT_REGISTRY_WRITE_KEY"); k != "" {
		return k
	}
	if svc, err := newBatteryService(); err == nil {
		if v, _, err := svc.Get("REGISTRY_WRITE_KEY"); err == nil {
			return v
		}
	}
	return ""
}

func init() {
	publishCmd.Flags().StringVar(&publishSource, "source", "", "local source directory to publish to (else $BUTTONS_REGISTRY_URL)")
	publishCmd.Flags().StringVar(&publishKind, "kind", "button", "registry entry kind: button | drawer")
	rootCmd.AddCommand(publishCmd)
}
