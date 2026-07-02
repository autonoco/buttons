package updater

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/manifest"
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
		updated, bin := applyBinary(ctx, opts, *report.Binary)
		if updated {
			bin.UpdateAvailable = false
		}
		result.Binary = &bin
		result.UpdatedBinary = updated
	}

	if !opts.SkipContent {
		for i := range result.Buttons {
			if !result.Buttons[i].UpdateAvailable || result.Buttons[i].Error != "" || result.Buttons[i].Pinned {
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

	m, err := manifest.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lock, err := manifest.LoadLockfile()
	if err != nil {
		return nil, err
	}
	src, err := sourceForManifest(ctx, opts)
	if err != nil {
		return reportsWithSourceError(m, lock, err), nil
	}

	deps := contentDependencies(m, lock)
	reports := make([]ButtonReport, 0, len(deps))
	for _, dep := range deps {
		reports = append(reports, checkOneDependency(ctx, src, dep.name, dep.requested, lock))
	}
	return reports, nil
}

type contentDependency struct {
	name      string
	requested string
}

func contentDependencies(m *manifest.Manifest, lock *manifest.Lockfile) []contentDependency {
	deps := make(map[string]string, len(m.Dependencies)+len(lock.Dependencies))
	for name, requested := range m.Dependencies {
		deps[name] = requested
	}
	for name, entry := range lock.Dependencies {
		if _, ok := deps[name]; ok {
			continue
		}
		deps[name] = entry.Requested
	}
	names := make([]string, 0, len(deps))
	for name := range deps {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]contentDependency, 0, len(names))
	for _, name := range names {
		out = append(out, contentDependency{name: name, requested: deps[name]})
	}
	return out
}

func reportsWithSourceError(m *manifest.Manifest, lock *manifest.Lockfile, err error) []ButtonReport {
	deps := contentDependencies(m, lock)
	reports := make([]ButtonReport, 0, len(deps))
	for _, dep := range deps {
		entry := lock.Dependencies[dep.name]
		kind, installedName := effectiveDependencyIdentity(dep.name, entry)
		reports = append(reports, ButtonReport{
			Name:           installedName,
			Kind:           kind,
			PackageName:    dep.name,
			Requested:      dep.requested,
			CurrentVersion: entry.Version,
			CurrentHash:    entry.ContentHash,
			Pinned:         !manifest.IsFloating(dep.requested),
			Error:          err.Error(),
		})
	}
	return reports
}

func checkOneDependency(ctx context.Context, src store.Source, name, requested string, lock *manifest.Lockfile) ButtonReport {
	entry, locked := lock.Dependencies[name]
	kind, installedName := effectiveDependencyIdentity(name, entry)
	rep := ButtonReport{
		Name:           installedName,
		Kind:           kind,
		PackageName:    name,
		Requested:      requested,
		CurrentVersion: entry.Version,
		CurrentHash:    entry.ContentHash,
		Pinned:         !manifest.IsFloating(requested),
	}
	if !locked {
		rep.UpdateAvailable = true
	}

	modified, err := locallyModified(entry)
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	if modified {
		rep.Skipped = true
		rep.SkipReason = "local files changed since install"
		return rep
	}

	select {
	case <-ctx.Done():
		rep.Error = ctx.Err().Error()
		return rep
	default:
	}
	ref, err := store.Resolve(src, name, "")
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	if ref.Kind != "" {
		rep.Kind = ref.Kind
	}
	rep.LatestVersion = ref.Version
	rep.LatestHash = ref.SHA256
	if manifest.IsFloating(requested) {
		rep.UpdateAvailable = !locked || contentUpdateAvailable(rep)
	}
	return rep
}

func effectiveDependencyIdentity(packageName string, entry manifest.LockEntry) (kind, installedName string) {
	kind = entry.Kind
	if kind == "" {
		kind = "button"
	}
	installedName = entry.InstalledName
	if installedName == "" {
		installedName = localNameFromPackage(packageName)
	}
	return kind, installedName
}

func contentUpdateAvailable(rep ButtonReport) bool {
	if rep.LatestVersion != "" && CompareVersions(rep.CurrentVersion, rep.LatestVersion) < 0 {
		return true
	}
	if rep.CurrentVersion == "" && rep.LatestVersion != "" {
		return true
	}
	return false
}

func applyContentUpdate(ctx context.Context, opts Options, rep *ButtonReport) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if rep.Pinned {
		return nil
	}
	src, err := sourceForManifest(ctx, opts)
	if err != nil {
		return err
	}
	prior, err := manifest.LoadLockfile()
	if err != nil {
		return err
	}
	m := &manifest.Manifest{SchemaVersion: manifest.SchemaVersion, Dependencies: map[string]string{rep.PackageName: rep.Requested}}
	_, next, err := store.InstallManifest(src, m, prior, store.InstallOptions{RefreshFloating: true})
	if err != nil {
		return err
	}
	for name, entry := range next.Dependencies {
		prior.Dependencies[name] = entry
	}
	return manifest.SaveLockfile(prior)
}

func locallyModified(entry manifest.LockEntry) (bool, error) {
	if entry.InstalledName == "" {
		return false, nil
	}
	var dir string
	var err error
	switch entry.Kind {
	case "drawer":
		dir, err = config.DrawerDir(button.Slugify(entry.InstalledName))
	default:
		dir, err = config.ButtonDir(button.Slugify(entry.InstalledName))
	}
	if err != nil {
		return false, err
	}
	state, err := store.ReadInstallState(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read install state for %s: %w", entry.InstalledName, err)
	}
	if state.ContentHash != "" && entry.ContentHash != "" && state.ContentHash != entry.ContentHash {
		return false, nil
	}
	current, err := store.HashInstalledDir(dir)
	if err != nil {
		return false, fmt.Errorf("hash installed files for %s: %w", entry.InstalledName, err)
	}
	return state.InstalledHash != "" && current != state.InstalledHash, nil
}

func sourceForManifest(ctx context.Context, opts Options) (store.Source, error) {
	if opts.RegistryURL == "" {
		return nil, fmt.Errorf("registry URL not set")
	}
	if opts.RegistryKey == "" {
		return nil, fmt.Errorf("registry key not set")
	}
	return &store.HTTPSource{
		BaseURL: opts.RegistryURL,
		Key:     opts.RegistryKey,
		Client:  httpClient(opts),
		Context: ctx,
	}, nil
}

func httpClient(opts Options) *http.Client {
	if opts.Client != nil {
		return opts.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func localNameFromPackage(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' {
			return name[i+1:]
		}
	}
	return name
}
