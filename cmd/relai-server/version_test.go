package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	mod := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}
	dev := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	cases := []struct {
		name, injected string
		bi             *debug.BuildInfo
		ok             bool
		want           string
	}{
		{"injected wins", "v9.9.9", mod, true, "v9.9.9"},
		{"go install", "dev", mod, true, "v1.2.3"},
		{"local build", "dev", dev, true, "dev"},
		{"no build info", "dev", nil, false, "dev"},
	}
	for _, c := range cases {
		if got := resolveVersion(c.injected, c.bi, c.ok); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
