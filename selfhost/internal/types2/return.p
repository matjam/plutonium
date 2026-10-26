// Translated by go2pu from internal/types2/return.go at the go-cutover tag; edited by hand since.

// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements isTerminating.

package types2

import (
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
)

// isTerminating reports if s is a terminating statement.
// If s is labeled, label is the label name; otherwise s
// is "".
func (check *Checker) isTerminating(s ast.Stmt, label string) bool {
	switch s := s.(type) {
	default:
		panic("unreachable")

	case *ast.BadStmt, *ast.DeclStmt, *ast.EmptyStmt, *ast.SendStmt,
		*ast.IncDecStmt, *ast.AssignStmt, *ast.TaskStmt, *ast.DeferStmt,
		*ast.RangeStmt:
		// no chance

	case *ast.OrStmt:
		// Plutonium: control goes on after an or statement whose call
		// succeeds, although its clause leaves (language spec 12.1).

	case *ast.LabeledStmt:
		return check.isTerminating(s.Stmt, s.Label.Name)

	case *ast.ExprStmt:
		// calling the predeclared (possibly parenthesized) panic() function is terminating
		if call, ok := ast.Unparen(s.X).(*ast.CallExpr); ok && check.isPanic[call] {
			return true
		}

	case *ast.ReturnStmt:
		return true

	case *ast.BranchStmt:
		if s.Tok == token.GOTO || s.Tok == token.FALLTHROUGH {
			return true
		}

	case *ast.BlockStmt:
		return check.isTerminatingList(s.List, "")

	case *ast.IfStmt:
		if s.Else != nil &&
			check.isTerminating(s.Body, "") &&
			check.isTerminating(s.Else, "") {
			return true
		}

	case *ast.SwitchStmt:
		// Plutonium: a switch on an enum value that lists every member
		// needs no default case to be terminating (spec section 12.1).
		return check.isTerminatingSwitch(s.Body, label, check.exhaustive[s])

	case *ast.TypeSwitchStmt:
		// Plutonium: a switch that names every variant of a union needs
		// no default case to be terminating.
		return check.isTerminatingSwitch(s.Body, label, check.exhaustive[s])

	case *ast.SelectStmt:
		for _, s := range s.Body.List {
			cc := s.(*ast.CommClause)
			if !check.isTerminatingList(cc.Body, "") || hasBreakList(cc.Body, label, true) {
				return false
			}

		}
		return true

	case *ast.ForStmt:
		if s.Cond == nil && !hasBreak(s.Body, label, true) {
			return true
		}
	}

	return false
}

func (check *Checker) isTerminatingList(list []ast.Stmt, label string) bool {
	// trailing empty statements are permitted - skip them
	for i := len(list) - 1; i >= 0; i-- {
		if _, ok := list[i].(*ast.EmptyStmt); !ok {
			return check.isTerminating(list[i], label)
		}
	}
	return false // all statements are empty
}

func (check *Checker) isTerminatingSwitch(body *ast.BlockStmt, label string, exhaustive bool) bool {
	hasDefault := exhaustive
	for _, s := range body.List {
		cc := s.(*ast.CaseClause)
		if cc.List == nil {
			hasDefault = true
		}
		if !check.isTerminatingList(cc.Body, "") || hasBreakList(cc.Body, label, true) {
			return false
		}
	}
	return hasDefault
}

// TODO(gri) For nested breakable statements, the current implementation of hasBreak
// will traverse the same subtree repeatedly, once for each label. Replace
// with a single-pass label/break matching phase.

// hasBreak reports if s is or contains a break statement
// referring to the label-ed statement or implicit-ly the
// closest outer breakable statement.
func hasBreak(s ast.Stmt, label string, implicit bool) bool {
	switch s := s.(type) {
	default:
		panic("unreachable")

	case *ast.BadStmt, *ast.DeclStmt, *ast.EmptyStmt, *ast.ExprStmt,
		*ast.SendStmt, *ast.IncDecStmt, *ast.AssignStmt, *ast.TaskStmt,
		*ast.DeferStmt, *ast.ReturnStmt:
		// no chance

	case *ast.LabeledStmt:
		return hasBreak(s.Stmt, label, implicit)

	case *ast.OrStmt:
		// Plutonium: the clause of an or statement is at the place of the
		// statement (language spec 11.27).
		return hasBreak(s.Clause, label, implicit)

	case *ast.BranchStmt:
		if s.Tok == token.BREAK {
			if s.Label == nil {
				return implicit
			}
			if s.Label.Name == label {
				return true
			}
		}

	case *ast.BlockStmt:
		return hasBreakList(s.List, label, implicit)

	case *ast.IfStmt:
		if hasBreakInit(s.Init, label, implicit) || hasBreak(s.Body, label, implicit) ||
			s.Else != nil && hasBreak(s.Else, label, implicit) {
			return true
		}

	case *ast.CaseClause:
		return hasBreakList(s.Body, label, implicit)

	case *ast.SwitchStmt:
		if hasBreakInit(s.Init, label, implicit) || label != "" && hasBreak(s.Body, label, false) {
			return true
		}

	case *ast.TypeSwitchStmt:
		if hasBreakInit(s.Init, label, implicit) || label != "" && hasBreak(s.Body, label, false) {
			return true
		}

	case *ast.CommClause:
		return hasBreakList(s.Body, label, implicit)

	case *ast.SelectStmt:
		if label != "" && hasBreak(s.Body, label, false) {
			return true
		}

	case *ast.ForStmt:
		if hasBreakInit(s.Init, label, implicit) || label != "" && hasBreak(s.Body, label, false) {
			return true
		}

	case *ast.RangeStmt:
		if label != "" && hasBreak(s.Body, label, false) {
			return true
		}
	}

	return false
}

// hasBreakInit reports if the init statement s of an if, switch or for
// statement is an or statement whose clause has a break referring to the
// label-ed statement or implicit-ly the closest breakable statement that
// encloses the whole statement (language spec 11.27).
func hasBreakInit(s ast.Stmt, label string, implicit bool) bool {
	or, _ := s.(*ast.OrStmt)
	return or != nil && hasBreak(or.Clause, label, implicit)
}

func hasBreakList(list []ast.Stmt, label string, implicit bool) bool {
	for _, s := range list {
		if hasBreak(s, label, implicit) {
			return true
		}
	}
	return false
}
