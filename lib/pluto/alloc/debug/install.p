//pu:build linux && allocdebug

package debug

import "pluto/alloc"

// "pluto build -alloc=debug" sets the allocdebug tag and loads this package
// right after pluto/alloc, so this runs before any other package's
// initialisation can allocate.
func init() {
	alloc.SetDefault(New(EnvOptions()))
}
