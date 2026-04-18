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
//
// Themes
//
// `BUTTONS_THEME` can be set to `paper`, `phosphor`, or `amber` to
// override the default (adaptive light/dark) palette. Every theme
// defines the same set of color roles — only the hex values differ,
// and the Indicator-is-ACTIVE-only rule holds across all of them. The
// warm-shifted indicator on phosphor / amber exists because true
// orange on phosphor-green vibrates painfully; a warm red / brighter
// amber preserves the "this row is running" read without the clash.
package tui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"

	"github.com/autonoco/buttons/internal/settings"
)

// Brand palette. Exact hex values from the identity spec.
const (
	hexInk       = "#0A0A0A"
	hexIndicator = "#FF5A1F"
	hexPaper     = "#F5F5F2"
	hexAluminum  = "#C5C5C0"
	hexDust      = "#6E6E68"
)

// themeColors bundles the palette each theme provides. Using
// color.Color (from image/color) lets both plain lipgloss.Color values
// and adaptive ld(...) results share a single type — no interface
// gymnastics in the style builder.
type themeColors struct {
	primary     color.Color
	secondary   color.Color
	muted       color.Color
	indicator   color.Color
	onIndicator color.Color
	warn        color.Color
	actionFill  color.Color
	actionText  color.Color
	chipBg      color.Color
}

// resolveTheme picks a palette by precedence:
//
//  1. $BUTTONS_THEME env var — wins, always. Same pattern as batteries
//     and default-timeout: an explicit env override beats saved
//     preference so flipping themes for A/B comparison stays easy.
//  2. `theme` key in ~/.buttons/settings.json — the persistent choice
//     the user made via `buttons config set theme NAME`.
//  3. Default adaptive palette (ld(ink/paper)).
//
// Unknown values at any level fall through to the next step; a typo
// can't make the board illegible.
func resolveTheme() themeColors {
	if t := os.Getenv("BUTTONS_THEME"); t != "" {
		if colors, ok := themeByName(t); ok {
			return colors
		}
	}
	// Settings lookup. Errors fall through silently — a garbled
	// settings file should never block the TUI.
	if svc, err := settings.NewServiceFromEnv(); err == nil {
		if st, err := svc.Load(); err == nil {
			if name, ok := st.Theme(); ok {
				if colors, ok := themeByName(name); ok {
					return colors
				}
			}
		}
	}
	return defaultTheme()
}

// themeByName maps a theme name to its palette. Returns (_, false) for
// unknown names so the resolver can fall to the next step in its
// precedence chain.
func themeByName(name string) (themeColors, bool) {
	switch name {
	case "paper":
		return paperTheme(), true
	case "phosphor":
		return phosphorTheme(), true
	case "amber":
		return amberTheme(), true
	case "default":
		return defaultTheme(), true
	}
	return themeColors{}, false
}

// defaultTheme preserves the pre-theming behavior: ink on paper, or
// paper on a detected dark background. HasDarkBackground queries the
// terminal once at startup — mid-session theme changes aren't tracked.
func defaultTheme() themeColors {
	hasDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	ld := lipgloss.LightDark(hasDark)
	return themeColors{
		primary:     ld(lipgloss.Color(hexInk), lipgloss.Color(hexPaper)),
		secondary:   ld(lipgloss.Color(hexDust), lipgloss.Color("#A8A8A0")),
		muted:       ld(lipgloss.Color(hexAluminum), lipgloss.Color("#3A3A38")),
		indicator:   lipgloss.Color(hexIndicator),
		onIndicator: lipgloss.Color(hexPaper),
		warn:        lipgloss.Color("#F0C060"),
		actionFill:  lipgloss.Color(hexInk),
		actionText:  lipgloss.Color(hexPaper),
		chipBg:      ld(lipgloss.Color("#E8E8E3"), lipgloss.Color("#1E1E1C")),
	}
}

// paperTheme forces the ink-on-paper identity-spec palette regardless
// of the terminal's detected background. For users who run a light
// theme and want the exact on-brand colors.
func paperTheme() themeColors {
	return themeColors{
		primary:     lipgloss.Color(hexInk),
		secondary:   lipgloss.Color(hexDust),
		muted:       lipgloss.Color(hexAluminum),
		indicator:   lipgloss.Color(hexIndicator),
		onIndicator: lipgloss.Color(hexPaper),
		warn:        lipgloss.Color("#B7861F"), // darker amber reads on paper
		actionFill:  lipgloss.Color(hexInk),
		actionText:  lipgloss.Color(hexPaper),
		chipBg:      lipgloss.Color("#E8E8E3"),
	}
}

// phosphorTheme channels the Nostromo CRT — phosphor green on near-
// black. The indicator shifts to warm red so "active" reads instantly
// without the orange-on-green clash. Warn goes phosphor-amber for
// the same reason.
func phosphorTheme() themeColors {
	return themeColors{
		primary:     lipgloss.Color("#b8f3c3"),
		secondary:   lipgloss.Color("#6c9e78"),
		muted:       lipgloss.Color("#2a4a32"),
		indicator:   lipgloss.Color("#ff8a6c"), // warm red, not brand orange
		onIndicator: lipgloss.Color("#0a1c12"),
		warn:        lipgloss.Color("#ffd470"),
		actionFill:  lipgloss.Color("#1a3a22"),
		actionText:  lipgloss.Color("#b8f3c3"),
		chipBg:      lipgloss.Color("#1a3a22"),
	}
}

// amberTheme is the DEC VT220 / old-terminal amber look. The indicator
// warms to hot amber so running rows outshine the idle ones without
// leaving the palette.
func amberTheme() themeColors {
	return themeColors{
		primary:     lipgloss.Color("#f7c472"),
		secondary:   lipgloss.Color("#a17a3c"),
		muted:       lipgloss.Color("#3a2a15"),
		indicator:   lipgloss.Color("#ffbe5a"),
		onIndicator: lipgloss.Color("#1a1108"),
		warn:        lipgloss.Color("#ffe08a"),
		actionFill:  lipgloss.Color("#2a1a0a"),
		actionText:  lipgloss.Color("#f7c472"),
		chipBg:      lipgloss.Color("#2a1a0a"),
	}
}

// Styles bundles every Lip Gloss style the board uses. Built once at
// TUI startup via BuildStyles(), after theme + background resolution,
// so palette choices are resolved eagerly rather than on every render.
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
	KeyChip               lipgloss.Style

	PinnedIdle     lipgloss.Style
	PinnedSelected lipgloss.Style
	PinnedActive   lipgloss.Style

	HeroTitle lipgloss.Style
	HeroBody  lipgloss.Style
	HeroCode  lipgloss.Style

	StatusError lipgloss.Style
	StatusOK    lipgloss.Style
	StatusWarn  lipgloss.Style
}

// BuildStyles assembles the full style graph from whatever palette the
// active theme resolved to. Every style references roles (primary,
// indicator, …) — never hex literals — so swapping themes changes
// every rendered pixel in lockstep.
func BuildStyles() Styles {
	c := resolveTheme()

	return Styles{
		Wordmark:  lipgloss.NewStyle().Foreground(c.primary).Bold(true),
		Label:     lipgloss.NewStyle().Foreground(c.secondary),
		Divider:   lipgloss.NewStyle().Foreground(c.muted),
		Secondary: lipgloss.NewStyle().Foreground(c.secondary),
		Muted:     lipgloss.NewStyle().Foreground(c.muted),
		Indicator: lipgloss.NewStyle().Foreground(c.indicator),

		ButtonName:         lipgloss.NewStyle().Foreground(c.primary),
		ButtonNameSelected: lipgloss.NewStyle().Foreground(c.primary).Bold(true),
		ButtonNameActive:   lipgloss.NewStyle().Foreground(c.indicator).Bold(true),

		BadgeActive: lipgloss.NewStyle().
			Foreground(c.onIndicator).
			Background(c.indicator).
			Padding(0, 1).
			Bold(true),

		ActionPrimary: lipgloss.NewStyle().
			Foreground(c.actionText).
			Background(c.actionFill).
			Border(lipgloss.NormalBorder()).
			BorderForeground(c.actionFill).
			Padding(0, 2),

		ActionSecondary: lipgloss.NewStyle().
			Foreground(c.primary).
			Border(lipgloss.NormalBorder()).
			BorderForeground(c.muted).
			Padding(0, 2),

		ActionPrimaryDisabled: lipgloss.NewStyle().
			Foreground(c.secondary).
			Border(lipgloss.NormalBorder()).
			BorderForeground(c.muted).
			Padding(0, 2),

		KeyChip: lipgloss.NewStyle().
			Foreground(c.primary).
			Background(c.chipBg).
			Padding(0, 1).
			Bold(true),

		PinnedIdle: lipgloss.NewStyle().
			Foreground(c.primary).
			Border(lipgloss.NormalBorder()).
			BorderForeground(c.muted).
			Padding(1, 3).
			Align(lipgloss.Center),

		PinnedSelected: lipgloss.NewStyle().
			Foreground(c.primary).
			Bold(true).
			Border(lipgloss.ThickBorder()).
			BorderForeground(c.primary).
			Padding(1, 3).
			Align(lipgloss.Center),

		PinnedActive: lipgloss.NewStyle().
			Foreground(c.indicator).
			Bold(true).
			Border(lipgloss.ThickBorder()).
			BorderForeground(c.indicator).
			Padding(1, 3).
			Align(lipgloss.Center),

		HeroTitle: lipgloss.NewStyle().Foreground(c.primary).Bold(true),
		HeroBody:  lipgloss.NewStyle().Foreground(c.secondary),
		HeroCode:  lipgloss.NewStyle().Foreground(c.primary).Background(c.chipBg).Padding(0, 1),

		StatusError: lipgloss.NewStyle().Foreground(c.indicator),
		StatusOK:    lipgloss.NewStyle().Foreground(c.secondary),
		StatusWarn:  lipgloss.NewStyle().Foreground(c.warn),
	}
}

// indicator returns the unicode glyph placed to the left of each list
// row: filled square for active (running), empty square otherwise. When
// `frame` is non-negative and active is true, returns a spinner frame
// instead so the running state reads as "something's happening."
func (s Styles) indicator(active bool, frame int) string {
	if active {
		if frame >= 0 {
			return s.Indicator.Render(string(spinnerFrames[frame%len(spinnerFrames)]))
		}
		return s.Indicator.Render("■")
	}
	return s.Muted.Render("□")
}

// spinnerFrames cycles a braille-spinner. Standard Charm convention.
var spinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
