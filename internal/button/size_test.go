package button

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		// Plain bytes.
		{"1", 1, false},
		{"1024", 1024, false},
		{"1024B", 1024, false},
		{"1024b", 1024, false},

		// Kilobytes.
		{"1K", 1 << 10, false},
		{"1KB", 1 << 10, false},
		{"10kb", 10 << 10, false},

		// Megabytes.
		{"1M", 1 << 20, false},
		{"10M", 10 << 20, false},
		{"100MB", 100 << 20, false},

		// Gigabytes.
		{"1G", 1 << 30, false},
		{"2GB", 2 << 30, false},

		// Whitespace tolerance.
		{" 10 M ", 10 << 20, false},
		{"5 MB", 5 << 20, false},

		// Invalid inputs.
		{"", 0, true},                // empty
		{"abc", 0, true},             // no number
		{"-1", 0, true},              // negative
		{"0", 0, true},               // zero
		{"10X", 0, true},             // bogus unit
		{"10TB", 0, true},            // unsupported unit
		{"999999999999999", 0, true}, // overflow vs MaxAllowedResponseBytes
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1 << 20, "1.0 MB"},
		{10 << 20, "10.0 MB"},
		{1 << 30, "1.00 GB"},
		{2 << 30, "2.00 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := FormatSize(tt.input); got != tt.want {
				t.Errorf("FormatSize(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveMaxResponseBytes(t *testing.T) {
	tests := []struct {
		declared int64
		want     int64
	}{
		{0, DefaultMaxResponseBytes},
		{-1, DefaultMaxResponseBytes},
		{1 << 20, 1 << 20},
		{100 << 20, 100 << 20},
	}

	for _, tt := range tests {
		t.Run(FormatSize(tt.declared), func(t *testing.T) {
			if got := ResolveMaxResponseBytes(tt.declared); got != tt.want {
				t.Errorf("ResolveMaxResponseBytes(%d) = %d, want %d", tt.declared, got, tt.want)
			}
		})
	}
}
