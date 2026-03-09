// Package version provides build version information.
// Values are injected at build time via ldflags.
package version

import (
	"fmt"
	"runtime"
)

// Build information, set via ldflags
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// String returns a formatted version string.
func String() string {
	return fmt.Sprintf("%s (commit: %s, built: %s, %s/%s)",
		Version, GitCommit, BuildDate, runtime.GOOS, runtime.GOARCH)
}

// Short returns just the version.
func Short() string {
	return Version
}
