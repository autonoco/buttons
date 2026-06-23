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
	"github.com/autonoco/buttons/internal/store"
	"github.com/spf13/cobra"
)

const (
	releasesAPI = "https://api.github.com/repos/autonoco/buttons/releases/latest"
	repoOwner   = "autonoco"
	repoName    = "buttons"
)

var (
	updateCheck   bool
	updateBinary  bool
	updateContent bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update buttons — the CLI binary and installed buttons",
	Long: `Bring everything current: the buttons CLI binary (from GitHub Releases)
and the content of any installed buttons (re-fetched from the source each
was installed from, when it has drifted).

By default both are updated. Scope with --binary or --content. The binary
download verifies its SHA256 and atomically replaces the running binary;
button content is re-verified against its install-time content hash and only
rewritten when it changed.

Examples:
  buttons update              # binary + installed buttons
  buttons update --check      # report what would change, install nothing
  buttons update --binary     # only the CLI binary
  buttons update --content    # only installed buttons
  buttons update --json       # structured output`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default (neither flag) = do both.
		doBinary := updateBinary || !updateContent
		doContent := updateContent || !updateBinary

		out := map[string]any{}

		if doBinary {
			bin, err := runBinaryUpdate(updateCheck)
			if err != nil {
				if jsonOutput {
					_ = config.WriteJSONError("INTERNAL_ERROR", err.Error())
					return errSilent
				}
				return err
			}
			out["binary"] = bin
		}

		if doContent {
			content, err := store.UpdateInstalled(store.DefaultSourceResolver, updateCheck)
			if err != nil {
				if jsonOutput {
					_ = config.WriteJSONError("INTERNAL_ERROR", err.Error())
					return errSilent
				}
				return err
			}
			if !jsonOutput {
				printContentSummary(content, updateCheck)
			}
			out["content"] = content
		}

		if jsonOutput {
			return config.WriteJSON(out)
		}
		return nil
	},
}

// binaryUpdate is the structured result of the self-update half.
type binaryUpdate struct {
	Current  string `json:"current"`
	Latest   string `json:"latest"`
	UpToDate bool   `json:"up_to_date"`
	Updated  bool   `json:"updated,omitempty"`
	Path     string `json:"path,omitempty"`
	Skipped  string `json:"skipped,omitempty"` // reason the binary was not replaced
}

// runBinaryUpdate checks GitHub Releases and, unless check is set, downloads,
// verifies, and atomically swaps the running binary. A Homebrew-managed binary
// is reported as skipped (not a hard error) so a combined run can still update
// button content.
func runBinaryUpdate(check bool) (*binaryUpdate, error) {
	current := normalizeVersion(version)

	latest, err := fetchLatestRelease()
	if err != nil {
		return nil, fmt.Errorf("failed to check for updates: %w", err)
	}
	latestVersion := normalizeVersion(latest.TagName)
	bu := &binaryUpdate{Current: current, Latest: latestVersion, UpToDate: current == latestVersion}

	if bu.UpToDate {
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "Binary already up to date (%s)\n", current)
		}
		return bu, nil
	}

	if check {
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "Binary update available: %s → %s (run 'buttons update')\n", current, latestVersion)
		}
		return bu, nil
	}

	// Resolve the path to the currently running binary.
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("cannot determine executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve executable path: %w", err)
	}

	if isHomebrewManaged(execPath) {
		bu.Skipped = "homebrew-managed"
		if !jsonOutput {
			fmt.Fprintf(os.Stderr, "Binary is managed by Homebrew — run 'brew upgrade buttons' (skipping binary)\n")
		}
		return bu, nil
	}

	// Find the right archive for this platform.
	archiveName := archiveNameForPlatform(latestVersion)
	archiveAsset := latest.findAsset(archiveName)
	if archiveAsset == nil {
		return nil, fmt.Errorf("no release asset found for %s (looked for %s)", runtime.GOOS+"/"+runtime.GOARCH, archiveName)
	}
	checksumsAsset := latest.findAsset("checksums.txt")
	if checksumsAsset == nil {
		return nil, fmt.Errorf("checksums.txt not found in release %s", latest.TagName)
	}

	fmt.Fprintf(os.Stderr, "Updating binary %s → %s\n", current, latestVersion)

	fmt.Fprintf(os.Stderr, "  downloading %s\n", archiveName)
	archiveData, err := downloadAsset(archiveAsset.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to download archive: %w", err)
	}

	fmt.Fprintf(os.Stderr, "  verifying checksum\n")
	checksumsData, err := downloadAsset(checksumsAsset.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to download checksums: %w", err)
	}
	if err := verifyChecksum(archiveData, archiveName, checksumsData); err != nil {
		return nil, fmt.Errorf("checksum verification failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "  extracting binary\n")
	binaryData, err := extractBinaryFromTarGz(archiveData, "buttons")
	if err != nil {
		return nil, fmt.Errorf("failed to extract binary: %w", err)
	}

	fmt.Fprintf(os.Stderr, "  replacing %s\n", execPath)
	if err := atomicReplace(execPath, binaryData); err != nil {
		return nil, fmt.Errorf("failed to replace binary: %w", err)
	}

	bu.Updated = true
	bu.Path = execPath
	if !jsonOutput {
		fmt.Fprintf(os.Stderr, "Updated binary to %s\n", latestVersion)
	}
	return bu, nil
}

// printContentSummary renders the installed-button half for humans.
func printContentSummary(res *store.UpdateResult, check bool) {
	counts := res.Counts()
	if len(res.Buttons) == 0 {
		fmt.Fprintln(os.Stderr, "No installed buttons to update.")
		return
	}
	for _, b := range res.Buttons {
		switch b.Action {
		case "updated":
			fmt.Fprintf(os.Stderr, "  ✓ %s %s → %s\n", b.Name, b.From, b.To)
		case "available":
			fmt.Fprintf(os.Stderr, "  ↑ %s %s → %s (available)\n", b.Name, b.From, b.To)
		case "error":
			fmt.Fprintf(os.Stderr, "  ✗ %s: %s\n", b.Name, b.Reason)
		}
	}
	verb := "updated"
	if check {
		verb = "available"
	}
	fmt.Fprintf(os.Stderr, "Buttons: %d %s, %d unchanged, %d skipped, %d errors\n",
		counts["updated"]+counts["available"], verb, counts["unchanged"], counts["skipped"], counts["error"])
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
	updateCmd.Flags().BoolVar(&updateCheck, "check", false, "report what would change without installing")
	updateCmd.Flags().BoolVar(&updateBinary, "binary", false, "update only the CLI binary")
	updateCmd.Flags().BoolVar(&updateContent, "content", false, "update only installed buttons")
	rootCmd.AddCommand(updateCmd)
}
