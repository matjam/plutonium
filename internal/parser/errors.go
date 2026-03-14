package parser

import (
	"fmt"
	"sort"

	"github.com/matjam/plutonium/internal/token"
)

// Error represents a parse error.
type Error struct {
	Pos token.Position
	Msg string
}

func (e Error) Error() string {
	if e.Pos.IsValid() {
		return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
	}
	return e.Msg
}

// ErrorList is a list of parse errors.
type ErrorList []*Error

func (el ErrorList) Error() string {
	switch len(el) {
	case 0:
		return "no errors"
	case 1:
		return el[0].Error()
	default:
		return fmt.Sprintf("%s (and %d more errors)", el[0], len(el)-1)
	}
}

func (el ErrorList) Err() error {
	if len(el) == 0 {
		return nil
	}
	return el
}

func (el ErrorList) Len() int           { return len(el) }
func (el ErrorList) Less(i, j int) bool { return el[i].Msg < el[j].Msg }
func (el ErrorList) Swap(i, j int)      { el[i], el[j] = el[j], el[i] }

func (el *ErrorList) Add(pos token.Position, msg string) {
	*el = append(*el, &Error{pos, msg})
}

func (el ErrorList) Sort() {
	sort.Stable(el)
}
