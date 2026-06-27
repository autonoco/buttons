package cmd

import (
	"context"
	"testing"

	"github.com/autonoco/buttons/internal/button"
)

func TestSplitButtonNames(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"a,b,c"}, []string{"a", "b", "c"}},
		{[]string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{[]string{"a, b ,c", "d"}, []string{"a", "b", "c", "d"}},
		{[]string{",,"}, nil},
	}
	for _, c := range cases {
		got := splitButtonNames(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("splitButtonNames(%v) = %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("splitButtonNames(%v) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

func TestSmashPressRunsAndRecords(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	svc := button.NewService()
	if _, err := svc.Create(button.CreateOpts{Name: "echo-ok", Code: "echo hi", Runtime: "shell", TimeoutSeconds: 10}); err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := smashPress(context.Background(), "echo-ok", map[string]string{}, 0)
	if err != nil {
		t.Fatalf("smashPress: %v", err)
	}
	if res.Status != "ok" {
		t.Fatalf("status = %q, stderr = %q", res.Status, res.Stderr)
	}
}

func TestSmashPressMissingButton(t *testing.T) {
	t.Setenv("BUTTONS_HOME", t.TempDir())
	if _, err := smashPress(context.Background(), "nope", map[string]string{}, 0); err == nil {
		t.Fatal("expected error for missing button")
	}
}
