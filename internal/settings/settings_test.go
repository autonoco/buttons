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

func TestService_Set_Theme(t *testing.T) {
	svc := NewService(t.TempDir())

	if err := svc.Set(KeyTheme, "amber"); err != nil {
		t.Fatalf("set amber: %v", err)
	}
	st, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	v, ok := st.Theme()
	if !ok || v != "amber" {
		t.Errorf("theme = %q/%v, want amber/true", v, ok)
	}

	if err := svc.Set(KeyTheme, "unknown-theme"); err == nil {
		t.Error("set unknown theme should fail")
	}
}

func TestService_ButtonsAutoUpdateDefaultsOn(t *testing.T) {
	svc := NewService(t.TempDir())
	st, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !st.ButtonsAutoUpdateEnabled() {
		t.Fatal("buttons-auto-update should default on")
	}
}

func TestService_SetButtonsAutoUpdate(t *testing.T) {
	svc := NewService(t.TempDir())
	if err := svc.Set(KeyButtonsAutoUpdate, "false"); err != nil {
		t.Fatalf("set false: %v", err)
	}
	st, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if st.ButtonsAutoUpdateEnabled() {
		t.Fatal("buttons-auto-update should be disabled")
	}

	if err := svc.Set(KeyButtonsAutoUpdate, "true"); err != nil {
		t.Fatalf("set true: %v", err)
	}
	st, err = svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !st.ButtonsAutoUpdateEnabled() {
		t.Fatal("buttons-auto-update should be enabled")
	}

	if err := svc.Set(KeyButtonsAutoUpdate, "maybe"); err == nil {
		t.Fatal("invalid bool should fail")
	}
}

func TestService_SetCLIAutoUpdate(t *testing.T) {
	svc := NewService(t.TempDir())
	st, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if st.CLIAutoUpdateEnabled() {
		t.Fatal("cli-auto-update should default off")
	}

	if err := svc.Set(KeyCLIAutoUpdate, "true"); err != nil {
		t.Fatalf("set true: %v", err)
	}
	st, err = svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !st.CLIAutoUpdateEnabled() {
		t.Fatal("cli-auto-update should be enabled")
	}

	if err := svc.Set(KeyCLIAutoUpdate, "false"); err != nil {
		t.Fatalf("set false: %v", err)
	}
	st, err = svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if st.CLIAutoUpdateEnabled() {
		t.Fatal("cli-auto-update should be disabled")
	}

	if err := svc.Set(KeyCLIAutoUpdate, "maybe"); err == nil {
		t.Fatal("invalid bool should fail")
	}
}

func TestService_LastUpdateCheckUnix(t *testing.T) {
	svc := NewService(t.TempDir())
	if err := svc.SetLastUpdateCheckUnix(1234); err != nil {
		t.Fatalf("set last check: %v", err)
	}
	st, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := st.LastUpdateCheckUnixOrZero(); got != 1234 {
		t.Fatalf("last check = %d, want 1234", got)
	}
}

func TestService_Unset_Theme(t *testing.T) {
	svc := NewService(t.TempDir())
	if err := svc.Set(KeyTheme, "phosphor"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Unset(KeyTheme); err != nil {
		t.Fatalf("unset: %v", err)
	}
	st, _ := svc.Load()
	if _, ok := st.Theme(); ok {
		t.Error("theme should be unset")
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
