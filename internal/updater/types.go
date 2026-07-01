package updater

import (
	"io"
	"net/http"
)

type Options struct {
	CurrentVersion string
	RegistryURL    string
	RegistryKey    string
	Client         *http.Client
	Writer         io.Writer
	ExecutablePath string
	SkipBinary     bool
	SkipContent    bool
}

type Report struct {
	Binary          *BinaryReport  `json:"binary,omitempty"`
	Buttons         []ButtonReport `json:"buttons"`
	UpdateAvailable bool           `json:"update_available"`
}

type BinaryReport struct {
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	MinRequired     string `json:"min_required,omitempty"`
	Forced          bool   `json:"forced,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	HomebrewManaged bool   `json:"homebrew_managed,omitempty"`
	Error           string `json:"error,omitempty"`
	release         *ghRelease
}

type ButtonReport struct {
	Name            string `json:"name"`
	PackageName     string `json:"package_name"`
	Requested       string `json:"requested,omitempty"`
	Pinned          bool   `json:"pinned,omitempty"`
	CurrentVersion  string `json:"current_version,omitempty"`
	LatestVersion   string `json:"latest_version,omitempty"`
	CurrentHash     string `json:"current_hash,omitempty"`
	LatestHash      string `json:"latest_hash,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	Skipped         bool   `json:"skipped,omitempty"`
	SkipReason      string `json:"skip_reason,omitempty"`
	Updated         bool   `json:"updated,omitempty"`
	Error           string `json:"error,omitempty"`
}

type Result struct {
	Report
	UpdatedBinary  bool     `json:"updated_binary"`
	UpdatedButtons []string `json:"updated_buttons"`
}

func (o Options) output() io.Writer {
	if o.Writer != nil {
		return o.Writer
	}
	return io.Discard
}
