package settings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestService_LoadMissingFile(t *testing.T) {
	svc := NewService(t.TempDir())
	st, err := svc.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if st.SchemaVersion != schemaVersion {
		t.Errorf("schema = %d, want %d", st.SchemaVersion, schemaVersion)
	}
	if _, ok := st.DefaultTimeout(); ok {
		t.Errorf("expected DefaultTimeout unset on first load")
	}
}

func TestService_SetAndLoad_DefaultTimeout(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(dir)
	if err := svc.Set(KeyDefaultTimeout, "600"); err != nil {
		t.Fatalf("set: %v", err)
	}
	st, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	v, ok := st.DefaultTimeout()
	if !ok {
		t.Fatal("DefaultTimeout should be set")
	}
	if v != 600 {
		t.Errorf("timeout = %d, want 600", v)
	}
}

func TestService_Set_InvalidTimeout(t *testing.T) {
	svc := NewService(t.TempDir())
	cases := []string{"", "abc", "0", "-5"}
	for _, c := range cases {
		if err := svc.Set(KeyDefaultTimeout, c); err == nil {
			t.Errorf("Set(%q) should fail", c)
		}
	}
}

func TestService_Unset(t *testing.T) {
	svc := NewService(t.TempDir())
	if err := svc.Set(KeyDefaultTimeout, "600"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Unset(KeyDefaultTimeout); err != nil {
		t.Fatalf("unset: %v", err)
	}
	st, _ := svc.Load()
	if _, ok := st.DefaultTimeout(); ok {
		t.Errorf("DefaultTimeout should be unset after Unset")
	}
}

func TestService_UnknownKey(t *testing.T) {
	svc := NewService(t.TempDir())
	err := svc.Set("nope", "value")
	if !errors.Is(err, ErrUnknownKey) {
		t.Errorf("Set(nope): err = %v, want ErrUnknownKey", err)
	}
	err = svc.Unset("nope")
	if !errors.Is(err, ErrUnknownKey) {
		t.Errorf("Unset(nope): err = %v, want ErrUnknownKey", err)
	}
}

func TestService_FileMode0600(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(dir)
	if err := svc.Set(KeyDefaultTimeout, "100"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
}
