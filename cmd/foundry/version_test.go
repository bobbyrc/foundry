package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	rev := "0123456789abcdef0123"
	vcs := func(modified string) []debug.BuildSetting {
		return []debug.BuildSetting{{Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: modified}}
	}
	tests := []struct {
		name    string
		ldflags string
		bi      *debug.BuildInfo
		want    string
	}{
		{"ldflags wins", "v1.2.3", &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}, Settings: vcs("false")}, "v1.2.3"},
		{"module version", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v0.3.0"}}, "v0.3.0"},
		{"pseudo-version", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261028040911-0123456789ab"}, Settings: vcs("false")}, "dev+0123456789ab"},
		{"dirty pseudo-version", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261028040911-0123456789ab+dirty"}, Settings: vcs("true")}, "dev+0123456789ab-dirty"},
		{"tagged checkout", "dev", &debug.BuildInfo{Main: debug.Module{Version: "v0.3.0"}, Settings: vcs("false")}, "v0.3.0"},
		{"clean revision", "dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: vcs("false")}, "dev+0123456789ab"},
		{"dirty revision", "dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: vcs("true")}, "dev+0123456789ab-dirty"},
		{"short revision", "dev", &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}}, "dev+abc"},
		{"no vcs info", "dev", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "dev"},
		{"nil build info", "dev", nil, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.ldflags, tt.bi); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionCommandShowsToolchain(t *testing.T) {
	code, out, errOut := runCLI("version")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	want := fmt.Sprintf("foundry %s (%s %s/%s)\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}
