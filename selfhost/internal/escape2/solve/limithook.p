// Translated by go2pu from internal/escape2/solve/limithook.go at the go-cutover tag; edited by hand since.

package solve

// SetLimits sets the limits a solve is checked against (MaxFuncArena and
// MaxFuncSteps), for tests of the scheduler that need functions past them,
// and returns a function that restores the previous ones. It is not for
// the compiler.
func SetLimits(arena, steps int) (restore func()) {
	saved := [2]int{maxFuncArena, maxFuncSteps}
	maxFuncArena, maxFuncSteps = arena, steps
	return func() { maxFuncArena, maxFuncSteps = saved[0], saved[1] }
}
