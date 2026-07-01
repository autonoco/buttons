package button

import (
	"fmt"
	"strconv"
	"strings"
)

const InitialContentVersion = "1"

// NextVersion bumps the simple button content version used for registry
// publishes: 1, 2, 3, and so on.
func NextVersion(version string) (string, error) {
	n, err := strconv.Atoi(strings.TrimSpace(version))
	if err != nil || n < 1 {
		return "", fmt.Errorf("version %q is not a positive integer", version)
	}
	return strconv.Itoa(n + 1), nil
}
