package cmd

import (
	"fmt"
	"os"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/store"
	"github.com/spf13/cobra"
)

var (
	publishKind string
)

var publishCmd = &cobra.Command{
	Use:   "publish <name | @desk/name>",
	Short: "Publish a local button to the registry",
	Long: `Publish a button. The button folder
(button.json + code + AGENTS.md, never its run history) is content-hashed and
uploaded so others can add and install it.

Publish uses $BUTTONS_REGISTRY_URL as the registry base URL. This repo does not
ship a default registry host; the caller must configure the target explicitly.

A registry publish takes a scoped name (@desk/name): the on-disk button is found
by its bare name, and @desk is its registry namespace. The registry pins
immutable versions; publish starts at the button's current version and auto-bumps
to the next number if that version already exists. Auth uses the *write* key
(REGISTRY_WRITE_KEY battery or $BUTTONS_BAT_REGISTRY_WRITE_KEY) — distinct from
the read key install uses.

Examples:
  BUTTONS_REGISTRY_URL=https://registry.example buttons publish @your-desk/hello`,
	Args:              exactArgs(1),
	ValidArgsFunction: completeFirstButtonName,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if reg := registryURL(); reg != "" {
			key := registryWriteKey()
			if key == "" {
				return publishConfigError("registry write key not set: run `buttons batteries set REGISTRY_WRITE_KEY <key>` (or set $BUTTONS_BAT_REGISTRY_WRITE_KEY)")
			}
			pub := &store.HTTPPublisher{BaseURL: reg, Key: key, Kind: publishKind}
			return renderPublish(func() (*store.PublishResult, error) {
				return store.PublishToRegistry(pub, name)
			}, "to "+reg)
		}

		return publishConfigError("no publish target: set $BUTTONS_REGISTRY_URL (+ REGISTRY_WRITE_KEY) for the registry")
	},
}

// renderPublish runs a publish closure and renders the shared success/error
// output. dest describes where it went.
func renderPublish(do func() (*store.PublishResult, error), dest string) error {
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
	printNextHint("buttons add %s", res.Name)
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
	publishCmd.Flags().StringVar(&publishKind, "kind", "button", "registry entry kind: button | drawer")
	rootCmd.AddCommand(publishCmd)
}
