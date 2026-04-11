package button

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultMaxResponseBytes is the fallback cap on HTTP response body size
// for URL buttons that do not declare their own MaxResponseBytes. 10 MB
// is a reasonable ceiling for typical API responses (JSON payloads,
// webhook bodies) without being so small it truncates legitimate
// large-page queries.
const DefaultMaxResponseBytes int64 = 10 << 20 // 10 MB

// MaxAllowedResponseBytes is the absolute ceiling we will accept in a
// button spec. Prevents a user from accidentally declaring a value so
// large it effectively disables the OOM guard. 2 GB covers every
// realistic streaming-API use case while still fitting in an int on
// any platform we care about.
const MaxAllowedResponseBytes int64 = 2 << 30 // 2 GB

// ParseSize parses a human-friendly size string into bytes.
//
// Accepted forms:
//
//	"1024"      → 1024 bytes
//	"1024B"     → 1024 bytes
//	"10K"/"10KB" → 10 × 1024 bytes
//	"10M"/"10MB" → 10 × 1024² bytes
//	"1G"/"1GB"   → 1 × 1024³ bytes
//
// Units are case-insensitive. Whitespace between the number and unit
// is allowed. Negative numbers, zero, and values above MaxAllowedResponseBytes
// are rejected with a descriptive error.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("size cannot be empty")
	}

	// Walk to find where the numeric prefix ends.
	i := 0
	for i < len(s) && (s[i] == '-' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid size %q: must start with a number", s)
	}

	numStr := s[:i]
	unitStr := strings.ToUpper(strings.TrimSpace(s[i:]))

	n, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("size must be positive, got %d", n)
	}

	var mult int64
	switch unitStr {
	case "", "B":
		mult = 1
	case "K", "KB":
		mult = 1 << 10
	case "M", "MB":
		mult = 1 << 20
	case "G", "GB":
		mult = 1 << 30
	default:
		return 0, fmt.Errorf("invalid size unit %q (use B, K/KB, M/MB, or G/GB)", unitStr)
	}

	// Overflow check before multiplying.
	if n > MaxAllowedResponseBytes/mult {
		return 0, fmt.Errorf("size exceeds maximum allowed (%s)", FormatSize(MaxAllowedResponseBytes))
	}

	return n * mult, nil
}

// FormatSize formats a byte count as a short human-readable string
// using binary units (KB = 1024 bytes, not 1000).
func FormatSize(b int64) string {
	switch {
	case b < 1<<10:
		return fmt.Sprintf("%d B", b)
	case b < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	case b < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	}
}

// ResolveMaxResponseBytes returns the effective response body cap for a
// button: the spec's declared value if positive, otherwise the default.
// Treating zero as "use default" keeps old spec files that predate this
// field working unchanged.
func ResolveMaxResponseBytes(declared int64) int64 {
	if declared <= 0 {
		return DefaultMaxResponseBytes
	}
	return declared
}
