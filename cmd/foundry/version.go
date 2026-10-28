package main

import (
	"runtime/debug"
)

func init() {
	if bi, ok := debug.ReadBuildInfo(); ok {
		version = resolveVersion(version, bi)
	}
}

// resolveVersion returns the version to report. A version set by -ldflags (anything but "dev")
// wins; otherwise it falls back to the module version, then to dev+<short vcs revision>.
func resolveVersion(ldflags string, bi *debug.BuildInfo) string {
	if ldflags != "dev" || bi == nil {
		return ldflags
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev string
	var dirty bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return ldflags
	}
	v := "dev+" + rev[:min(len(rev), 12)]
	if dirty {
		v += "-dirty"
	}
	return v
}
