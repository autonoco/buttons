package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const InstallStateFile = ".buttons-install-state.json"

type InstallState struct {
	ContentHash   string `json:"content_hash"`
	InstalledHash string `json:"installed_hash"`
}

func writeInstallState(dir, contentHash string) error {
	installedHash, err := HashInstalledDir(dir)
	if err != nil {
		return err
	}
	state := InstallState{ContentHash: contentHash, InstalledHash: installedHash}
	data, err := json.MarshalIndent(&state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal install state: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, InstallStateFile), data, 0o600)
}

func ReadInstallState(dir string) (*InstallState, error) {
	data, err := os.ReadFile(filepath.Join(dir, InstallStateFile)) // #nosec G304 -- dir is caller-controlled under ButtonsDir.
	if err != nil {
		return nil, err
	}
	var state InstallState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func HashInstalledDir(dir string) (string, error) {
	files := map[string][]byte{}
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == "pressed" || strings.HasPrefix(rel, "pressed/") {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == InstallStateFile {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G304 -- path comes from WalkDir under dir.
		if err != nil {
			return err
		}
		files[rel] = data
		return nil
	}); err != nil {
		return "", err
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(files[name]))
		h.Write(files[name])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
