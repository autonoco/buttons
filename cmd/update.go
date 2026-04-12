package cmd

import (
	"archive/tar"
	"compress/gzip"
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

	"github.com/autonoco/buttons/internal/config"
	"github.com/spf13/cobra"
)

const (
	releasesAPI = "https://api.github.com/repos/autonoco/buttons/releases/latest"
	repoOwner   = "autonoco"
	repoName    = "buttons"
)

var updateCheck bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update buttons to the latest version",
	Long: `Check for and install the latest version of buttons from GitHub Releases.

Downloads the correct archive for your OS and architecture, verifies
the SHA256 checksum, and atomically replaces the running binary.

Examples:
  buttons update              # download and install the latest version
  buttons update --check      # just check, don't install
  buttons update --json       # structured output`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		current := normalizeVersion(version)

		latest, err := fetchLatestRelease()
		if err != nil {
			if jsonOutput {
				_ = config.WriteJSONError("INTERNAL_ERROR", fmt.Sprintf("failed to check for updates: %v", err))
				return errSilent
			}
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		latestVersion := normalizeVersion(latest.TagName)
		upToDate := current == latestVersion

		if upToDate {
			if jsonOutput {
				return config.WriteJSON(map[string]any{
					"current":    current,
					"latest":     latestVersion,
					"up_to_date": true,
				})
			}
			fmt.Fprintf(os.Stderr, "Already up to date (%s)\n", current)
			return nil
		}

		if updateCheck {
			if jsonOutput {
				return config.WriteJSON(map[string]any{
					"current":    current,
					"latest":     latestVersion,
					"up_to_date": false,
				})
			}
			fmt.Fprintf(os.Stderr, "Update available: %s → %s\n", current, latestVersion)
			fmt.Fprintf(os.Stderr, "Run 'buttons update' to install.\n")
			return nil
		}

		// Resolve the path to the currently running binary.
		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot determine executable path: %w", err)
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return fmt.Errorf("cannot resolve executable path: %w", err)
		}

		if isHomebrewManaged(execPath) {
			if jsonOutput {
				_ = config.WriteJSONError("VALIDATION_ERROR", "this binary is managed by Homebrew — run 'brew upgrade buttons' instead")
				return errSilent
			}
			return fmt.Errorf("this binary is managed by Homebrew — run 'brew upgrade buttons' instead")
		}

		// Find the right archive for this platform.
		archiveName := archiveNameForPlatform(latestVersion)
		archiveAsset := latest.findAsset(archiveName)
		if archiveAsset == nil {
			return fmt.Errorf("no release asset found for %s (looked for %s)", runtime.GOOS+"/"+runtime.GOARCH, archiveName)
		}
		checksumsAsset := latest.findAsset("checksums.txt")
		if checksumsAsset == nil {
			return fmt.Errorf("checksums.txt not found in release %s", latest.TagName)
		}

		fmt.Fprintf(os.Stderr, "Updating %s → %s\n", current, latestVersion)

		// Download the archive.
		fmt.Fprintf(os.Stderr, "  downloading %s\n", archiveName)
		archiveData, err := downloadAsset(archiveAsset.URL)
		if err != nil {
			return fmt.Errorf("failed to download archive: %w", err)
		}

		// Download and verify checksum.
		fmt.Fprintf(os.Stderr, "  verifying checksum\n")
		checksumsData, err := downloadAsset(checksumsAsset.URL)
		if err != nil {
			return fmt.Errorf("failed to download checksums: %w", err)
		}
		if err := verifyChecksum(archiveData, archiveName, checksumsData); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}

		// Extract the binary from the tarball.
		fmt.Fprintf(os.Stderr, "  extracting binary\n")
		binaryData, err := extractBinaryFromTarGz(archiveData, "buttons")
		if err != nil {
			return fmt.Errorf("failed to extract binary: %w", err)
		}

		// Atomically swap the binary.
		fmt.Fprintf(os.Stderr, "  replacing %s\n", execPath)
		if err := atomicReplace(execPath, binaryData); err != nil {
			return fmt.Errorf("failed to replace binary: %w", err)
		}

		if jsonOutput {
			return config.WriteJSON(map[string]any{
				"current":    current,
				"latest":     latestVersion,
				"up_to_date": true,
				"updated":    true,
				"path":       execPath,
			})
		}
		fmt.Fprintf(os.Stderr, "Updated to %s\n", latestVersion)
		return nil
	},
}

// ghRelease is a minimal representation of a GitHub API release.
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

func fetchLatestRelease() (*ghRelease, error) {
	req, err := http.NewRequest("GET", releasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release: %w", err)
	}
	return &release, nil
}

func downloadAsset(url string) ([]byte, error) {
	resp, err := http.Get(url) // #nosec G107 -- URL comes from GitHub API response, not user input
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download returned %d", resp.StatusCode)
	}

	// Cap at 50 MB to prevent abuse.
	return io.ReadAll(io.LimitReader(resp.Body, 50<<20))
}

func verifyChecksum(data []byte, name string, checksums []byte) error {
	// Parse checksums.txt: each line is "<hash>  <filename>"
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
	gz, err := gzip.NewReader(strings.NewReader(string(data)))
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

// atomicReplace writes newData to the path of the current binary
// using a temp-file + rename pattern. On most filesystems rename
// is atomic, so there's no window where the binary is missing.
func atomicReplace(path string, newData []byte) error {
	dir := filepath.Dir(path)

	// Write to a temp file in the same directory (same filesystem
	// required for atomic rename).
	tmp, err := os.CreateTemp(dir, ".buttons-update-*")
	if err != nil {
		return fmt.Errorf("cannot create temp file in %s (is the directory writable?): %w", dir, err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(newData); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	// #nosec G302 -- the replacement binary needs exec bit to run.
	if err := tmp.Chmod(0755); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	// Rename current → .old, temp → current, remove .old.
	oldPath := path + ".old"
	_ = os.Remove(oldPath) // clean up any leftover from a previous update
	if err := os.Rename(path, oldPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("cannot move current binary: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// Try to restore the old binary.
		_ = os.Rename(oldPath, path)
		return fmt.Errorf("cannot install new binary: %w", err)
	}
	_ = os.Remove(oldPath)
	return nil
}

func archiveNameForPlatform(version string) string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	return fmt.Sprintf("buttons_%s_%s_%s.tar.gz", version, os, arch)
}

func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSuffix(v, "-dirty"), "v")
}

func isHomebrewManaged(path string) bool {
	return strings.Contains(path, "/Cellar/") ||
		strings.Contains(path, "/opt/homebrew/") ||
		strings.Contains(path, "/usr/local/Cellar/")
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheck, "check", false, "check for updates without installing")
	rootCmd.AddCommand(updateCmd)
}
