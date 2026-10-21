// Package sched computes the whole-program facts of stage B and schedules the
// solver bottom-up over the SCCs of the call graph (plan
// docs/plans/2026-10-20-escape-redesign.md, section (b)).
//
// Stage B, computed once before any summary, so that no fact a summary reads
// changes under it: the instantiation closure; the converted-type set, with a
// canonical type interner (identity, type parameters qualified by their
// declaring object, no text keys); candidate sets per (method name,
// signature) from the method sets of converted types and their promoted
// wrappers, and per signature from the func values; heap-only field classes
// (invariant I6) as a greatest fixed point over the functions' source facts.
//
// The call graph has an edge f→g for each static call, f→L and f→T.M for the
// closures f builds and types it converts, and f→D(M) for a dynamic call
// through a dispatch node with edges to every candidate. SCCs (Tarjan) are
// solved callees first, each in its own arena, reset when the SCC is final.
//
// The candidate machinery sits behind Facts.Candidates so that either answer
// to the open question of a rule for dynamic calls plugs in without changing
// the solver.
//
// This file is the interface other packages build against: change it only by
// agreement (internal/escape2 package comment).
package sched

import (
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/solve"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/types2"
)

// Facts are the stage B facts.
type Facts interface {
	// Candidates are the functions a dynamic call in f may reach.
	Candidates(f *eir.Func, c *eir.Call) []eir.FuncID
	// HeapOnly reports whether field index field of struct type T holds
	// only heap pointers.
	HeapOnly(T types2.Type, field int) bool
	// Instantiated reports the type arguments a type parameter is
	// instantiated with anywhere in the program.
	Instantiated(tp *types2.TypeParam) []types2.Type
}

// Stats are the measurements the plan asks for (section (e), (h)).
type Stats struct {
	Funcs        int
	SCCs         int
	LargestSCC   int // functions in the largest SCC
	PeakArena    int // bytes, the most one SCC's arena held
	SummaryBytes int // bytes, all final summaries
	Iterations   int // solves of functions, over all SCCs
}

// Result is the outcome of the analysis of a program.
type Result struct {
	Summaries []*summary.Summary // by eir.FuncID
	Findings  []solve.Finding    // sorted by position
	Stats     Stats
}
