package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/store"
)

func Check(ctx context.Context, opts Options) (*Report, error) {
	report := &Report{}
	if !opts.SkipBinary {
		bin := checkBinary(ctx, opts)
		report.Binary = &bin
		if bin.UpdateAvailable {
			report.UpdateAvailable = true
		}
	}
	if !opts.SkipContent {
		buttons, err := CheckContent(ctx, opts)
		if err != nil {
			return nil, err
		}
		report.Buttons = buttons
		for _, b := range buttons {
			if b.UpdateAvailable {
				report.UpdateAvailable = true
				break
			}
		}
	}
	return report, nil
}

func Apply(ctx context.Context, opts Options) (*Result, error) {
	report, err := Check(ctx, opts)
	if err != nil {
		return nil, err
	}
	result := &Result{Report: *report}

	if report.Binary != nil && report.Binary.UpdateAvailable {
		updated, bin := applyBinary(ctx, opts)
		if updated {
			bin.UpdateAvailable = false
		}
		result.Binary = &bin
		result.UpdatedBinary = updated
	}

	if !opts.SkipContent {
		for i := range result.Buttons {
			if !result.Buttons[i].UpdateAvailable || result.Buttons[i].Error != "" {
				continue
			}
			if err := applyContentUpdate(ctx, opts, &result.Buttons[i]); err != nil {
				result.Buttons[i].Error = err.Error()
				continue
			}
			result.Buttons[i].Updated = true
			result.Buttons[i].UpdateAvailable = false
			result.UpdatedButtons = append(result.UpdatedButtons, result.Buttons[i].Name)
		}
	}

	result.UpdateAvailable = false
	if result.Binary != nil && result.Binary.UpdateAvailable && !result.UpdatedBinary && result.Binary.Error == "" {
		result.UpdateAvailable = true
	}
	for _, b := range result.Buttons {
		if b.UpdateAvailable && !b.Updated && b.Error == "" {
			result.UpdateAvailable = true
			break
		}
	}
	return result, nil
}

func CheckContent(ctx context.Context, opts Options) ([]ButtonReport, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	installed, err := installedButtons()
	if err != nil {
		return nil, err
	}
	reports := make([]ButtonReport, 0, len(installed))
	for _, b := range installed {
		rep := checkOneButton(ctx, opts, b)
		if rep.Source != "" {
			reports = append(reports, rep)
		}
	}
	return reports, nil
}

func checkOneButton(ctx context.Context, opts Options, b button.Button) ButtonReport {
	rep := ButtonReport{
		Name:           b.Name,
		SourceName:     b.SourceName,
		Source:         b.Source,
		CurrentVersion: b.Version,
		CurrentHash:    b.ContentHash,
	}
	if b.Source == "" {
		return rep
	}
	if b.SourceName == "" {
		rep.Error = "installed button is missing source_name"
		return rep
	}
	modified, err := locallyModified(b)
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	if modified {
		rep.Skipped = true
		rep.SkipReason = "local files changed since install"
		return rep
	}
	src, err := sourceFor(b.Source, opts)
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	select {
	case <-ctx.Done():
		rep.Error = ctx.Err().Error()
		return rep
	default:
	}
	bundle, err := src.Fetch(b.SourceName, "")
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	rep.LatestVersion = bundle.Version
	rep.LatestHash = bundle.SHA256
	rep.UpdateAvailable = contentUpdateAvailable(rep)
	return rep
}

func contentUpdateAvailable(rep ButtonReport) bool {
	if rep.LatestVersion != "" && CompareVersions(rep.CurrentVersion, rep.LatestVersion) < 0 {
		return true
	}
	if rep.CurrentVersion == "" && rep.LatestHash != "" && rep.CurrentHash != "" {
		return rep.CurrentHash != rep.LatestHash
	}
	return false
}

func applyContentUpdate(ctx context.Context, opts Options, rep *ButtonReport) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	src, err := sourceFor(rep.Source, opts)
	if err != nil {
		return err
	}
	if _, err := store.InstallSpec(src, rep.SourceName, rep.Source); err != nil {
		return err
	}
	return nil
}

func locallyModified(b button.Button) (bool, error) {
	dir, err := config.ButtonDir(button.Slugify(b.Name))
	if err != nil {
		return false, err
	}
	state, err := store.ReadInstallState(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read install state for %s: %w", b.Name, err)
	}
	if state.ContentHash != "" && b.ContentHash != "" && state.ContentHash != b.ContentHash {
		return false, nil
	}
	current, err := store.HashInstalledDir(dir)
	if err != nil {
		return false, fmt.Errorf("hash installed files for %s: %w", b.Name, err)
	}
	return state.InstalledHash != "" && current != state.InstalledHash, nil
}

func sourceFor(sourceRef string, opts Options) (store.Source, error) {
	if local, ok := strings.CutPrefix(sourceRef, "local:"); ok {
		return &store.LocalSource{Root: local}, nil
	}
	if strings.HasPrefix(sourceRef, "http://") || strings.HasPrefix(sourceRef, "https://") {
		if opts.RegistryKey == "" {
			return nil, fmt.Errorf("registry key not set")
		}
		return &store.HTTPSource{
			BaseURL: sourceRef,
			Key:     opts.RegistryKey,
			Client:  httpClient(opts),
		}, nil
	}
	return nil, fmt.Errorf("unsupported update source %q", sourceRef)
}

func httpClient(opts Options) *http.Client {
	if opts.Client != nil {
		return opts.Client
	}
	return http.DefaultClient
}

func installedButtons() ([]button.Button, error) {
	dir, err := config.ButtonsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	buttons := make([]button.Button, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), "button.json")
		data, err := os.ReadFile(path) // #nosec G304 -- path is under ButtonsDir plus an enumerated entry.
		if err != nil {
			continue
		}
		var b button.Button
		if err := json.Unmarshal(data, &b); err != nil {
			continue
		}
		buttons = append(buttons, b)
	}
	return buttons, nil
}
