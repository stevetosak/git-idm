package version

import (
	"runtime/debug"
	"strings"
)

const Dev = "dev"

var Version = ""

func Current() string {
	if v := normalize(Version); v != "" {
		return v
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := normalize(info.Main.Version); v != "" && v != "(devel)" {
			return v
		}
	}
	return Dev
}

func normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "(devel)" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}
