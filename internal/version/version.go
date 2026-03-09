package version

import "runtime/debug"

// commit is set at build time via -ldflags:
//
//	-X github.com/kazz187/taskguild/internal/version.commit=$(git rev-parse --short HEAD)
//
// When not set (e.g. `go install` without ldflags), Commit() falls back to
// runtime/debug.ReadBuildInfo() which includes VCS revision since Go 1.18.
var commit string

// Commit returns the full git commit hash embedded in the binary.
// It first checks the ldflags-set value, then falls back to
// debug.ReadBuildInfo's vcs.revision, and finally returns "unknown".
func Commit() string {
	if commit != "" {
		return commit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return "unknown"
}

// Short returns the first 8 characters of the commit hash,
// suitable for display in logs and protocol messages.
func Short() string {
	c := Commit()
	if len(c) > 8 {
		return c[:8]
	}
	return c
}
