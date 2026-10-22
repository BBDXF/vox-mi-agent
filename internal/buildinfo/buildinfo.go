// Package buildinfo holds build metadata injected via -ldflags -X.
package buildinfo

import "runtime"

// Set at link time: -X github.com/andy/vox-mi-agent/internal/buildinfo.Version=...
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String returns a multi-line human summary.
func String() string {
	return "version: " + Version + "\ncommit: " + Commit + "\nbuilt: " + Date +
		"\ngo: " + runtime.Version() + "\nos/arch: " + runtime.GOOS + "/" + runtime.GOARCH
}
