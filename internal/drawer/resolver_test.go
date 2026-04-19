package drawer

import (
	"testing"
)

func TestResolve_WholeStringRef_PreservesType(t *testing.T) {
	ctx := Context{
		"inputs": map[string]any{"count": 42},
		"build":  map[string]any{"output": map[string]any{"version": "1.2.3"}},
	}

	got, err := Resolve("${inputs.count}", ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// CEL returns int64 for integer values.
	if i64, ok := got.(int64); !ok || i64 != 42 {
		if i, ok2 := got.(int); !ok2 || i != 42 {
			t.Errorf("want 42, got %v (%T)", got, got)
		}
	}

	got, err = Resolve("${build.output.version}", ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "1.2.3" {
		t.Errorf("want string 1.2.3, got %v", got)
	}
}

// CEL operators — new in stage 2. String concat, arithmetic, ternary.
func TestResolve_CELOperators(t *testing.T) {
	ctx := Context{
		"inputs": map[string]any{"fallback": "default", "count": 3, "env_name": "prod"},
		"build":  map[string]any{"output": map[string]any{"version": "1.2.3"}},
	}

	// Ternary with has() for null-coalescing.
	got, err := Resolve("${has(build.output.url) ? build.output.url : inputs.fallback}", ctx)
	if err != nil {
		t.Fatalf("ternary: %v", err)
	}
	if got != "default" {
		t.Errorf("ternary: got %v", got)
	}

	// String concat.
	got, err = Resolve("${'shipped ' + build.output.version}", ctx)
	if err != nil {
		t.Fatalf("concat: %v", err)
	}
	if got != "shipped 1.2.3" {
		t.Errorf("concat: got %v", got)
	}

	// Equality.
	got, err = Resolve("${inputs.env_name == 'prod'}", ctx)
	if err != nil {
		t.Fatalf("eq: %v", err)
	}
	if got != true {
		t.Errorf("eq: got %v", got)
	}
}

func TestResolve_MixedInterpolation(t *testing.T) {
	ctx := Context{"build": map[string]any{"output": map[string]any{"url": "https://example.com"}}}
	got, err := Resolve("shipped ${build.output.url}", ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "shipped https://example.com" {
		t.Errorf("want interpolated string, got %q", got)
	}
}

func TestResolve_EnvReference(t *testing.T) {
	t.Setenv("BUTTONS_TEST_VAR", "secret-value")
	got, err := Resolve("${env.BUTTONS_TEST_VAR}", Context{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "secret-value" {
		t.Errorf("want env value, got %v", got)
	}
}

func TestResolve_ENVSugarSyntax(t *testing.T) {
	t.Setenv("BUTTONS_TEST_VAR", "from-sugar")
	got, err := Resolve("$ENV{BUTTONS_TEST_VAR}", Context{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "from-sugar" {
		t.Errorf("want env value, got %v", got)
	}
}

func TestResolve_UnknownRoot_Errors(t *testing.T) {
	_, err := Resolve("${nonexistent.field}", Context{})
	if err == nil {
		t.Fatal("expected error for unknown root, got nil")
	}
}

func TestResolve_MapAndSliceRecursion(t *testing.T) {
	ctx := Context{"inputs": map[string]any{"name": "world"}}
	got, err := Resolve(map[string]any{
		"greeting": "hello ${inputs.name}",
		"nested":   []any{"${inputs.name}", "literal"},
	}, ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	m := got.(map[string]any)
	if m["greeting"] != "hello world" {
		t.Errorf("greeting: got %v", m["greeting"])
	}
	s := m["nested"].([]any)
	if s[0] != "world" || s[1] != "literal" {
		t.Errorf("nested: got %v", s)
	}
}

func TestExtractRefs(t *testing.T) {
	v := map[string]any{
		"a": "${step1.output.foo}",
		"b": "hello ${inputs.name}",
		"c": "${env.X}",
		"d": []any{"${step2.output.bar}"},
		"e": "$ENV{TOKEN}", // should NOT appear — $ENV{} isn't schema-checked
	}
	refs := ExtractRefs(v)
	want := map[string]bool{
		"step1.output.foo": true,
		"inputs.name":      true,
		"env.X":            true,
		"step2.output.bar": true,
	}
	if len(refs) != len(want) {
		t.Errorf("want %d refs, got %d: %v", len(want), len(refs), refs)
	}
	for _, r := range refs {
		if !want[r] {
			t.Errorf("unexpected ref: %s", r)
		}
	}
}
