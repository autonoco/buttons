package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/autonoco/buttons/internal/button"
)

// argForm is the inline press-with-args form that the board opens
// when a button with required args is invoked. Hand-rolled rather
// than huh-embedded so the board owns its keyboard dispatch without
// juggling two tea.Models.
//
// Behavior:
//
//   - One field per declared arg (both required and optional). Users
//     land on the first required field. Tab / shift+tab navigate.
//   - Typing characters appends; backspace drops the last rune.
//   - Enter submits IF every required field has a non-empty value;
//     otherwise the cursor jumps to the first missing required field.
//   - Esc cancels — the board returns to its normal view with no
//     press dispatched.
//
// The form is intentionally untyped at the UI level: int and bool
// fields accept free text (users type "42" / "true") and validation
// happens when the submitted values run through button.ParsePressArgs
// — the same code path the CLI uses. One validator, one error shape.
type argForm struct {
	btnName string
	fields  []argField
	cursor  int
	// lastErr surfaces validation errors from ParsePressArgs after
	// the user presses Enter — rendered below the fields so the form
	// doesn't dismiss on a typo.
	lastErr string
}

// argField is one editable row. For text-style types (string / int /
// bool), `value` is the current input buffer. For `enum`, `values`
// holds the declared choices and `selectedIdx` is the currently-
// highlighted one — `value` is derived from `values[selectedIdx]` on
// every selection move so submit and render agree.
type argField struct {
	name     string
	typ      string
	required bool
	value    string

	// Enum-only.
	values      []string
	selectedIdx int
}

// newArgForm builds a form from a button's ArgDefs. Returns nil when
// the button has no args — callers should skip the form in that case
// and press directly.
func newArgForm(btn *button.Button) *argForm {
	if len(btn.Args) == 0 {
		return nil
	}
	fields := make([]argField, len(btn.Args))
	for i, a := range btn.Args {
		f := argField{name: a.Name, typ: a.Type, required: a.Required}
		// Enum: pre-select the first value so Enter can submit
		// immediately without additional input. Any pre-selected
		// value is valid by definition.
		if a.Type == "enum" && len(a.Values) > 0 {
			f.values = a.Values
			f.selectedIdx = 0
			f.value = a.Values[0]
		}
		fields[i] = f
	}
	f := &argForm{btnName: btn.Name, fields: fields}
	f.cursor = f.firstRequiredIndex()
	return f
}

// firstRequiredIndex returns the index of the first required field
// that's still empty — the natural landing spot on open, and where
// we bounce the cursor on a premature submit.
func (f *argForm) firstRequiredIndex() int {
	for i, fld := range f.fields {
		if fld.required && fld.value == "" {
			return i
		}
	}
	return 0
}

// argFormResult is what handleKey returns to the board — exactly one
// of submit / cancel / nothing.
type argFormResult int

const (
	argFormPending argFormResult = iota
	argFormSubmit
	argFormCancel
)

// handleKey processes one key press. Returns (result, values) where
// values is populated only when result == argFormSubmit. The board
// is responsible for running ParsePressArgs and dispatching the press.
func (f *argForm) handleKey(msg tea.KeyPressMsg) (argFormResult, map[string]string) {
	key := msg.String()
	switch key {
	case "esc":
		return argFormCancel, nil

	case "enter":
		// Bounce to the first missing required field rather than
		// submitting an incomplete form — users type Enter reflexively
		// and silently rejecting would feel broken.
		if idx := f.firstRequiredIndex(); idx >= 0 && f.fields[idx].value == "" {
			f.cursor = idx
			f.lastErr = fmt.Sprintf("%s is required", f.fields[idx].name)
			return argFormPending, nil
		}
		return argFormSubmit, f.values()

	case "tab", "down":
		f.cursor = (f.cursor + 1) % len(f.fields)
		f.lastErr = ""
		return argFormPending, nil

	case "shift+tab", "up":
		f.cursor = (f.cursor - 1 + len(f.fields)) % len(f.fields)
		f.lastErr = ""
		return argFormPending, nil

	case "left":
		// Only meaningful on enum fields — moves the selection back.
		// No-op on text fields so users don't feel like they broke
		// anything by pressing it.
		f.moveEnumSelection(-1)
		f.lastErr = ""
		return argFormPending, nil

	case "right":
		f.moveEnumSelection(+1)
		f.lastErr = ""
		return argFormPending, nil

	case "backspace":
		// Enum values aren't edited character-by-character — ignore
		// backspace so users don't clear a radio pick by reflex.
		if f.fields[f.cursor].typ == "enum" {
			return argFormPending, nil
		}
		v := f.fields[f.cursor].value
		if len(v) > 0 {
			// Rune-safe: strip the last complete rune, not just byte.
			runes := []rune(v)
			f.fields[f.cursor].value = string(runes[:len(runes)-1])
		}
		f.lastErr = ""
		return argFormPending, nil
	}

	// Printable input — append every rune of the Text (handles
	// multi-rune keys on non-Latin layouts without special-casing).
	// Enum fields don't accept text input; the user moves between
	// choices with left/right.
	if f.fields[f.cursor].typ == "enum" {
		return argFormPending, nil
	}
	if text := msg.Text; text != "" {
		f.fields[f.cursor].value += text
		f.lastErr = ""
	}
	return argFormPending, nil
}

// moveEnumSelection advances the current field's enum selection by
// delta (wrapping). No-op if the current field isn't an enum.
func (f *argForm) moveEnumSelection(delta int) {
	fld := &f.fields[f.cursor]
	if fld.typ != "enum" || len(fld.values) == 0 {
		return
	}
	n := len(fld.values)
	fld.selectedIdx = (fld.selectedIdx + delta + n) % n
	fld.value = fld.values[fld.selectedIdx]
}

// values returns the raw key→value map for ParsePressArgs. Empty
// optional fields are omitted (ParsePressArgs would reject an empty
// string for a typed optional anyway).
func (f *argForm) values() map[string]string {
	out := make(map[string]string, len(f.fields))
	for _, fld := range f.fields {
		if fld.value == "" && !fld.required {
			continue
		}
		out[fld.name] = fld.value
	}
	return out
}

// render paints the form as a single block that replaces the board's
// content area. Styled to read as a focused prompt: header ("press
// NAME · N required args"), a row per field, and an inline error /
// preview line. The board's header + footer render around this.
func (f *argForm) render(styles Styles, width int) string {
	if width <= 0 {
		width = 80
	}

	reqCount := 0
	for _, fld := range f.fields {
		if fld.required {
			reqCount++
		}
	}

	title := styles.HeroTitle.Render("press " + f.btnName)
	subtitle := styles.Muted.Render(fmt.Sprintf("  ·  %d arg", len(f.fields)))
	if len(f.fields) != 1 {
		subtitle = styles.Muted.Render(fmt.Sprintf("  ·  %d args · %d required", len(f.fields), reqCount))
	}

	lines := []string{
		title + subtitle,
		"",
	}
	for i, fld := range f.fields {
		lines = append(lines, f.renderField(styles, i, fld))
	}
	if f.lastErr != "" {
		lines = append(lines, "", styles.StatusError.Render("!  "+f.lastErr))
	} else {
		lines = append(lines, "", styles.Muted.Render("↵ run · ⇥ next field · ⎋ cancel"))
	}

	return indentBlock(strings.Join(lines, "\n"), leftPad*2)
}

// renderField formats one row: name, type tag, required/optional tag,
// then the current value with a caret on the focused row.
func (f *argForm) renderField(styles Styles, idx int, fld argField) string {
	focused := idx == f.cursor

	var name string
	if focused {
		name = styles.ButtonNameSelected.Render("› " + fld.name)
	} else {
		name = styles.ButtonName.Render("  " + fld.name)
	}

	typTag := styles.Muted.Render(fmt.Sprintf("%s · %s", fld.typ, optionalityLabel(fld.required)))

	var value string
	if fld.typ == "enum" {
		value = f.renderEnumValue(styles, fld, focused)
	} else if focused {
		// Block cursor at the end of the buffer.
		value = styles.HeroCode.Render(fld.value + "▎")
	} else if fld.value != "" {
		value = styles.HeroCode.Render(fld.value)
	} else {
		value = styles.Muted.Render("—")
	}

	return fmt.Sprintf("%-22s  %-20s  %s", name, typTag, value)
}

// renderEnumValue draws a horizontal choice strip for an enum field.
// Unselected values render muted; the selected value renders in
// HeroCode (the "inline code" chip style). When the row is focused,
// arrow glyphs flank the set so the left/right navigation affordance
// is visible without reading the help line.
func (f *argForm) renderEnumValue(styles Styles, fld argField, focused bool) string {
	if len(fld.values) == 0 {
		return styles.Muted.Render("(no choices)")
	}
	parts := make([]string, 0, len(fld.values))
	for i, v := range fld.values {
		if i == fld.selectedIdx {
			parts = append(parts, styles.HeroCode.Render(v))
		} else {
			parts = append(parts, styles.Muted.Render(v))
		}
	}
	body := strings.Join(parts, styles.Muted.Render("  "))
	if focused {
		return styles.Muted.Render("‹ ") + body + styles.Muted.Render(" ›")
	}
	return "  " + body + "  "
}

func optionalityLabel(required bool) string {
	if required {
		return "required"
	}
	return "optional"
}
