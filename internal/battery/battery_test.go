package battery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateKey(t *testing.T) {
	cases := []struct {
		key string
		ok  bool
	}{
		{"APIFY_TOKEN", true},
		{"OPENAI_KEY_2", true},
		{"A", true},
		{"", false},
		{"lowercase", false},
		{"1LEADING_DIGIT", false},
		{"HAS-DASH", false},
		{"HAS SPACE", false},
	}
	for _, tc := range cases {
		err := ValidateKey(tc.key)
		if (err == nil) != tc.ok {
			t.Errorf("ValidateKey(%q) ok=%v, err=%v", tc.key, tc.ok, err)
		}
	}
}

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"a":         "•",
		"abcd":      "••••",
		"abcde":     "•bcde",
		"longvalue": "•••••alue",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestService_SetGetDelete_GlobalOnly(t *testing.T) {
	globalDir := t.TempDir()
	svc := NewService(globalDir, "")

	if err := svc.Set("APIFY_TOKEN", "secret", ScopeGlobal); err != nil {
		t.Fatalf("set: %v", err)
	}

	v, scope, err := svc.Get("APIFY_TOKEN")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if v != "secret" {
		t.Errorf("value = %q, want secret", v)
	}
	if scope != ScopeGlobal {
		t.Errorf("scope = %q, want global", scope)
	}

	if err := svc.Delete("APIFY_TOKEN", ScopeGlobal); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err := svc.Get("APIFY_TOKEN"); err != ErrNotFound {
		t.Errorf("after delete, err = %v, want ErrNotFound", err)
	}
}

func TestService_LocalOverridesGlobal(t *testing.T) {
	globalDir := t.TempDir()
	localDir := t.TempDir()
	svc := NewService(globalDir, localDir)

	if err := svc.Set("TOKEN", "global-value", ScopeGlobal); err != nil {
		t.Fatalf("set global: %v", err)
	}
	if err := svc.Set("TOKEN", "local-value", ScopeLocal); err != nil {
		t.Fatalf("set local: %v", err)
	}

	v, scope, err := svc.Get("TOKEN")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if v != "local-value" {
		t.Errorf("value = %q, want local-value", v)
	}
	if scope != ScopeLocal {
		t.Errorf("scope = %q, want local", scope)
	}

	env, err := svc.Env()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	if env["TOKEN"] != "local-value" {
		t.Errorf("env TOKEN = %q, want local-value", env["TOKEN"])
	}
}

func TestService_LocalUnavailable(t *testing.T) {
	svc := NewService(t.TempDir(), "")
	if err := svc.Set("FOO", "bar", ScopeLocal); err != ErrLocalUnavailable {
		t.Errorf("Set(local) err = %v, want ErrLocalUnavailable", err)
	}
	if err := svc.Delete("FOO", ScopeLocal); err != ErrLocalUnavailable {
		t.Errorf("Delete(local) err = %v, want ErrLocalUnavailable", err)
	}
}

func TestService_List_BothScopes(t *testing.T) {
	globalDir := t.TempDir()
	localDir := t.TempDir()
	svc := NewService(globalDir, localDir)

	if err := svc.Set("A_GLOBAL", "g", ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	if err := svc.Set("Z_GLOBAL", "z", ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	if err := svc.Set("B_LOCAL", "b", ScopeLocal); err != nil {
		t.Fatal(err)
	}

	entries, err := svc.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	// Global first, sorted alphabetically.
	if entries[0].Key != "A_GLOBAL" || entries[0].Scope != ScopeGlobal {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].Key != "Z_GLOBAL" || entries[1].Scope != ScopeGlobal {
		t.Errorf("entries[1] = %+v", entries[1])
	}
	if entries[2].Key != "B_LOCAL" || entries[2].Scope != ScopeLocal {
		t.Errorf("entries[2] = %+v", entries[2])
	}
}

func TestService_InvalidKey(t *testing.T) {
	svc := NewService(t.TempDir(), "")
	if err := svc.Set("lowercase", "v", ScopeGlobal); err == nil {
		t.Errorf("Set(lowercase key) should fail")
	}
	if _, _, err := svc.Get("lowercase"); err == nil {
		t.Errorf("Get(lowercase key) should fail")
	}
}

func TestService_FileMode0600(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(dir, "")
	if err := svc.Set("FOO", "bar", ScopeGlobal); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "batteries.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
}
