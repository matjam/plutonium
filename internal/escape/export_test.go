package escape

import "github.com/matjam/plutonium/internal/load"

// CheckUserResultHolds is Check with //pu:resultholds allowed outside the
// standard library, so that tests can exercise the directive.
func CheckUserResultHolds(prog *load.Program) error { return check(prog, true) }
