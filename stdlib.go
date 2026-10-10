// Package plutonium holds the Plutonium standard library sources, embedded
// so that the compiler binary is self-contained.
package plutonium

import (
	"embed"
	"io/fs"
)

//go:embed all:lib
var lib embed.FS

// Stdlib returns the standard library as a file system whose top-level
// directory is "pluto".
func Stdlib() fs.FS {
	sub, err := fs.Sub(lib, "lib")
	if err != nil {
		panic(err)
	}
	return sub
}
