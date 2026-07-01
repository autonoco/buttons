package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const releasesAPI = "https://api.github.com/repos/autonoco/buttons/releases/latest"

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func (r *ghRelease) findAsset(name string) *ghAsset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

func checkBinary(ctx context.Context, opts Options) BinaryReport {
	current := NormalizeVersion(opts.CurrentVersion)
	report := BinaryReport{Current: current}
	if current == "" || current == "dev" {
		return report
	}
	if meta, err := FetchRegistryMeta(ctx, opts); err == nil && meta != nil && meta.MinCLIVersion != "" {
		report.MinRequired = NormalizeVersion(meta.MinCLIVersion)
		report.Forced = CompareVersions(current, report.MinRequired) < 0
	}
	latest, err := fetchLatestRelease(ctx, opts)
	if err != nil {
		report.Error = err.Error()
		return report
	}
	report.release = latest
	report.Latest = NormalizeVersion(latest.TagName)
	report.UpdateAvailable = report.Forced || CompareVersions(current, report.Latest) < 0
	if report.UpdateAvailable {
		if path, err := executablePath(opts); err == nil && isHomebrewManaged(path) {
			report.HomebrewManaged = true
		}
	}
	return report
}

func applyBinary(ctx context.Context, opts Options, report BinaryReport) (bool, BinaryReport) {
	if report.Error != "" || !report.UpdateAvailable {
		return false, report
	}

	execPath, err := executablePath(opts)
	if err != nil {
		report.Error = err.Error()
		return false, report
	}
	if isHomebrewManaged(execPath) {
		report.HomebrewManaged = true
		report.Error = "this binary is managed by Homebrew; run 'brew upgrade buttons' instead"
		return false, report
	}

	latest := report.release
	if latest == nil {
		report.Error = "latest release metadata unavailable"
		return false, report
	}
	archiveName := archiveNameForPlatform(report.Latest)
	archiveAsset := latest.findAsset(archiveName)
	if archiveAsset == nil {
		report.Error = fmt.Sprintf("no release asset found for %s (looked for %s)", runtime.GOOS+"/"+runtime.GOARCH, archiveName)
		return false, report
	}
	checksumsAsset := latest.findAsset("checksums.txt")
	if checksumsAsset == nil {
		report.Error = fmt.Sprintf("checksums.txt not found in release %s", report.Latest)
		return false, report
	}

	out := opts.output()
	fmt.Fprintf(out, "Updating buttons %s -> %s\n", report.Current, report.Latest)
	fmt.Fprintf(out, "  downloading %s\n", archiveName)
	archiveData, err := downloadAsset(ctx, opts, archiveAsset.URL)
	if err != nil {
		report.Error = fmt.Sprintf("failed to download archive: %v", err)
		return false, report
	}

	fmt.Fprintln(out, "  verifying checksum")
	checksumsData, err := downloadAsset(ctx, opts, checksumsAsset.URL)
	if err != nil {
		report.Error = fmt.Sprintf("failed to download checksums: %v", err)
		return false, report
	}
	if err := verifyChecksum(archiveData, archiveName, checksumsData); err != nil {
		report.Error = fmt.Sprintf("checksum verification failed: %v", err)
		return false, report
	}

	fmt.Fprintln(out, "  extracting binary")
	binaryData, err := extractBinaryFromTarGz(archiveData, "buttons")
	if err != nil {
		report.Error = fmt.Sprintf("failed to extract binary: %v", err)
		return false, report
	}

	fmt.Fprintf(out, "  replacing %s\n", execPath)
	if err := atomicReplace(execPath, binaryData); err != nil {
		report.Error = fmt.Sprintf("failed to replace binary: %v", err)
		return false, report
	}
	return true, report
}

func fetchLatestRelease(ctx context.Context, opts Options) (*ghRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient(opts).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release: %w", err)
	}
	return &release, nil
}

func downloadAsset(ctx context.Context, opts Options, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient(opts).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 50<<20))
}

func verifyChecksum(data []byte, name string, checksums []byte) error {
	lines := strings.Split(string(checksums), "\n")
	var expected string
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == name {
			expected = parts[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("no checksum entry for %s", name)
	}

	h := sha256.Sum256(data)
	actual := hex.EncodeToString(h[:])
	if actual != expected {
		return fmt.Errorf("expected %s, got %s", expected, actual)
	}
	return nil
}

func extractBinaryFromTarGz(data []byte, binaryName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("binary %q not found in archive", binaryName)
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == binaryName && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
}

func atomicReplace(path string, newData []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".buttons-update-*")
	if err != nil {
		return fmt.Errorf("cannot create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(newData); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(0o700); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	oldPath := path + ".old"
	_ = os.Remove(oldPath)
	if err := os.Rename(path, oldPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("cannot move current binary: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Rename(oldPath, path)
		return fmt.Errorf("cannot install new binary: %w", err)
	}
	_ = os.Remove(oldPath)
	return nil
}

func executablePath(opts Options) (string, error) {
	if opts.ExecutablePath != "" {
		return filepath.EvalSymlinks(opts.ExecutablePath)
	}
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve executable path: %w", err)
	}
	return execPath, nil
}

func archiveNameForPlatform(version string) string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	return fmt.Sprintf("buttons_%s_%s_%s.tar.gz", version, runtime.GOOS, arch)
}

func isHomebrewManaged(path string) bool {
	return strings.Contains(path, "/Cellar/") ||
		strings.Contains(path, "/opt/homebrew/") ||
		strings.Contains(path, "/usr/local/Cellar/")
}
