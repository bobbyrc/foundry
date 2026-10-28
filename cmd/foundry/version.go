package main

import (
	"runtime/debug"
	"strings"
)

func init() {
	if bi, ok := debug.ReadBuildInfo(); ok {
		version = resolveVersion(version, bi)
	}
}

// resolveVersion returns the version to report. A version set by -ldflags (anything but "dev")
// wins (an explicit -X main.version=dev is indistinguishable from the default); otherwise it falls
// back to a tagged module version, then to dev+<short vcs revision>.
func resolveVersion(ldflags string, bi *debug.BuildInfo) string {
	if ldflags != "dev" || bi == nil {
		return ldflags
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
	short := rev[:min(len(rev), 12)]
	// Go stamps checkout builds with a pseudo-version (v0.0.0-<time>-<short sha>[+dirty]); that is
	// not a release, so only a version that does not embed the revision is reported as is.
	if v := bi.Main.Version; v != "" && v != "(devel)" && (short == "" || !strings.Contains(v, short)) {
		return v
	}
	if rev == "" {
		return ldflags
	}
	v := "dev+" + short
	if dirty {
		v += "-dirty"
	}
	return v
}
