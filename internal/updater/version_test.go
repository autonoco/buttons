package updater

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "1.2.3", 0},
		{"1.2.3-dirty", "1.2.3", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.10.0", "1.9.9", 1},
		{"1.2", "1.2.0", 0},
		{"1", "2", -1},
		{"dev", "v1.0.0", 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Fatalf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
