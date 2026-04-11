package cmd

import (
	"encoding/json"
	"runtime"
	"testing"
)

// TestVersionInfo_Defaults verifies that without -ldflags injection the
// helper returns the in-code defaults, plus the live runtime values.
func TestVersionInfo_Defaults(t *testing.T) {
	info := versionInfo()

	if info.Version != "dev" {
		t.Errorf("Version = %q, want \"dev\" (the fallback when not ldflag-injected)", info.Version)
	}
	if info.Commit != "unknown" {
		t.Errorf("Commit = %q, want \"unknown\"", info.Commit)
	}
	if info.Date != "unknown" {
		t.Errorf("Date = %q, want \"unknown\"", info.Date)
	}

	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	if info.Os != runtime.GOOS {
		t.Errorf("Os = %q, want %q", info.Os, runtime.GOOS)
	}
	if info.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", info.Arch, runtime.GOARCH)
	}
}

// TestVersionInfo_JSONShape verifies the JSON representation stays stable —
// downstream tooling (buttons update, install.sh version check, dashboards)
// will rely on these exact field names.
func TestVersionInfo_JSONShape(t *testing.T) {
	info := VersionInfo{
		Version:   "v1.2.3",
		Commit:    "abc1234",
		Date:      "2026-04-11T00:00:00Z",
		GoVersion: "go1.26.2",
		Os:        "linux",
		Arch:      "amd64",
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal(VersionInfo): %v", err)
	}

	// Round-trip: parse into a generic map and verify every expected key
	// exists. This catches accidental tag renames without hardcoding the
	// exact JSON byte layout.
	var parsed map[string]string
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v (body: %s)", err, data)
	}

	wantKeys := map[string]string{
		"version":    "v1.2.3",
		"commit":     "abc1234",
		"date":       "2026-04-11T00:00:00Z",
		"go_version": "go1.26.2",
		"os":         "linux",
		"arch":       "amd64",
	}

	for key, want := range wantKeys {
		got, ok := parsed[key]
		if !ok {
			t.Errorf("JSON missing key %q (got %s)", key, data)
			continue
		}
		if got != want {
			t.Errorf("JSON field %q = %q, want %q", key, got, want)
		}
	}

	if len(parsed) != len(wantKeys) {
		t.Errorf("JSON has %d fields, want exactly %d — did someone add a field without updating this test?",
			len(parsed), len(wantKeys))
	}
}

// TestVersionCmd_ExistsInRoot verifies that the version subcommand is
// actually registered on rootCmd, so `buttons version` dispatches
// correctly after init() runs.
func TestVersionCmd_ExistsInRoot(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "version" {
			found = true
			break
		}
	}
	if !found {
		t.Error("versionCmd not registered on rootCmd — did init() run?")
	}
}
