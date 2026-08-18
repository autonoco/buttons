package cmd

import "testing"

func TestLoginUsesGenericRegistryFlags(t *testing.T) {
	if loginCmd.Flags().Lookup("registry") == nil {
		t.Fatal("login must accept a registry URL")
	}
	if loginCmd.Flags().Lookup("switch-organization") == nil {
		t.Fatal("login must support an explicit organization switch")
	}
	if loginCmd.Flags().Lookup("desk") != nil {
		t.Fatal("login must not retain the Buttons Desk-specific authorization path")
	}
}

func TestBrowserOpenerRejectsNonHTTPURLs(t *testing.T) {
	if openBrowser("file:///tmp/credential") {
		t.Fatal("browser opener accepted a non-HTTP URL")
	}
}

func TestResolveLogoutRegistryUsesConfiguredRegistryWhenFlagIsAbsent(t *testing.T) {
	got := resolveLogoutRegistry("", func() string { return "https://registry.example.test/" })
	if got != "https://registry.example.test" {
		t.Fatalf("resolveLogoutRegistry() = %q", got)
	}
}

func TestResolveLogoutRegistryPrefersExplicitFlag(t *testing.T) {
	got := resolveLogoutRegistry("https://explicit.example.test/", func() string {
		t.Fatal("configured registry must not be read when --registry is explicit")
		return ""
	})
	if got != "https://explicit.example.test" {
		t.Fatalf("resolveLogoutRegistry() = %q", got)
	}
}

func TestResolveLogoutRegistryFallsBackToDefault(t *testing.T) {
	got := resolveLogoutRegistry("", func() string { return "" })
	if got != defaultRegistryURL {
		t.Fatalf("resolveLogoutRegistry() = %q, want %q", got, defaultRegistryURL)
	}
}
