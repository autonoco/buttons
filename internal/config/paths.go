package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const dataDirName = ".buttons"

func DataDir() (string, error) {
	if override := os.Getenv("BUTTONS_HOME"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, dataDirName), nil
}

func ButtonsDir() (string, error) {
	base, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "buttons"), nil
}

// ButtonDir returns the path to a specific button's folder.
//
// Callers should pass a name that has already been validated/slugified by the
// button service. As defense-in-depth, this function rejects any resolved path
// that escapes the buttons data directory — so a future caller that forgets
// to sanitize cannot accidentally cause a path traversal.
func ButtonDir(name string) (string, error) {
	dir, err := ButtonsDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	if !strings.HasPrefix(p, dir+string(filepath.Separator)) {
		return "", fmt.Errorf("button name resolves outside data directory: %q", name)
	}
	return p, nil
}

func DrawersDir() (string, error) {
	base, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "drawers"), nil
}

func EnsureDataDir() error {
	base, err := DataDir()
	if err != nil {
		return err
	}

	dirs := []string{
		base,
		filepath.Join(base, "buttons"),
		filepath.Join(base, "drawers"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("could not create directory %s: %w", dir, err)
		}
	}

	return nil
}
