package cmd

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

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
