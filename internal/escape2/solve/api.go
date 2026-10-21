// Package solve is the per-function solver of the escape analysis (plan
// docs/plans/2026-10-20-escape-redesign.md, sections (a) and (d)): inclusion
// constraints (Andersen style) over a function's EIR registers and object
// fields, solved with a worklist and difference propagation, copy cycles
// collapsed online. Loads, stores and calls are complex constraints that fire
// on the delta of their operands' sets. It produces the function's summary
// and its findings: every way a pointer to a frame object reaches a result,
// heap or package memory, caller memory, a channel, a task, or a place of a
// longer lifetime (eir.Lifetime) than the object (invariant I5).
//
// The rules, one per EIR instruction kind, are written in this package's
// rules table (rules.go) for reviewers and for the reference solver
// (internal/escape2/refsolve), which applies the same rules naively.
//
// Dynamic calls are resolved by union over Env.Candidates (stage 2). Tags,
// deferred dispatch and obligations (stage 3) wait for the owner's decision
// on a rule for dynamic calls.
//
// This file is the interface other packages build against: change it only by
// agreement (internal/escape2 package comment).
package solve

import (
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Env is what the solver reads of the rest of the program. Everything it
// returns is fixed while the function is solved, except the summaries of
// functions of the same SCC, which grow monotonically between solves.
type Env interface {
	// Program is the whole program in EIR.
	Program() *eir.Program
	// Paths is the program-wide path interner.
	Paths() domain.Paths
	// Summary is the current summary of a callee: final for callees in
	// earlier SCCs, the latest iterate for callees of the same SCC (nil:
	// nothing yet, the bottom summary).
	Summary(id eir.FuncID) *summary.Summary
	// Candidates are the functions a dynamic call may reach (plan
	// invariant I2): for CallIface and CallTypeParam the methods of that
	// name and signature, for CallFunc the func values of that signature.
	Candidates(f *eir.Func, c *eir.Call) []eir.FuncID
	// Contract is the summary of an entry of the contract table, and
	// whether there is one. A contract call without an entry is an
	// internal compiler error.
	Contract(name string) (*summary.Summary, bool)
	// HeapOnly reports whether field index field of struct type T holds
	// only heap pointers (plan invariant I6).
	HeapOnly(T types2.Type, field int) bool
}

// Workspace is the memory one solve works in: the SCC arena and its set
// table, reset by the scheduler when the SCC is final.
type Workspace struct {
	Arena domain.Arena
	Sets  domain.Sets
}

// Finding is an escape the solver found in a function: a pointer to a frame
// object reaching a place that outlives it.
type Finding struct {
	Code diag.Code // E3001 to E3006, E3009
	Pos  token.Pos // where the pointer goes: the store, return, call or send
	Func eir.FuncID
	Obj  eir.ObjID // the frame object
	Sink eir.SinkKind
	// Through the call at Site of Func, to Callee and its summary entry
	// Entry (stage 5 turns these into messages); Callee is eir.NoFunc for
	// the function's own code.
	Site   eir.SiteID
	Callee eir.FuncID
	Entry  int
}

// Result is the outcome of solving one function.
type Result struct {
	Summary  *summary.Summary
	Findings []Finding
}
