package version

import (
	"fmt"
	"runtime/debug"
)

// commit is injected at build time via:
//
//	go build -ldflags "-X github.com/grocky/squares/internal/version.commit=<sha>"
var commit string

func Get() string {
	// Prefer the ldflag-injected value (reliable in Docker/CI builds)
	if commit != "" {
		return commit
	}

	// Fall back to VCS info embedded by the Go toolchain (works for local builds)
	var revision string
	var modified bool

	bi, ok := debug.ReadBuildInfo()
	if ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					modified = true
				}
			}
		}
	}

	if revision == "" {
		return "unavailable"
	}

	if modified {
		return fmt.Sprintf("%s-dirty", revision)
	}

	return revision
}
