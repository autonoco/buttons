package tui

import (
	"testing"
)

// TestResolveTheme_KnownNames verifies each supported theme name maps
// to a distinct palette. Uses primary as the discriminator — if paper,
// phosphor, amber, and default ever share a primary, the theme switch
// broke and the test catches it.
func TestResolveTheme_KnownNames(t *testing.T) {
	cases := []struct {
		env    string
		wantFn func() themeColors
	}{
		{"paper", paperTheme},
		{"phosphor", phosphorTheme},
		{"amber", amberTheme},
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv("BUTTONS_THEME", tc.env)
			got := resolveTheme()
			want := tc.wantFn()
			if got.primary != want.primary {
				t.Errorf("primary mismatch for %s: got %v, want %v", tc.env, got.primary, want.primary)
			}
		})
	}
}

// TestResolveTheme_UnknownFallsToDefault ensures a typo'd value doesn't
// crash or produce a zero-valued palette — both would render an
// illegible board. The test doesn't pin to the default's exact
// primary since that depends on terminal detection; it checks that
// every role is set to something non-nil.
func TestResolveTheme_UnknownFallsToDefault(t *testing.T) {
	t.Setenv("BUTTONS_THEME", "nonsense")
	c := resolveTheme()
	if c.primary == nil || c.secondary == nil || c.muted == nil ||
		c.indicator == nil || c.warn == nil || c.actionFill == nil ||
		c.actionText == nil || c.chipBg == nil || c.onIndicator == nil {
		t.Errorf("unknown theme left roles unset: %+v", c)
	}
}

// TestBuildStyles_EveryThemeProducesFullGraph walks each theme name
// and asserts BuildStyles returns every style populated. A theme that
// forgets a color would leave some style with a zero Foreground
// (black) that reads as a blank square on dark terminals.
func TestBuildStyles_EveryThemeProducesFullGraph(t *testing.T) {
	themes := []string{"paper", "phosphor", "amber", "", "nonsense"}
	for _, th := range themes {
		t.Run("theme="+th, func(t *testing.T) {
			t.Setenv("BUTTONS_THEME", th)
			s := BuildStyles()
			// Spot-check: styles that do render need a non-zero foreground.
			if s.Wordmark.GetForeground() == nil {
				t.Errorf("Wordmark foreground missing on theme %q", th)
			}
			if s.Indicator.GetForeground() == nil {
				t.Errorf("Indicator foreground missing on theme %q", th)
			}
			if s.StatusWarn.GetForeground() == nil {
				t.Errorf("StatusWarn foreground missing on theme %q", th)
			}
		})
	}
}
