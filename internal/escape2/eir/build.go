package eir

import (
	"errors"

	"github.com/matjam/plutonium/internal/load"
)

// Build desugars every function of a loaded program into EIR: declared
// functions and methods (generic ones once, by origin), literals, range
// bodies, package initializers and the synthesized functions the lowerer
// emits. A construct the desugarer does not know is an internal compiler
// error, never a silent omission.
//
// This is the interface stub; the desugarer replaces it.
func Build(prog *load.Program) (*Program, error) {
	return nil, errors.New("internal compiler error: escape2: the EIR desugarer is not built yet")
}
