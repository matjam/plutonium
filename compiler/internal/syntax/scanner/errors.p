// Translated by go2pu from internal/syntax/scanner/errors.go at the go-cutover tag; edited by hand since.

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package scanner

import (
	"pluto/cmp"
	"pluto/fmt"
	"pluto/io"
	"pluto/slices"
	"compiler/compat"
	"compiler/internal/syntax/token"
	"compiler/pu"
)

// In an [ErrorList], an error is represented by an *Error.
// The position Pos, if valid, points to the beginning of
// the offending token, and the error condition is described
// by Msg.
type Error struct {
	Pos token.Position
	Msg string
}

var _ error = (*Error)(nil) // *Error (not Error) is the correct type in error.Is tests.

// Error implements the error interface.
func (e Error) Error() string {
	if e.Pos.Filename != "" || e.Pos.IsValid() {
		// don't print "<unknown position>"
		// TODO(gri) reconsider the semantics of Position.IsValid
		return compat.Concat(e.Pos.String(), ": ", e.Msg)
	}
	return e.Msg
}

// ErrorList is a list of *Errors.
// The zero value for an ErrorList is an empty ErrorList ready to use.
type ErrorList []*Error

// Add adds an [Error] with given position and error message to an [ErrorList].
func (p *ErrorList) Add(pos token.Position, msg string) {
	*p = ErrorList(compat.Append(*p, pu.New(Error{pos, msg})))
}

// Reset resets an [ErrorList] to no errors.
func (p *ErrorList) Reset() { *p = (*p)[0:0] }

// [ErrorList] implements the sort Interface.
func (p ErrorList) Len() int      { return len(p) }
func (p ErrorList) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

func (p ErrorList) Less(i, j int) bool { return compareErrors(p[i], p[j]) < 0 }

// compareErrors orders errors by position, then message.
func compareErrors(a, b *Error) int {
	e := &a.Pos
	f := &b.Pos
	// Note that it is not sufficient to simply compare file offsets because
	// the offsets do not reflect modified line information (through //line
	// comments).
	if c := cmp.Compare(e.Filename, f.Filename); c != 0 {
		return c
	}
	if c := cmp.Compare(e.Line, f.Line); c != 0 {
		return c
	}
	if c := cmp.Compare(e.Column, f.Column); c != 0 {
		return c
	}
	return cmp.Compare(a.Msg, b.Msg)
}

// Sort sorts an [ErrorList]. *[Error] entries are sorted by position,
// other errors are sorted by error message, and before any *[Error]
// entry.
func (p ErrorList) Sort() {
	slices.SortFunc(p, compareErrors)
}

// RemoveMultiples sorts an [ErrorList] and removes all but the first error per line.
func (p *ErrorList) RemoveMultiples() {
	p.Sort()
	var last token.Position // initial last.Line is != any legal error line
	i := 0
	for _, e := range *p {
		if e.Pos.Filename != last.Filename || e.Pos.Line != last.Line {
			last = e.Pos
			(*p)[i] = e
			i++
		}
	}
	*p = (*p)[0:i]
}

// An [ErrorList] implements the error interface.
func (p ErrorList) Error() string {
	switch len(p) {
	case 0:
		return "no errors"
	case 1:
		return p[0].Error()
	}
	return compat.Sprintf("%s (and %d more errors)", p[0], len(p)-1)
}

// Err returns an error equivalent to this error list: a *ErrorList, since a
// pointer fits an interface's data word (a Plutonium error is not boxed in
// the frame that returns it). If the list is empty, Err returns nil.
func (gop ErrorList) Err() error {
	p := pu.New(gop)

	if len((*p)) == 0 {
		return nil
	}
	return p
}

// PrintError is a utility function that prints a list of errors to w,
// one error per line, if the err parameter is an [ErrorList] or a
// *[ErrorList]. Otherwise it prints the err string.
func PrintError(w io.Writer, err error) {
	if list, ok := err.(*ErrorList); ok {
		for _, e := range *list {
			_, _ = fmt.Fprintf(w, "%s\n", e)
		}
	} else if err != nil {
		_, _ = fmt.Fprintf(w, "%s\n", err)
	}
}
