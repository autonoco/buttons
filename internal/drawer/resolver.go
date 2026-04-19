package drawer

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	celgo "github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
)

// Reference syntax in drawer step args. A value like
// "${build.output.version}" is resolved at press time against the
// drawer's execution context (inputs + per-step outputs + env).
//
// Stage 2: the contents of ${...} are parsed as CEL expressions
// (github.com/google/cel-go). The wire format is stable across
// stages — agents that learned to emit ${step.output.field} in
// stage 1 still work, and now also get:
//
//	${build.output.version ?? inputs.fallback}   — null coalescing
//	${'shipped ' + publish.output.url}           — string concat
//	${inputs.count * 2}                          — arithmetic
//	${inputs.env == 'prod' ? 'strict' : 'lax'}   — ternary
//
// $ENV{VAR} is sugar for ${env.VAR} kept for readability.
//
// Reserved roots available inside CEL expressions:
//
//	inputs.*   — drawer-level inputs supplied at press time
//	<step_id>  — a completed step; <step_id>.output.* walks the
//	             button's structured JSON output
//	env.*      — process environment at resolve time

var refPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$ENV\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// dottedPath matches a pure dotted-path CEL expression (no operators,
// no function calls). Used by ExtractRefs to surface type-checkable
// references to the validator; anything more complex is skipped.
var dottedPath = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

// Context is the resolution environment. Keys are reference roots
// (inputs, <step_id>). Values can be any JSON-ish shape. "env" is
// injected automatically from os.Environ().
type Context map[string]any

// Resolve replaces every ${...} / $ENV{VAR} substring in v. If v is
// a string that is ENTIRELY one reference, the return value is the
// raw CEL result (preserving its type — int stays int). Otherwise
// each reference is stringified and interpolated.
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

func resolveString(s string, ctx Context) (any, error) {
	// Whole-string reference: value-preserving.
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") && strings.Count(s, "${") == 1 {
		expr := s[2 : len(s)-1]
		return evalCEL(expr, ctx)
	}
	if strings.HasPrefix(s, "$ENV{") && strings.HasSuffix(s, "}") && strings.Count(s, "$ENV{") == 1 {
		name := s[5 : len(s)-1]
		return os.Getenv(name), nil
	}

	// Mixed literal + expr. Stringify each match and interpolate.
	var firstErr error
	out := refPattern.ReplaceAllStringFunc(s, func(match string) string {
		m := refPattern.FindStringSubmatch(match)
		var val any
		var err error
		switch {
		case m[1] != "":
			val, err = evalCEL(m[1], ctx)
		case m[2] != "":
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

// evalCEL compiles and evaluates a CEL expression against the
// context. Each top-level key in ctx becomes a DYN-typed CEL
// variable; env is injected from os.Environ() every call.
//
// Simpler to rebuild the env per call than to cache — CEL compilation
// is cheap (microseconds) and the var set changes between calls
// (different upstream step ids per drawer). If this shows up in
// profiles later, cache by context-shape fingerprint.
func evalCEL(expr string, ctx Context) (any, error) {
	opts := make([]celgo.EnvOption, 0, len(ctx)+1)
	for k := range ctx {
		opts = append(opts, celgo.Variable(k, celgo.DynType))
	}
	opts = append(opts, celgo.Variable("env", celgo.DynType))

	env, err := celgo.NewEnv(opts...)
	if err != nil {
		return nil, &ResolveError{Path: expr, Reason: "cel env: " + err.Error()}
	}
	ast, issues := env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		return nil, &ResolveError{Path: expr, Reason: "parse: " + issues.Err().Error()}
	}
	prg, err := env.Program(ast)
	if err != nil {
		return nil, &ResolveError{Path: expr, Reason: "program: " + err.Error()}
	}

	activation := make(map[string]any, len(ctx)+1)
	for k, v := range ctx {
		activation[k] = v
	}
	activation["env"] = envFromOS()

	val, _, err := prg.Eval(activation)
	if err != nil {
		return nil, &ResolveError{Path: expr, Reason: err.Error()}
	}
	return celToAny(val), nil
}

// envFromOS materializes the current process environment as a
// CEL-friendly map. Called once per expression eval — cheap relative
// to CEL compile cost, and keeps env reads honest to "now", not
// "when the drawer was authored."
func envFromOS() map[string]any {
	e := os.Environ()
	out := make(map[string]any, len(e))
	for _, kv := range e {
		if i := strings.Index(kv, "="); i > 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

// celToAny unwraps a CEL value to its native Go representation so
// the caller can round-trip it through encoding/json without
// knowing about CEL types. CEL lists/maps are handled recursively
// so nested structures still work.
func celToAny(v ref.Val) any {
	native := v.Value()
	switch t := native.(type) {
	case map[ref.Val]ref.Val:
		out := make(map[string]any, len(t))
		for k, val := range t {
			key, _ := k.Value().(string)
			out[key] = celToAny(val)
		}
		return out
	case []ref.Val:
		out := make([]any, len(t))
		for i, elem := range t {
			out[i] = celToAny(elem)
		}
		return out
	default:
		return native
	}
}

// ResolveError carries both the expression that failed and the
// specific reason. Agents turn this into a remediation hint.
type ResolveError struct {
	Path   string
	Reason string
}

func (e *ResolveError) Error() string {
	return fmt.Sprintf("reference ${%s}: %s", e.Path, e.Reason)
}

// ExtractRefs returns every dotted-path identifier referenced inside
// any ${...} in v. Used by the validator to type-check simple
// references against upstream output schemas. Complex expressions
// (operators, ternaries, function calls) are skipped — they'll fail
// at runtime if malformed, but can't be statically checked here in
// stage 2. A future pass can wire CEL's full type checker in.
func ExtractRefs(v any) []string {
	var out []string
	seen := map[string]bool{}
	walk(v, func(s string) {
		matches := refPattern.FindAllStringSubmatch(s, -1)
		for _, m := range matches {
			expr := strings.TrimSpace(m[1])
			if expr == "" {
				continue
			}
			// Only surface pure dotted paths. Complex CEL is
			// intentionally opaque to the validator.
			if dottedPath.MatchString(expr) && !seen[expr] {
				seen[expr] = true
				out = append(out, expr)
			}
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
