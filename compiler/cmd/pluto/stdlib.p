package main

// The Go build embeds the standard library in the compiler (stdlib.go);
// Plutonium has no way to embed a directory yet, so this hand-written file
// finds the library on disk: $PLUTOROOT, or a lib directory beside the
// executable or one level above it (self-hosting plan, section 8).

import (
	"compiler/compat"
	"compiler/compat/fs"
	"pluto/os"
	"pluto/path/filepath"
)

// stdlib returns the standard library: the directory stdlibDir names, or
// the current directory's lib if there is none (the loader then reports
// the packages it cannot find).
func stdlib() fs.FS {
	if dir := stdlibDir(); dir != "" {
		return fs.DirFS(dir)
	}
	return fs.DirFS("lib")
}

// stdlibDir returns the absolute directory of the standard library: that
// of $PLUTOROOT if set, otherwise the first of exe/../lib and exe/lib that
// holds pluto/rt, where exe is the directory of the running compiler.
func stdlibDir() string {
	if root := compat.Getenv("PLUTOROOT"); root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			return abs
		}
		return root
	}
	exe, err := os.ReadlinkLossy("/proc/self/exe")
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	for _, cand := range []string{filepath.Join(dir, "..", "lib"), filepath.Join(dir, "lib")} {
		if _, err := os.Stat(filepath.Join(cand, "pluto", "rt")); err == nil {
			return cand
		}
	}
	return ""
}
