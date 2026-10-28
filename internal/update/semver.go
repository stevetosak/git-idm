package update

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	tagPattern    = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)
	pseudoPattern = regexp.MustCompile(`[0-9]{14}-[0-9a-f]{12}$`)
)

type semver struct {
	core [3]int
	pre  []string
}

func ValidTag(tag string) bool {
	return tagPattern.MatchString(tag)
}

func IsRelease(v string) bool {
	s, ok := parse(v)
	return ok && !pseudoPattern.MatchString(strings.Join(s.pre, "."))
}

func Newer(latest, current string) bool {
	if !ValidTag(latest) || !IsRelease(current) {
		return false
	}
	l, _ := parse(latest)
	c, _ := parse(current)
	return compare(l, c) > 0
}

func parse(v string) (semver, bool) {
	v, _, _ = strings.Cut(v, "+")
	rest, ok := strings.CutPrefix(v, "v")
	if !ok {
		return semver{}, false
	}
	core, pre, hasPre := strings.Cut(rest, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var s semver
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		s.core[i] = n
	}
	if hasPre {
		if pre == "" {
			return semver{}, false
		}
		s.pre = strings.Split(pre, ".")
	}
	return s, true
}

func compare(a, b semver) int {
	for i := range a.core {
		if a.core[i] != b.core[i] {
			return sign(a.core[i] - b.core[i])
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := comparePre(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return sign(len(a.pre) - len(b.pre))
}

func comparePre(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return sign(an - bn)
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}
