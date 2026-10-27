// Translated by go2pu from internal/escape2/sched/api.go at the go-cutover tag; edited by hand since.

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
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/solve"
	"compiler/internal/escape2/summary"
	"compiler/internal/types2"
)

// Facts are the stage B facts.
type Facts interface {
	// Candidates are the functions a dynamic call in f may reach.
	Candidates(f *eir.Func, c *eir.Call) []eir.FuncID
	// HeapOnly reports whether field index field of struct type T holds
	// only heap pointers.
	HeapOnly(T types2.Type, field int) bool
	// Instantiated reports the type arguments a type parameter is
	// instantiated with anywhere in the program, and whether that set is
	// known. known false means open: the instantiations cannot be bounded
	// (a type parameter instantiated with itself through a recursive
	// chain), and a client must treat it as any type, never as none.
	Instantiated(tp *types2.TypeParam) (types []types2.Type, known bool)
}

// Stats are the measurements the plan asks for (section (e), (h)).
type Stats struct {
	Funcs        int
	SCCs         int
	LargestSCC   int // functions in the largest SCC
	PeakArena    int // bytes, the most one SCC's arena held
	SummaryBytes int // bytes, all final summaries
	Iterations   int // solves of functions, over all SCCs

	// The D44 cache (Options.Cache), counted in SCCs: accepted from the
	// file after one checking solve each; missed (no entry for every
	// function); rejected (the check failed, so the SCC was solved from
	// bottom); redone (solved again from bottom because a function in or
	// above it has a finding and it depends on a summary from the file).
	CacheAccepted, CacheMissed, CacheRejected, CacheRedone int
}

// Result is the outcome of the analysis of a program.
type Result struct {
	Summaries []*summary.Summary // by eir.FuncID
	Findings  []solve.Finding    // sorted by position
	Stats     Stats
	// Env is the final environment of the run: every summary final, its
	// Paths, the virtual per-signature functions.
	Env solve.Env
}

// Options configure Run.
type Options struct {
	// Contract gives the summaries of contract-table entries; nil means
	// those of internal/escape2/contract.
	Contract func(name string) (*summary.Summary, bool)
	// OnSCC, if set, is called after each SCC is final, with its
	// functions and measurements, before its arena is reset.
	OnSCC func(scc []eir.FuncID, st SCCStats)
	// Cache, if set, is the checked summary cache of decision D44
	// (internal/escape2/cache, cache.go here): it offers summaries from a
	// file, which the driver checks and never trusts.
	Cache Cache
}

// SCCStats are the measurements of one SCC.
type SCCStats struct {
	Funcs  int
	Solves int
	// ArenaBytes and SetBytes are the memory the arena and the set table
	// held after the SCC's solves. The workspace is reset after every
	// solve and keeps its chunks, so these grow only when a solve needs
	// more than every solve before it: the largest is the peak.
	ArenaBytes int
	SetBytes   int
	// Cache is what the D44 cache did for the SCC.
	Cache CacheOutcome
	// RetainedPeak is the most the kept solver states of the SCC's members
	// held at once, in bytes, and Resumes the solves that resumed one
	// (incremental re-solving, incr.go).
	RetainedPeak int
	Resumes      int
	// Unfounded counts the entries of the SCC's final summaries that no
	// derivation of finite depth supports (reasons.go).
	Unfounded int
}
