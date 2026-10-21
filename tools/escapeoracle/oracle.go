package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// A Kind classifies a disagreement between the calls the generated code
// makes and the calls the escape analysis applied.
type Kind string

const (
	// Missing: the generated code calls a source function at a position
	// where the analysis applied nothing for it. A likely hole.
	Missing Kind = "missing"
	// Filtered: the analysis considered the function at that call and left
	// it out by a filter (a class C narrowing of the plan); a hole if the
	// filter is wrong.
	Filtered Kind = "filtered"
	// Elsewhere: the analysis applied the function on the same line of the
	// same file, at another column. Usually a position convention, recorded
	// so that it can be checked.
	Elsewhere Kind = "elsewhere"
	// Runtime: an implicit call into a runtime function that the analysis
	// does not model, and that the contract table does not list.
	Runtime Kind = "runtime"
	// ElsewhereInFunc: the analysis applied the function at another place
	// of the same function's source. Drops are applied once, at the end of
	// the variable's scope, where the generated code calls them at every
	// exit; recorded so that it can be checked.
	ElsewhereInFunc Kind = "elsewhere-in-function"
	// BoxRelease: the Drop of a counted or owned box's payload, reached
	// through the release of the box. The payload is heap memory, so its
	// Drop receives a heap pointer and no view of a frame.
	BoxRelease Kind = "box-release"
	// ForeignBody: the body of another range loop, which the lowerer lists
	// as a target because its signature matches, reached through an
	// iterator it made (a ring's All): that iterator calls the body of its
	// own loop only.
	ForeignBody Kind = "foreign-body"
	// BlankItab: a method of a type converted to an interface only in a
	// declaration of the blank identifier (var _ I = (*T)(nil)), whose value
	// is discarded. The lowerer emits the itab; no interface ever holds it.
	BlankItab Kind = "blank-itab"
	// Unrecorded: an indirect call lowering recorded no targets for.
	Unrecorded Kind = "unrecorded"
	// NotWalked: a source function lowering emitted whose body the analysis
	// never walked.
	NotWalked Kind = "not-walked"
)

// A Finding is one disagreement.
type Finding struct {
	Kind    Kind
	Pos     token.Position
	Caller  string // the lowered function making the call
	Callee  string // the source function called
	How     string // static, interface I.M, func value of type S, drop adapter
	Why     string // for Filtered: the filter's reason
	Program string
}

func (f Finding) String() string {
	s := fmt.Sprintf("%s: %s: %s calls %s (%s)", f.Kind, f.Pos, f.Caller, f.Callee, f.How)
	if f.Why != "" {
		s += ": " + f.Why
	}
	return s
}

// Report is the outcome of the oracle on one program.
type Report struct {
	Program  string
	Calls    int // calls with a source position compared
	Targets  int // (call, source target) pairs compared
	Findings []Finding
	// Notes: errors of the checks and lowering, for the record.
	Notes []string
}

// compare runs the oracle on the inputs of one program.
func compare(name string, in *build.OracleInputs) *Report {
	r := &Report{Program: name}
	if in.EscapeErr != nil {
		r.Notes = append(r.Notes, "escape: "+firstLine(in.EscapeErr.Error()))
	}
	if in.LowerErr != nil {
		r.Notes = append(r.Notes, "lower: "+firstLine(in.LowerErr.Error()))
	}
	cf, log := in.Facts, in.Log
	if cf == nil {
		r.Notes = append(r.Notes, "lowering produced nothing")
		return r
	}
	// The analysis's records by file and line, for Elsewhere.
	byLine := map[lineKey][]escape.Callee{}
	for pos, cs := range log.Applied {
		p := cf.Position(pos)
		byLine[lineKey{p.Filename, p.Line}] = append(byLine[lineKey{p.Filename, p.Line}], cs...)
	}
	// The analysis's records in position order, for ElsewhereInFunc.
	var order []token.Pos
	for pos := range log.Applied {
		order = append(order, pos)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	inRange := func(from, to token.Pos) []escape.Callee {
		var out []escape.Callee
		for i := sort.Search(len(order), func(i int) bool { return order[i] >= from }); i < len(order) && order[i] <= to; i++ {
			out = append(out, log.Applied[order[i]]...)
		}
		return out
	}
	blank := blankConversions(in)
	seen := map[string]bool{}
	add := func(f Finding) {
		f.Program = name
		k := f.String()
		if !seen[k] {
			seen[k] = true
			r.Findings = append(r.Findings, f)
		}
	}
	for _, f := range cf.Funcs() {
		o := cf.Origin(f.Sym)
		notWalked := false
		switch {
		case o.Lit != nil:
			notWalked = !log.Walked[o.Lit.Pos()]
		case o.Range != nil:
			notWalked = !log.Walked[o.Range.Pos()]
		case o.Decl != nil && o.Decl.Body != nil:
			notWalked = !log.Walked[o.Decl.Pos()]
		}
		if notWalked {
			add(Finding{Kind: NotWalked, Pos: cf.Position(f.Pos), Caller: f.Name, Callee: f.Name, How: "body"})
		}
		for _, c := range cf.Calls(f) {
			if !c.Pos.IsValid() || c.Value.Inl != 0 {
				continue
			}
			r.Calls++
			how := howOf(c)
			if c.Kind == lower.CallUnknown {
				add(Finding{Kind: Unrecorded, Pos: cf.Position(c.Pos), Caller: f.Name, How: how})
				continue
			}
			applied := log.Applied[c.Pos]
			if hasUnknown(applied) {
				// The analysis assumed the worst of this call.
				continue
			}
			for _, t := range c.Targets {
				if to := cf.Origin(t); to.Obj != nil && matches(applied, to) {
					// The analysis applied the function the code calls,
					// by a summary of its own (a body-less hook, a
					// generated method): not what it is lowered to.
					r.Targets++
					continue
				}
				for _, s := range cf.SourceTargets(t) {
					r.Targets++
					so := cf.Origin(s)
					if matches(applied, so) {
						continue
					}
					if contract(s, c, so) != "" {
						continue
					}
					fd := Finding{Pos: cf.Position(c.Pos), Caller: f.Name, Callee: s.Name, How: how}
					if t != s {
						fd.How += " through " + t.Name
					}
					switch {
					case skippedFor(log.Skipped[c.Pos], so) != "":
						fd.Kind, fd.Why = Filtered, skippedFor(log.Skipped[c.Pos], so)
					case !so.Source(), so.Obj != nil && so.Obj.Pkg() != nil && so.Obj.Pkg().Path() == "pluto/rt":
						// The analysis applies every call written in the
						// source, so a runtime function it did not apply
						// is one the compiler calls.
						fd.Kind = Runtime
					case boxRelease(t):
						fd.Kind = BoxRelease
					case c.Kind == lower.CallIface && so.Obj != nil && blankOnly(blank, so.Obj):
						fd.Kind = BlankItab
					case so.Range != nil && t != s && so.Range.Pos() != c.Pos:
						// A loop body reached, by its signature, through
						// a function the lowerer made: the iterator calls
						// only the body of its own loop.
						fd.Kind = ForeignBody
					case matches(byLine[lineKey{fd.Pos.Filename, fd.Pos.Line}], so):
						fd.Kind = Elsewhere
					case f.Pos.IsValid() && f.End.IsValid() && matches(inRange(f.Pos, f.End), so):
						fd.Kind = ElsewhereInFunc
					default:
						fd.Kind = Missing
					}
					add(fd)
				}
			}
		}
	}
	sort.Slice(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.String() < b.String()
	})
	return r
}

type lineKey struct {
	file string
	line int
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func hasUnknown(cs []escape.Callee) bool {
	for _, c := range cs {
		if c.Unknown {
			return true
		}
	}
	return false
}

// matches reports whether the analysis applied the source function o.
func matches(cs []escape.Callee, o lower.Origin) bool {
	for _, c := range cs {
		switch {
		case o.Obj != nil && c.Obj == o.Obj,
			o.Lit != nil && c.Lit == o.Lit,
			o.Range != nil && c.Range == o.Range:
			return true
		}
	}
	return false
}

func skippedFor(ss []escape.Skip, o lower.Origin) string {
	for _, s := range ss {
		if matches([]escape.Callee{s.Callee}, o) {
			return s.Why
		}
	}
	return ""
}

func howOf(c lower.Call) string {
	s := ""
	switch c.Kind {
	case lower.CallStatic:
		s = "static"
	case lower.CallIface:
		s = fmt.Sprintf("interface %s.%s", c.Iface, lower.MethodName(c.Iface, c.Method))
	case lower.CallFuncVal:
		s = fmt.Sprintf("func value %s", c.FSig)
	case lower.CallDropAdapt:
		s = fmt.Sprintf("drop adapter of %s", c.Iface)
	default:
		s = "indirect, unrecorded"
	}
	if c.Implicit {
		s += ", implicit"
	}
	return s
}

// contract returns why the analysis need not apply s at the call c, if s is
// an entry of the reviewed contract table, or "".
func contract(s *ssa.Sym, c lower.Call, o lower.Origin) string {
	if why, ok := contractTable[s.Name]; ok {
		return why
	}
	return ""
}

// boxRelease reports whether t is the release or drop of a counted or owned
// box, which drops the box's payload (lower: box.go, drop.go, owniface.go).
func boxRelease(t *ssa.Sym) bool {
	n := t.Name
	if strings.HasSuffix(n, "·release") {
		return true
	}
	for _, p := range []string{"shared[", "synced[", "owned[", "weak["} {
		if strings.HasPrefix(n, p) && strings.Contains(n, "·drop") {
			return true
		}
	}
	return false
}

// blankConversions returns the types converted to an interface by the
// package-level declarations of the blank identifier.
func blankConversions(in *build.OracleInputs) []types2.Type {
	var out []types2.Type
	for _, p := range in.Prog.Packages {
		for _, init := range p.Info.InitOrder {
			for _, v := range init.Lhs {
				if v.Name() != "_" || !types2.IsInterface(v.Type()) {
					continue
				}
				if tv, ok := p.Info.Types[init.Rhs]; ok && tv.Type != nil && !types2.IsInterface(tv.Type) {
					out = append(out, tv.Type)
				}
			}
		}
	}
	return out
}

// blankOnly reports whether method m belongs to one of the blank-converted
// types (by its receiver's type, or the pointer to it).
func blankOnly(blank []types2.Type, m *types2.Func) bool {
	sig, _ := m.Type().(*types2.Signature)
	if sig == nil || sig.Recv() == nil {
		return false
	}
	r := sig.Recv().Type()
	for _, t := range blank {
		if types2.Identical(t, r) {
			return true
		}
		if p, ok := t.(*types2.Pointer); ok && types2.Identical(p.Elem(), r) {
			return true
		}
	}
	return false
}
