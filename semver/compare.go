// Package semver is a small dotted numeric compare for minCoreVersion gates.
package semver

import (
	"strconv"
	"strings"
)

// AtLeast reports whether core >= min (empty min is always true).
func AtLeast(core, min string) bool {
	core = strings.TrimPrefix(strings.TrimSpace(core), "v")
	min = strings.TrimPrefix(strings.TrimSpace(min), "v")
	if min == "" {
		return true
	}
	if core == "" {
		return false
	}
	return cmpDotted(core, min) >= 0
}

func cmpDotted(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
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
