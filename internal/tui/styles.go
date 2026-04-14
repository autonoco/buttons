// Package tui implements the `buttons board` TUI. Styles here are the
// terminal translation of the Buttons identity spec:
//
//   - Ink       #0A0A0A  — wordmark, text
//   - Indicator #FF5A1F  — ACTIVE STATE ONLY, never decorative
//   - Paper     #F5F5F2  — default surface (terminal bg, not set by us)
//   - Aluminum  #C5C5C0  — panels, dividers
//   - Dust      #6E6E68  — secondary text
//
// Hard rule from the identity spec: orange (Indicator) is only used to
// signal an active/running state. If you find yourself reaching for it
// for decoration, don't.
package tui

import (
	"os"

	"charm.land/lipgloss/v2"
)

// Brand palette. Exact hex values from the identity spec.
const (
	hexInk       = "#0A0A0A"
	hexIndicator = "#FF5A1F"
	hexPaper     = "#F5F5F2"
	hexAluminum  = "#C5C5C0"
	hexDust      = "#6E6E68"
)

// Styles bundles every Lip Gloss style the board uses. Built once at
// TUI startup via BuildStyles(), after terminal background detection,
// so light-vs-dark color choices are resolved eagerly rather than on
// every render.
type Styles struct {
	Wordmark  lipgloss.Style
	Label     lipgloss.Style
	Divider   lipgloss.Style
	Secondary lipgloss.Style
	Muted     lipgloss.Style
	Indicator lipgloss.Style

	ButtonName         lipgloss.Style
	ButtonNameSelected lipgloss.Style
	ButtonNameActive   lipgloss.Style

	BadgeActive           lipgloss.Style
	ActionPrimary         lipgloss.Style
	ActionSecondary       lipgloss.Style
	ActionPrimaryDisabled lipgloss.Style

	PinnedIdle     lipgloss.Style
	PinnedSelected lipgloss.Style
	PinnedActive   lipgloss.Style
}

// BuildStyles detects the terminal's background (light vs dark) and
// returns a Styles value with adaptive colors resolved. Ink/Paper swap
// roles on dark terminals — the wordmark is the "foreground" in either
// theme, it just changes hex code.
func BuildStyles() Styles {
	// Background detection has to query the terminal; if it fails for
	// any reason (non-TTY, unsupported terminal), default to dark since
	// that's the majority case for developer terminals.
	hasDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	ld := lipgloss.LightDark(hasDark)

	colorPrimary := ld(lipgloss.Color(hexInk), lipgloss.Color(hexPaper))
	colorSecondary := ld(lipgloss.Color(hexDust), lipgloss.Color("#A8A8A0"))
	colorMuted := ld(lipgloss.Color(hexAluminum), lipgloss.Color("#3A3A38"))
	colorIndicator := lipgloss.Color(hexIndicator)
	colorOnIndicator := lipgloss.Color(hexPaper)
	// Action-primary always reads the same: dark fill, light text. On a
	// light terminal that's ink/paper; on a dark terminal it's still a
	// near-black block against the terminal bg (slight contrast, but the
	// bordered style reads fine).
	colorActionFill := lipgloss.Color(hexInk)
	colorActionText := lipgloss.Color(hexPaper)

	return Styles{
		Wordmark:  lipgloss.NewStyle().Foreground(colorPrimary),
		Label:     lipgloss.NewStyle().Foreground(colorSecondary),
		Divider:   lipgloss.NewStyle().Foreground(colorMuted),
		Secondary: lipgloss.NewStyle().Foreground(colorSecondary),
		Muted:     lipgloss.NewStyle().Foreground(colorMuted),
		Indicator: lipgloss.NewStyle().Foreground(colorIndicator),

		ButtonName: lipgloss.NewStyle().Foreground(colorPrimary),
		// Bold makes the selected row pop without reaching for orange
		// (which is reserved for active/running, not mere selection).
		ButtonNameSelected: lipgloss.NewStyle().Foreground(colorPrimary).Bold(true),
		ButtonNameActive:   lipgloss.NewStyle().Foreground(colorIndicator).Bold(true),

		BadgeActive: lipgloss.NewStyle().
			Foreground(colorOnIndicator).
			Background(colorIndicator).
			Padding(0, 1).
			Bold(true),

		ActionPrimary: lipgloss.NewStyle().
			Foreground(colorActionText).
			Background(colorActionFill).
			Padding(0, 2),

		ActionSecondary: lipgloss.NewStyle().
			Foreground(colorPrimary).
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorMuted).
			Padding(0, 2),

		ActionPrimaryDisabled: lipgloss.NewStyle().
			Foreground(colorSecondary).
			Background(colorMuted).
			Padding(0, 2),

		PinnedIdle: lipgloss.NewStyle().
			Foreground(colorPrimary).
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorMuted).
			Padding(1, 3).
			Align(lipgloss.Center),

		PinnedSelected: lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			Border(lipgloss.ThickBorder()).
			BorderForeground(colorPrimary).
			Padding(1, 3).
			Align(lipgloss.Center),

		PinnedActive: lipgloss.NewStyle().
			Foreground(colorIndicator).
			Bold(true).
			Border(lipgloss.ThickBorder()).
			BorderForeground(colorIndicator).
			Padding(1, 3).
			Align(lipgloss.Center),
	}
}

// indicator returns the unicode glyph placed to the left of each list
// row: filled square for active (running), empty square otherwise.
func (s Styles) indicator(active bool) string {
	if active {
		return s.Indicator.Render("■")
	}
	return s.Muted.Render("□")
}
