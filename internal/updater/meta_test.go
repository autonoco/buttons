package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForceCLIUpdateRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer read" {
			t.Fatalf("authorization = %q, want Bearer read", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"service":"buttons-registry","min_cli_version":"1.2.0"}`))
	}))
	defer srv.Close()

	opts := Options{CurrentVersion: "1.1.9", RegistryURL: srv.URL, RegistryKey: "read", Client: srv.Client()}
	if !ForceCLIUpdateRequired(context.Background(), opts) {
		t.Fatal("expected forced update when current is below registry minimum")
	}

	opts.CurrentVersion = "1.2.0"
	if ForceCLIUpdateRequired(context.Background(), opts) {
		t.Fatal("did not expect forced update at the registry minimum")
	}
}
