package cli

import (
	"fmt"
	"runtime/debug"
)

// version is set at release build time with -ldflags "-X .../internal/cli.version=v0.1.0".
var version = "dev"

// Version is gpucost's version: the release it was built as, the module version "go install" recorded, or "dev".
func Version() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

func toolName() string { return fmt.Sprintf("gpucost %s", Version()) }
