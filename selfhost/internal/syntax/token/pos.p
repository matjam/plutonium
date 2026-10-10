// Package token is a hand-written stand-in for the part of
// internal/syntax/token that internal/ssa uses (Pos), until go2pu
// translates the whole package. The definitions match
// internal/syntax/token/position.go.
package token

// Pos is a compact encoding of a source position within a file set.
type Pos int

// NoPos is the zero value for Pos; there is no file and line information
// associated with it.
const NoPos Pos = 0

// IsValid reports whether the position is valid.
func (p Pos) IsValid() bool { return p != NoPos }
