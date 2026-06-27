package cmd

import "testing"

func TestIsLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1":       true,
		"127.5.5.5":       true, // all of 127.0.0.0/8 is loopback
		"::1":             true,
		"0:0:0:0:0:0:0:1": true, // expanded ::1
		"localhost":       true,
		"0.0.0.0":         false,
		"::":              false,
		"192.168.1.10":    false,
		"127.evil.com":    false, // not an IP — must NOT count as loopback
		"example.com":     false,
	} {
		if got := isLoopbackHost(host); got != want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}
