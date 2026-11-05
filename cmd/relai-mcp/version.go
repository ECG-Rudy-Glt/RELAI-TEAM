package main

import "runtime/debug"

// resolveVersion is the version to report. A release binary has it injected at build time
// (-ldflags "-X main.version=..."); a binary installed with `go install module@vX.Y.Z` carries it in its build
// information instead; anything else is a development build.
func resolveVersion(injected string, bi *debug.BuildInfo, ok bool) string {
	if injected != "dev" || !ok || bi == nil {
		return injected
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return injected
}

func init() {
	bi, ok := debug.ReadBuildInfo()
	version = resolveVersion(version, bi, ok)
}
