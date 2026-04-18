package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/autonoco/buttons/internal/button"
)

// View renders the detail page. Blocks, top to bottom:
//
//	header     name + description
//	spec       runtime, method/URL, timeout, max-resp, private-net
//	args       each arg's name, type, required/optional
//	usage      the exact `buttons press` line the user would type,
//	           with required-arg stubs
//	last run   timestamp, status, duration (if any)
//	footer     action hints
//
// Every block is rendered through the same leftPad so the vertical
// rhythm matches the board and the logs view.
func (m DetailModel) View() tea.View {
	var parts []string

	parts = append(parts, m.renderDetailHeader())
	parts = append(parts, m.renderDetailDivider())
	parts = append(parts, "")

	parts = append(parts, m.renderSpec())

	if len(m.btn.Args) > 0 {
		parts = append(parts, "")
		parts = append(parts, m.renderArgsBlock())
	}

	parts = append(parts, "")
	parts = append(parts, m.renderUsageBlock())

	if m.lastRun != nil {
		parts = append(parts, "")
		parts = append(parts, m.renderLastRunBlock())
	}

	parts = append(parts, "")
	parts = append(parts, m.renderDetailDivider())
	parts = append(parts, "")
	parts = append(parts, m.renderDetailFooter())

	v := tea.NewView(strings.Join(parts, "\n"))
	v.AltScreen = true
	return v
}

// renderDetailHeader is the name + optional description line.
func (m DetailModel) renderDetailHeader() string {
	left := m.styles.Wordmark.Render(m.btn.Name)
	if m.btn.Description != "" {
		left += "  " + m.styles.Muted.Render("— "+m.btn.Description)
	}
	right := m.styles.Label.Render("detail")
	w := m.width
	if w <= 0 {
		w = 80
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - leftPad*2
	if gap < 2 {
		gap = 2
	}
	return strings.Repeat(" ", leftPad) + left + strings.Repeat(" ", gap) + right
}

// renderDetailDivider is the board's divider style localized so the
// DetailModel doesn't need a pointer back to the board's Model.
func (m DetailModel) renderDetailDivider() string {
	w := m.width
	if w <= 4 {
		w = 80
	}
	return m.styles.Divider.Render(strings.Repeat("─", w))
}

// renderSpec shows runtime + type-specific fields. HTTP buttons get
// method / URL / max-resp / private-network; runtime-backed get the
// runtime label. Timeout appears everywhere.
func (m DetailModel) renderSpec() string {
	rows := [][2]string{}
	rows = append(rows, [2]string{"runtime", m.btn.Runtime})
	if m.btn.URL != "" {
		rows = append(rows, [2]string{"method", m.btn.Method})
		// URL value gets its {{template}} vars colorized — spec treats
		// template placeholders as the visually-distinctive "this
		// changes per press" bit of the spec.
		rows = append(rows, [2]string{"url", m.highlightTemplateVars(m.btn.URL)})
		rows = append(rows, [2]string{"max resp", button.FormatSize(button.ResolveMaxResponseBytes(m.btn.MaxResponseBytes))})
		net := "blocked"
		if m.btn.AllowPrivateNetworks {
			net = "allowed"
		}
		rows = append(rows, [2]string{"private net", net})
	}
	rows = append(rows, [2]string{"timeout", fmt.Sprintf("%ds", m.btn.TimeoutSeconds)})

	return m.renderKVBlock(rows)
}

// highlightTemplateVars wraps each {{name}} substring in the
// TemplateVar style. Everything outside the braces stays primary —
// so only the placeholders catch the eye, not the URL itself.
func (m DetailModel) highlightTemplateVars(s string) string {
	out := ""
	i := 0
	for i < len(s) {
		j := strings.Index(s[i:], "{{")
		if j < 0 {
			out += m.styles.ButtonName.Render(s[i:])
			break
		}
		out += m.styles.ButtonName.Render(s[i : i+j])
		end := strings.Index(s[i+j:], "}}")
		if end < 0 {
			out += m.styles.ButtonName.Render(s[i+j:])
			break
		}
		out += m.styles.TemplateVar.Render(s[i+j : i+j+end+2])
		i += j + end + 2
	}
	return out
}

// renderArgsBlock lists every arg with its type and required flag.
// Enum-value inline display is gated to the D2 branch that adds
// ArgDef.Values — once D2 lands this block will show the value set
// alongside the type tag.
func (m DetailModel) renderArgsBlock() string {
	label := m.styles.HeroTitle.Render("args")
	lines := []string{label, ""}

	for _, a := range m.btn.Args {
		name := m.styles.ButtonName.Render(a.Name)
		// Colorize "required" in indicator orange — spec treats it as
		// an active concern, not muted metadata. Optional stays
		// muted.
		var reqCol string
		if a.Required {
			reqCol = m.styles.RequiredTag.Render("required")
		} else {
			reqCol = m.styles.Muted.Render("optional")
		}
		typ := m.styles.Muted.Render(a.Type+" · ") + reqCol

		line := fmt.Sprintf("%-22s  %s", name, typ)
		lines = append(lines, line)
	}
	return indentBlock(strings.Join(lines, "\n"), leftPad*2)
}

// renderUsageBlock is the "here's the exact press command" section.
// Required args get stub placeholders (`<string>`) so the user can
// copy the line and fill in values. Spec colorizes the command
// keyword + flag tokens in orange; names and values stay primary —
// matches the arg form's "will run:" line.
func (m DetailModel) renderUsageBlock() string {
	label := m.styles.HeroTitle.Render("usage")

	build := func(extra string) string {
		out := m.styles.CommandKeyword.Render("buttons press") + " " + m.styles.ButtonName.Render(m.btn.Name)
		for _, a := range m.btn.Args {
			if !a.Required {
				continue
			}
			out += " " + m.styles.FlagToken.Render("--arg") + " " +
				m.styles.ButtonName.Render(fmt.Sprintf("%s=<%s>", a.Name, a.Type))
		}
		if extra != "" {
			out += " " + m.styles.FlagToken.Render(extra)
		}
		return out
	}

	lines := []string{
		label,
		"",
		build(""),
		build("--json"),
	}
	return indentBlock(strings.Join(lines, "\n"), leftPad*2)
}

// renderLastRunBlock shows a single one-line summary of the most
// recent press. Anything deeper lives in the logs view / history
// command — this is a glance, not a dashboard.
func (m DetailModel) renderLastRunBlock() string {
	r := m.lastRun
	statusCol := m.styles.StatusOK.Render("✓ " + r.Status)
	if r.Status != "ok" {
		statusCol = m.styles.StatusError.Render("✗ " + r.Status)
	}
	line := fmt.Sprintf("%s  %s  %dms",
		r.StartedAt.Local().Format("2006-01-02 15:04"),
		statusCol,
		r.DurationMs,
	)
	return indentBlock(
		m.styles.HeroTitle.Render("last run")+"\n\n"+line,
		leftPad*2,
	)
}

// renderDetailFooter is the hint line. e is hidden for URL / prompt
// buttons because there's no code file to edit there.
func (m DetailModel) renderDetailFooter() string {
	pair := func(key, label string) string {
		return m.styles.KeyChip.Render(key) + m.styles.Muted.Render(" "+label)
	}
	sep := m.styles.Muted.Render("  ·  ")
	hints := pair("↵", "back")
	if m.codePath != "" {
		hints = pair("e", "edit") + sep + hints
	}
	return indentBlock(hints, leftPad)
}

// renderKVBlock formats a key/value table. Value column is colored
// primary so the eye tracks values over labels — labels are just
// signposts.
func (m DetailModel) renderKVBlock(rows [][2]string) string {
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		key := m.styles.Muted.Render(fmt.Sprintf("%-12s", r[0]))
		val := m.styles.ButtonName.Render(r[1])
		lines = append(lines, key+"  "+val)
	}
	return indentBlock(strings.Join(lines, "\n"), leftPad*2)
}
