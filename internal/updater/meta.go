package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type RegistryMeta struct {
	Service       string `json:"service"`
	MinCLIVersion string `json:"min_cli_version,omitempty"`
}

func FetchRegistryMeta(ctx context.Context, opts Options) (*RegistryMeta, error) {
	if opts.RegistryURL == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(opts.RegistryURL, "/")+"/v1/meta", nil)
	if err != nil {
		return nil, err
	}
	if opts.RegistryKey != "" {
		req.Header.Set("Authorization", "Bearer "+opts.RegistryKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient(opts).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry meta returned %d", resp.StatusCode)
	}
	var meta RegistryMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("decode registry meta: %w", err)
	}
	return &meta, nil
}

func ForceCLIUpdateRequired(ctx context.Context, opts Options) bool {
	meta, err := FetchRegistryMeta(ctx, opts)
	if err != nil || meta == nil || meta.MinCLIVersion == "" {
		return false
	}
	return CompareVersions(opts.CurrentVersion, meta.MinCLIVersion) < 0
}
