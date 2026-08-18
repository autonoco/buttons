package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	buttonsauth "github.com/autonoco/buttons/internal/auth"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/store"
	"github.com/spf13/cobra"
)

var (
	publishKind string
)

var publishCmd = &cobra.Command{
	Use:   "publish <name | @desk/name>",
	Short: "Publish a local button or drawer to the registry",
	Long: `Publish a local package. A package can be a button
(.buttons/buttons/<name>/button.json + code + AGENTS.md) or a drawer
(.buttons/drawers/<name>/drawer.json + AGENTS.md). Run history under pressed/
is never published.

Publish uses $BUTTONS_REGISTRY_URL when set, otherwise it uses the registry URL
pinned by "buttons login". The https://api.buttons.sh default applies to login
and logout only; publish requires $BUTTONS_REGISTRY_URL or a pinned registry.

A registry publish takes a scoped name (@desk/name): the on-disk package is
found by its bare name, and @desk is its registry namespace. The CLI detects
whether the local package is a button or drawer from button.json or drawer.json.
The registry pins immutable versions; publish starts at the package's current
version and auto-bumps to the next number if that version already exists. Auth
uses either the explicit machine/CI key in $BUTTONS_BAT_REGISTRY_WRITE_KEY or
the human OAuth credential stored by "buttons login" in the OS keychain.

Examples:
  BUTTONS_REGISTRY_URL=https://registry.example buttons publish @your-desk/hello
  BUTTONS_REGISTRY_URL=https://registry.example buttons publish @your-desk/my-pack`,
	Args:              exactArgs(1),
	ValidArgsFunction: completeFirstButtonName,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if reg := registryURL(); reg != "" {
			key, err := registryWriteKey(cmd.Context(), reg)
			if err != nil {
				return publishConfigError(err.Error())
			}
			if key == "" {
				return publishConfigError("not logged in: run `buttons login` or set $BUTTONS_BAT_REGISTRY_WRITE_KEY for machine/CI publishing")
			}
			pub := &store.HTTPPublisher{BaseURL: reg, Key: key, Kind: publishKind}
			return renderPublish(func() (*store.PublishResult, error) {
				return store.PublishToRegistry(pub, name)
			}, "to "+reg)
		}

		return publishConfigError("no publish target: run `buttons login` or set $BUTTONS_REGISTRY_URL with $BUTTONS_BAT_REGISTRY_WRITE_KEY for machine/CI publishing")
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

// registryWriteKey keeps machine and human authorization distinct: an explicit
// press/CI key wins; otherwise the OAuth keychain envelope supplies a bounded
// human capability.
func registryWriteKey(ctx context.Context, registry string) (string, error) {
	return resolveRegistryWriteKey(ctx, registry, func(ctx context.Context, registry string) (string, error) {
		return buttonsauth.NewClient(buttonsauth.KeyringStore{}).Capability(ctx, registry)
	})
}

func resolveRegistryWriteKey(
	ctx context.Context,
	registry string,
	capability func(context.Context, string) (string, error),
) (string, error) {
	if key := os.Getenv("BUTTONS_BAT_REGISTRY_WRITE_KEY"); key != "" {
		return key, nil
	}
	key, err := capability(ctx, registry)
	if errors.Is(err, buttonsauth.ErrCredentialsNotFound) {
		return "", nil
	}
	return key, err
}

func init() {
	publishCmd.Flags().StringVar(&publishKind, "kind", "button", "registry entry kind: button | drawer")
	_ = publishCmd.Flags().MarkHidden("kind")
	rootCmd.AddCommand(publishCmd)
}
