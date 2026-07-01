package updater

import (
	"strconv"
	"strings"
)

// NormalizeVersion trims release adornments so "v1.2.3" and "1.2.3" compare
// the same. Build-local "-dirty" suffixes should not block an update check.
func NormalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(v), "-dirty"), "v")
}

// CompareVersions compares dotted numeric versions with a small fallback for
// dev builds and non-semver tags. It returns -1, 0, or 1.
func CompareVersions(a, b string) int {
	a = NormalizeVersion(a)
	b = NormalizeVersion(b)
	if a == b {
		return 0
	}

	as, aOK := numericSegments(a)
	bs, bOK := numericSegments(b)
	if aOK && bOK {
		n := len(as)
		if len(bs) > n {
			n = len(bs)
		}
		for i := 0; i < n; i++ {
			av, bv := 0, 0
			if i < len(as) {
				av = as[i]
			}
			if i < len(bs) {
				bv = bs[i]
			}
			if av < bv {
				return -1
			}
			if av > bv {
				return 1
			}
		}
		return 0
	}

	if a < b {
		return -1
	}
	return 1
}

func numericSegments(v string) ([]int, bool) {
	if v == "" {
		return nil, false
	}
	base := strings.SplitN(v, "-", 2)[0]
	parts := strings.Split(base, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
