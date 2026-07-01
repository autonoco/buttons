package cmd

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autonoco/buttons/internal/settings"
	"github.com/spf13/cobra"
)

func TestPassiveUpdateSkipsCIWithoutRegistryProbe(t *testing.T) {
	oldVersion := version
	version = "1.0.0"
	t.Cleanup(func() { version = oldVersion })

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"service":"buttons-registry","min_cli_version":"2.0.0"}`))
	}))
	defer srv.Close()

	t.Setenv("BUTTONS_REGISTRY_URL", srv.URL)
	t.Setenv("BUTTONS_BAT_REGISTRY_KEY", "read")
	t.Setenv("BUTTONS_HOME", t.TempDir())
	t.Setenv("BUTTONS_NO_UPDATE", "")
	t.Setenv("CI", "true")

	maybeRunPassiveUpdate(&cobra.Command{Use: "status"})

	if got := hits.Load(); got != 0 {
		t.Fatalf("registry probe count = %d, want 0", got)
	}
}

func TestPassiveUpdatePlanRunsContentInsideBinaryThrottle(t *testing.T) {
	autoUpdate := true
	now := time.Unix(2000, 0)
	last := now.Add(-time.Minute).Unix()
	st := &settings.Settings{
		Defaults: settings.Defaults{
			AutoUpdate:          &autoUpdate,
			LastUpdateCheckUnix: &last,
		},
	}

	plan := passiveUpdatePlan(st, false, now)
	if !plan.run {
		t.Fatal("passive content update should run even when binary check is throttled")
	}
	if !plan.skipBinary {
		t.Fatal("binary update should stay throttled")
	}
	if plan.recordCheck {
		t.Fatal("content-only passive update should not refresh binary throttle timestamp")
	}
}
