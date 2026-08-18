package cmd

import (
	"context"
	"os"
	"testing"
)

func TestRegistryWriteAuthorizationSeparatesMachineAndHumanCredentials(t *testing.T) {
	t.Setenv("BUTTONS_BAT_REGISTRY_WRITE_KEY", "machine-key")
	calls := 0
	key, err := resolveRegistryWriteKey(context.Background(), "https://registry.example", func(context.Context, string) (string, error) {
		calls++
		return "human-capability", nil
	})
	if err != nil || key != "machine-key" || calls != 0 {
		t.Fatalf("machine authorization = %q, %v, calls=%d", key, err, calls)
	}

	if err := os.Unsetenv("BUTTONS_BAT_REGISTRY_WRITE_KEY"); err != nil {
		t.Fatal(err)
	}
	key, err = resolveRegistryWriteKey(context.Background(), "https://registry.example", func(_ context.Context, registry string) (string, error) {
		calls++
		if registry != "https://registry.example" {
			t.Fatalf("wrong registry %q", registry)
		}
		return "human-capability", nil
	})
	if err != nil || key != "human-capability" || calls != 1 {
		t.Fatalf("human authorization = %q, %v, calls=%d", key, err, calls)
	}
}
