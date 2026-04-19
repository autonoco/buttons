package drawer

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Reference syntax used in drawer step args. A value like
// "${build.output.version}" is resolved at press time against the
// drawer's execution context (inputs map + per-step outputs).
//
// Stage 1 supports only dotted-path field access. No operators, no
// fallbacks, no arithmetic. Stage 2 swaps the implementation for
// google/cel-go while keeping the ${...} wire format unchanged —
// this package's Resolve signature is the stable interface.
//
// Reserved roots that the context must provide:
//
//	inputs.*   — drawer-level inputs supplied at press time
//	<step_id>  — a completed step; <step_id>.output.* walks the
//	             button's structured JSON output
//	env.*      — environment variables; read from os.Getenv at
//	             resolve time, never stored in the drawer spec
//
// $ENV{VAR} is a pseudo-reference that resolves through the same
// env path but is accepted as sugar so drawer.json is readable when
// mixed with CEL-style ${...} later.

var refPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$ENV\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Context is the resolution environment. Keys are reference roots
// (inputs, env, <step_id>). Values can be any JSON-ish shape —
// resolve() walks them with reflect-free dotted-path access.
type Context map[string]any

// Resolve replaces every ${...} / $ENV{VAR} substring in v. If v is
// a string that is ENTIRELY one reference, the return value is the
// raw resolved value (preserving its type — int stays int). Otherwise
// each reference is stringified and interpolated.
//
// Recursively handles maps and slices so Step.Args (map[string]any)
// can carry arbitrarily nested literal + reference mixes.
func Resolve(v any, ctx Context) (any, error) {
	switch t := v.(type) {
	case string:
		return resolveString(t, ctx)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, elem := range t {
			r, err := Resolve(elem, ctx)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, elem := range t {
			r, err := Resolve(elem, ctx)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		return v, nil
	}
}

// resolveString handles the string case. The common shape is a
// whole-string reference ("${build.output.version}") where we want
// to preserve the underlying type; handle that specially so an int
// output doesn't become "42".
func resolveString(s string, ctx Context) (any, error) {
	// Whole-string reference: value-preserving.
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") && strings.Count(s, "${") == 1 {
		path := s[2 : len(s)-1]
		return lookup(path, ctx)
	}
	if strings.HasPrefix(s, "$ENV{") && strings.HasSuffix(s, "}") && strings.Count(s, "$ENV{") == 1 {
		name := s[5 : len(s)-1]
		return os.Getenv(name), nil
	}

	// Mixed literal + ref: stringify each match and interpolate.
	var firstErr error
	out := refPattern.ReplaceAllStringFunc(s, func(match string) string {
		m := refPattern.FindStringSubmatch(match)
		var val any
		var err error
		if m[1] != "" {
			val, err = lookup(m[1], ctx)
		} else if m[2] != "" {
			val = os.Getenv(m[2])
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return match
		}
		return fmt.Sprintf("%v", val)
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// lookup walks a dotted path through the context map. Arrays are
// accessed with numeric indices: "items.0.name".
func lookup(path string, ctx Context) (any, error) {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty reference path")
	}

	// env.* reads os environment at resolve time.
	if parts[0] == "env" {
		if len(parts) != 2 {
			return nil, fmt.Errorf("env references must be ${env.VARNAME}, got %q", path)
		}
		return os.Getenv(parts[1]), nil
	}

	cur, ok := ctx[parts[0]]
	if !ok {
		return nil, &ResolveError{Path: path, Reason: fmt.Sprintf("unknown root %q", parts[0])}
	}
	for _, seg := range parts[1:] {
		switch c := cur.(type) {
		case map[string]any:
			v, exists := c[seg]
			if !exists {
				return nil, &ResolveError{Path: path, Reason: fmt.Sprintf("field %q not found", seg)}
			}
			cur = v
		case []any:
			var idx int
			if _, err := fmt.Sscanf(seg, "%d", &idx); err != nil {
				return nil, &ResolveError{Path: path, Reason: fmt.Sprintf("expected numeric index, got %q", seg)}
			}
			if idx < 0 || idx >= len(c) {
				return nil, &ResolveError{Path: path, Reason: fmt.Sprintf("index %d out of range", idx)}
			}
			cur = c[idx]
		default:
			return nil, &ResolveError{Path: path, Reason: fmt.Sprintf("cannot index into %T at %q", cur, seg)}
		}
	}
	return cur, nil
}

// ResolveError carries both the full reference path and the specific
// reason it failed — agents can turn this into a targeted error
// message with remediation.
type ResolveError struct {
	Path   string
	Reason string
}

func (e *ResolveError) Error() string {
	return fmt.Sprintf("reference ${%s}: %s", e.Path, e.Reason)
}

// ExtractRefs scans a value (typically a Step.Args map) and returns
// every ${ref} path it contains. Used by the validator to type-check
// references at connect time without running the resolver.
func ExtractRefs(v any) []string {
	var out []string
	walk(v, func(s string) {
		matches := refPattern.FindAllStringSubmatch(s, -1)
		for _, m := range matches {
			if m[1] != "" {
				out = append(out, m[1])
			}
			// $ENV{...} refs don't get type-checked against upstream
			// schemas — they're just env strings.
		}
	})
	return out
}

func walk(v any, onString func(string)) {
	switch t := v.(type) {
	case string:
		onString(t)
	case map[string]any:
		for _, elem := range t {
			walk(elem, onString)
		}
	case []any:
		for _, elem := range t {
			walk(elem, onString)
		}
	}
}
