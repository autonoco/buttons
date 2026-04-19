package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const dataDirName = ".buttons"

// DataDir returns the active buttons data directory. Priority:
//
//  1. BUTTONS_HOME env var (explicit override)
//  2. .buttons/ in the current directory or any parent (project-local)
//  3. ~/.buttons/ (global fallback)
//
// Project-local discovery walks up from os.Getwd() looking for a
// .buttons/ directory, the same way git walks up looking for .git/.
// Use `buttons init` to create a project-local .buttons/ folder.
func DataDir() (string, error) {
	if override := os.Getenv("BUTTONS_HOME"); override != "" {
		return override, nil
	}
	if projectDir, err := findProjectDir(); err == nil {
		return projectDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, dataDirName), nil
}

// findProjectDir walks up from the current working directory looking
// for a .buttons/ directory. Returns the path if found, or an error
// if none exists between CWD and the filesystem root.
func findProjectDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, dataDirName)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no %s directory found", dataDirName)
}

// IsProjectLocal returns true if the active data directory is a
// project-local .buttons/ (as opposed to the global ~/.buttons/).
func IsProjectLocal() bool {
	if os.Getenv("BUTTONS_HOME") != "" {
		return false
	}
	_, err := findProjectDir()
	return err == nil
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

// DrawerDir returns the path to a specific drawer's folder. Mirrors
// ButtonDir's path-escape rejection: even if a caller forgets to
// sanitize the drawer name, we won't let it traverse outside the
// drawers data directory.
func DrawerDir(name string) (string, error) {
	dir, err := DrawersDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	if !strings.HasPrefix(p, dir+string(filepath.Separator)) {
		return "", fmt.Errorf("drawer name resolves outside data directory: %q", name)
	}
	return p, nil
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
