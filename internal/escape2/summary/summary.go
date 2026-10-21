// Package summary is the format of what a function does with the pointers
// its caller passes (plan docs/plans/2026-10-20-escape-redesign.md, section
// (a), "Summary of f", and invariant I4). The solver writes summaries, the
// scheduler keeps the final ones in the program arena, callers apply them,
// the reference solver and the D44 cache check them.
//
// Every set is an antichain under domain.Covers, sorted by key.
//
// Deferred dispatches and obligations (plan section (c)) are not in this
// format: they belong to stage 3, which waits for the owner's decision on a
// rule for dynamic calls.
//
// This file is the interface other packages build against: change it only by
// agreement (internal/escape2 package comment).
package summary

import "github.com/matjam/plutonium/internal/escape2/domain"

// Caps on one input's keys (plan section (a), "Caps and widening"). Past a
// cap the input's keys are replaced by their domain.Paths.Cut with GE set,
// which covers them: monotone and sound (invariant I8).
const (
	MaxEscPerInput   = 64
	MaxStorePerInput = 128
)

// Summary is what a function does with the pointers its caller passes.
type Summary struct {
	// Inputs is the number of inputs keys may name (receiver first, then
	// parameters, then a literal's closure record).
	Inputs int

	// Esc are the pointers the function keeps where they outlive the call:
	// heap, package memory, channels, tasks, code outside Plutonium.
	Esc []domain.Key
	// Store are the stores into caller memory: the function stores what
	// Src leads to into the memory Dst leads to. Pairs with
	// Src.Input() == Dst.Input() are included (plan invariant I4(b)).
	Store []StorePair
	// HeapStore are the places the function fills with heap pointers.
	HeapStore []domain.Key
	// Res says what each result may hold, by result index.
	Res []Result

	// Region facts of section 11.5 (closure regions the caller provides).
	Region Region

	// Reasons explain Esc and Store entries, for messages (stage 5): the
	// call site and callee through which the key escapes, in the same
	// order as Esc then Store. Empty until stage 5 fills them.
	Reasons []Reason
}

// StorePair is one store edge of a summary.
type StorePair struct {
	Src, Dst domain.Key
}

// Result is what one result may hold.
type Result struct {
	// From are the input-relative pointers the result holds, each with the
	// field path of a struct result that holds it (domain.Root for the
	// result as a whole).
	From []ResultKey
	// Heap and CountedHeap: the result may hold heap pointers, and counted
	// ones (sections 16.5 to 16.7), by field path.
	Heap        []domain.PathID
	CountedHeap []domain.PathID
}

// ResultKey is a key a result holds, at a field path of the result.
type ResultKey struct {
	Key   domain.Key
	Field domain.PathID
	// FieldGE: the key is held at Field or at any field path under it.
	FieldGE bool
}

// Region is what a function does with closure regions (section 11.5).
type Region struct {
	// Builds: the function builds a closure in a region its caller
	// provides.
	Builds bool
	// OuterResults are the results that may hold a closure built in that
	// region.
	OuterResults []int
}

// Reason is a compact explanation of a summary entry: no closures over the
// analyzer, no strings (plan section (e), program arena).
type Reason struct {
	Site   uint32 // the call site in the function, or ^0 for its own code
	Callee uint32 // the eir.FuncID of the callee, or ^0
	Entry  uint32 // the index of the entry in the callee's summary
	// Pos is the position of what made the entry: the call, or for the
	// function's own code the store or sink (a token.Pos; 0 when unknown).
	Pos uint32
	// Sink is the eir.SinkKind of an entry the function's own code made by
	// a sink (package memory, send, task, external); 0 for a store.
	Sink uint8
}

// Leq and Widen are in ops.go, which the solver's builder owns.
