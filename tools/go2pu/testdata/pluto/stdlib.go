package main

import (
	"io/fs"
	"os"
	"path/filepath"

	plutonium "github.com/matjam/plutonium"
)

// stdlib returns the standard library: $PLUTOROOT if set (a directory
// containing pluto/), otherwise the copy embedded in the compiler. The
// translation (stdlib.p) has no embedded copy: it looks for the library
// next to its executable.
func stdlib() fs.FS {
	if root := os.Getenv("PLUTOROOT"); root != "" {
		return os.DirFS(root)
	}
	return plutonium.Stdlib()
}

// stdlibDir returns the absolute directory of $PLUTOROOT, or "" if it is
// not set.
func stdlibDir() string {
	if root := os.Getenv("PLUTOROOT"); root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			return abs
		}
	}
	return ""
}
