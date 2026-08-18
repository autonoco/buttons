package cmd

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	buttonsauth "github.com/autonoco/buttons/internal/auth"
	"github.com/autonoco/buttons/internal/battery"
	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

const defaultRegistryURL = "https://api.buttons.sh"

var (
	loginRegistry           string
	loginNoBrowser          bool
	loginSwitchOrganization bool
	logoutRegistry          string
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Sign in to a registry with OAuth",
	Long: `Sign in through the registry's standard OAuth/OIDC provider.

The CLI discovers the provider, opens an authorization-code + PKCE flow on a
127.0.0.1 loopback callback, and stores one credential envelope in the operating
system keychain. The CLI contains no provider-specific authentication code.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := os.Hostname()
		client := buttonsauth.NewClient(buttonsauth.KeyringStore{})
		client.OpenBrowser = openBrowser
		credential, err := client.Login(cmd.Context(), buttonsauth.LoginOptions{
			RegistryURL:        loginRegistry,
			Label:              host,
			NoBrowser:          loginNoBrowser,
			SwitchOrganization: loginSwitchOrganization,
		})
		if err != nil {
			return loginCommandError("LOGIN_ERROR", err)
		}

		// The registry URL is non-secret configuration. OAuth tokens and the
		// publish capability exist only in the OS keychain envelope above.
		registry := strings.TrimRight(loginRegistry, "/")
		if svc, batteryErr := newBatteryService(); batteryErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not persist REGISTRY_URL: %v\n", batteryErr)
		} else if setErr := svc.Set("REGISTRY_URL", registry, battery.ScopeGlobal); setErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not persist REGISTRY_URL: %v\n", setErr)
		}

		if jsonOutput {
			return config.WriteJSON(map[string]any{
				"organization_id": credential.OrganizationID,
				"registry_url":    registry,
			})
		}
		fmt.Fprintf(os.Stderr, "Logged in to organization %s.\n", credential.OrganizationID)
		printNextHint("publish with `buttons publish @<namespace>/<name>`")
		return nil
	},
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Revoke registry OAuth credentials and sign out",
	Long: `Revoke both the bounded publish capability and the OAuth refresh token,
then remove the credential envelope from the operating system keychain.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := buttonsauth.NewClient(buttonsauth.KeyringStore{})
		registry := resolveLogoutRegistry(logoutRegistry, registryURL)
		if err := client.Logout(cmd.Context(), registry); err != nil {
			return loginCommandError("LOGOUT_ERROR", err)
		}
		if jsonOutput {
			return config.WriteJSON(map[string]any{"registry_url": registry, "revoked": true})
		}
		fmt.Fprintln(os.Stderr, "Logged out and revoked this machine's credentials.")
		return nil
	},
}

func loginCommandError(code string, err error) error {
	if !jsonOutput {
		return err
	}
	_ = config.WriteJSONError(code, err.Error())
	return errSilent
}

func openBrowser(target string) bool {
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return false
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target) // #nosec G204 -- scheme-validated URL, argv only
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target) // #nosec G204 -- scheme-validated URL, argv only
	default:
		command = exec.Command("xdg-open", target) // #nosec G204 -- scheme-validated URL, argv only
	}
	return command.Start() == nil
}

func init() {
	loginCmd.Flags().StringVar(&loginRegistry, "registry", envOr("BUTTONS_REGISTRY_URL", defaultRegistryURL), "registry base URL")
	loginCmd.Flags().BoolVar(&loginNoBrowser, "no-browser", false, "print the authorization URL instead of opening a browser")
	loginCmd.Flags().BoolVar(&loginSwitchOrganization, "switch-organization", false, "revoke the current login and select another organization")
	logoutCmd.Flags().StringVar(&logoutRegistry, "registry", "", "registry base URL (defaults to the configured registry)")
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
}

func resolveLogoutRegistry(explicit string, configured func() string) string {
	if registry := strings.TrimRight(explicit, "/"); registry != "" {
		return registry
	}
	if registry := strings.TrimRight(configured(), "/"); registry != "" {
		return registry
	}
	return defaultRegistryURL
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
