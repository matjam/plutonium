// The lowering oracle runs, in the normal suite, over a sample of the
// corpus (oracleSample); over the whole corpus with
//
//	PLUTO_ESCAPE_ORACLE=1 go test ./internal/escape2/eir -run TestOracle
//
// (PLUTO_ESCAPE_ORACLE_ONLY=group/ for part of the corpus,
// PLUTO_ESCAPE_ORACLE_DIR=selfhost/demo/lowerdemo PLUTO_ESCAPE_ORACLE_NOESCAPE=1
// for a translated compiler, PLUTO_ESCAPE_ORACLE_OUT=file for every
// difference), which needs a bigjob slot.

package eir_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/escape2/contract"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// The lowering oracle (plan docs/plans/2026-10-20-escape-redesign.md,
// section 3.1): the calls internal/lower emits, before any optimisation,
// against the calls of EIR, function by function. A call is named by what
// it reaches: a source function (a declaration by origin, a literal, a
// range body), or, for a dynamic call, its kind and method name. A call of
// a function the lowerer synthesizes (drop glue, an adapter, a wrapper) is
// named by the calls that function makes, through any number of
// synthesized functions, on both sides. Every call the generated code makes
// must be in EIR (a missing call is a hole in the analysis), and every call
// EIR makes must be in the generated code of some instance of the function
// (an invented call is a false rejection waiting to happen).
//
// What neither side names, by the contract table (internal/escape2/
// contract): the runtime calls the lowerer makes for constructs (Implicit
// calls), which EIR states with its own instructions, and the inline
// expansions, which the generated code does not call. Releases of counted
// references (the ·release glue of shared, synced and weak boxes and
// channels, and the counted-error drop through an itab) are not EIR calls
// either: what they pass is the counted object, heap memory, so nothing
// they call can keep frame memory.

// oracleKey names what a call reaches.
type oracleKey struct {
	obj  *types2.Func
	lit  *ast.FuncLit
	rng  *ast.RangeStmt
	what string // "iface:M", "func", "dropiface", "tp:M", "tpdrop", "unknown"
}

func (k oracleKey) String() string {
	switch {
	case k.obj != nil:
		return types2.ObjectString(k.obj, nil)
	case k.lit != nil:
		return "func literal"
	case k.rng != nil:
		return "range body"
	}
	return k.what
}

// methodName returns the name of the method a key names, or "".
func (k oracleKey) methodName() string {
	if k.obj != nil && k.obj.Type().(*types2.Signature).Recv() != nil {
		return k.obj.Name()
	}
	for _, p := range []string{"iface:", "tp:"} {
		if strings.HasPrefix(k.what, p) {
			return k.what[len(p):]
		}
	}
	return ""
}

// oracleDiff is the difference the oracle finds in one program.
type oracleDiff struct {
	missing  []string // calls of the generated code that EIR does not have
	invented []string // calls of EIR that no generated code makes
	noEIR    []string // lowered source functions EIR has no function for
	generic  []string // calls of generic bodies that no instance makes
	shapes   []string // arguments lowering passes as a frame address that EIR passes as a loaded value
}

// isGeneric reports whether f is, or is nested in, a generic function or a
// method of a generic type.
func (o *oracle) isGeneric(f *eir.Func) bool {
	if f.Key.Lit != nil || f.Key.Range != nil {
		for _, g := range o.p.Funcs {
			if g.Key.Obj != nil && g.Pos <= f.Pos && f.Pos < g.End {
				return o.isGeneric(g)
			}
		}
		return false
	}
	sig := f.Sig
	return sig != nil && (sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0)
}

// runOracle lowers prog and builds its EIR with the same move facts, and
// compares their calls.
func runOracle(t *testing.T, cp corpusProgram, prog *load.Program) oracleDiff {
	t.Helper()
	var calls *escape.Calls
	if os.Getenv("PLUTO_ESCAPE_ORACLE_NOESCAPE") == "" {
		// The move facts lowering gets in a build: with what the escape
		// analysis found about calls. Without it (for programs the
		// current analysis is too large for), both sides get the move
		// checker's facts alone, which is all the comparison needs.
		calls, _ = escape.Analyze(prog)
	}
	mv, _ := moves.AnalyzeWith(prog, calls)
	cf, err := lower.LowerFacts(&lower.Config{
		Freestanding: cp.freestanding || cp.kernel,
		Kernel:       cp.kernel,
		Sizes:        types2.SizesFor("gc", "amd64"),
		GenCheck:     !cp.freestanding && !cp.kernel,
		Moves:        mv,
	}, prog)
	if cf == nil || err != nil {
		// A program the lowerer rejects (a diagnostics program) stops
		// being lowered where the error is: there is nothing to compare.
		t.Skipf("does not lower: %v", err)
	}
	p, err := eir.BuildWith(prog, eir.Options{Moves: mv})
	if err != nil {
		t.Fatalf("eir: %v", err)
	}
	if fn := os.Getenv("PLUTO_ESCAPE_ORACLE_FUNC"); fn != "" {
		// Debugging: the calls of the lowered functions of that name, and
		// their EIR.
		for _, f := range cf.Funcs() {
			if f.Name != fn {
				continue
			}
			for _, c := range cf.Calls(f) {
				name := ""
				if c.Static != nil {
					name = c.Static.Name
				}
				t.Logf("ssa %s: kind=%d implicit=%v static=%s iface=%v fsig=%v targets=%v at %v", f.Name, c.Kind, c.Implicit, name, c.Iface, c.FSig, c.Targets, prog.Fset.Position(c.Pos))
				if c.Static != nil {
					t.Logf("    origin %+v", cf.Origin(c.Static))
				}
			}
		}
		for _, f := range p.Funcs {
			if strings.HasSuffix(f.Name, fn) {
				var b strings.Builder
				p.Dump(&b, prog.Fset, f)
				t.Log(b.String())
			}
		}
	}
	o := &oracle{cf: cf, p: p, prog: prog, ssaExp: map[*ssa.Sym][]oracleKey{}, eirExp: map[eir.FuncID][]oracleKey{}, bodyless: map[*types2.Func]bool{}}
	for _, pkg := range prog.Packages {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body == nil {
					if fn, ok := pkg.Info.Defs[fd.Name].(*types2.Func); ok {
						o.bodyless[fn] = true
					}
				}
			}
		}
	}
	return o.compare()
}

type oracle struct {
	cf     *lower.CallFacts
	p      *eir.Program
	prog   *load.Program
	byName map[string]*ssa.Func
	ssaExp map[*ssa.Sym][]oracleKey
	eirExp map[eir.FuncID][]oracleKey
	// bodyless are the functions declared without a body: the lowerer
	// gives some of them one (the scheduler hooks), whose calls stand for
	// a call of them, as for synthesized functions.
	bodyless map[*types2.Func]bool
	declared map[*types2.Func]bool   // the functions declared in source
	cnames   map[*types2.Func]string // the C names of //pu:extern functions
}

// externKey names a //pu:extern function by its C name: two declarations
// of one C function are one function.
func (o *oracle) externKey(fn *types2.Func) (oracleKey, bool) {
	if o.cnames == nil {
		o.cnames = map[*types2.Func]string{}
		for _, pkg := range o.prog.Packages {
			for _, file := range pkg.Files {
				for _, decl := range file.Decls {
					fd, ok := decl.(*ast.FuncDecl)
					if !ok || fd.Doc == nil {
						continue
					}
					for _, c := range fd.Doc.List {
						if f := strings.Fields(c.Text); len(f) == 2 && f[0] == "//pu:extern" {
							if fn, ok := pkg.Info.Defs[fd.Name].(*types2.Func); ok {
								o.cnames[fn] = f[1]
							}
						}
					}
				}
			}
		}
	}
	if name, ok := o.cnames[fn.Origin()]; ok {
		return oracleKey{what: "extern " + name}, true
	}
	return oracleKey{}, false
}

// isRuntime reports whether a symbol is a runtime function of the contract
// table.
func isRuntime(name string) bool { return contract.IsRuntime(name) }

// isRelease reports whether a symbol is release glue of a counted
// reference.
func isRelease(name string) bool {
	return strings.HasSuffix(name, "·release") || strings.HasSuffix(name, "·rdrop")
}

// cnamed reports whether fn is declared //pu:extern.
func (o *oracle) cnamed(fn *types2.Func) bool {
	_, ok := o.externKey(fn)
	return ok
}

// generated reports whether fn is a method the type checker declares and
// the lowerer generates (an enum's String, an errors.New site's Error).
func (o *oracle) generated(fn *types2.Func) bool {
	if o.declared == nil {
		o.declared = map[*types2.Func]bool{}
		for _, pkg := range o.prog.Packages {
			for _, file := range pkg.Files {
				for _, decl := range file.Decls {
					if fd, ok := decl.(*ast.FuncDecl); ok {
						if fn, ok := pkg.Info.Defs[fd.Name].(*types2.Func); ok {
							o.declared[fn] = true
						}
					}
				}
			}
		}
	}
	return !o.declared[fn.Origin()]
}

// isLateHook reports whether fn is rt.Sleep or rt.mainWait, whose body the
// lowerer chooses once the program is lowered; it names them as they are.
func isLateHook(fn *types2.Func) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == "pluto/rt" && (fn.Name() == "Sleep" || fn.Name() == "mainWait")
}

// lateHook names the functions a late hook chooses between by the hook
// (rt.sleepDirect and rt.sleepParked are rt.Sleep): which one the lowerer
// calls depends on the whole program, and EIR calls the hook.
func (o *oracle) lateHook(k oracleKey) oracleKey {
	if k.obj == nil || k.obj.Pkg() == nil || k.obj.Pkg().Path() != "pluto/rt" {
		return k
	}
	for hook, names := range map[string][2]string{"Sleep": {"sleepDirect", "sleepParked"}, "mainWait": {"mainWaitDirect", "mainWaitParked"}} {
		if k.obj.Name() == names[0] || k.obj.Name() == names[1] {
			if fn, ok := k.obj.Pkg().Scope().Lookup(hook).(*types2.Func); ok {
				return oracleKey{obj: fn}
			}
		}
	}
	return k
}

// isRegionDrop reports whether a lowered call through a func value is the
// drop of a closure region (section 11.5), which releases the counted
// references the region holds.
func isRegionDrop(c lower.Call) bool {
	if c.FSig == nil || c.FSig.Params().Len() != 1 || c.FSig.Results().Len() != 0 || c.FSig.Params().At(0).Name() != "region" {
		return false
	}
	for _, t := range c.Targets {
		if !isRelease(t.Name) {
			return false
		}
	}
	return true
}

// owner returns the EIR function whose calls a lowered function's are: its
// own source function, or for a task thunk or a deferred-call runner the
// function of the statement; ok is false for a function synthesized for
// the program (drop glue, adapters, wrappers, the entry).
func (o *oracle) owner(f *ssa.Func) (eir.FuncID, bool) {
	origin := o.cf.Origin(f.Sym)
	if eir.IsFmtWalk(origin.Obj) {
		// Its EIR body is its contract (fmtwalk.go), not its source.
		return eir.NoFunc, false
	}
	switch {
	case origin.Lit != nil:
		return o.p.Lookup(eir.FuncKey{Lit: origin.Lit}), true
	case origin.Range != nil:
		return o.p.Lookup(eir.FuncKey{Range: origin.Range}), true
	case origin.Obj != nil:
		return o.p.Lookup(eir.FuncKey{Obj: origin.Obj}), true
	case origin.Decl != nil:
		for _, pkg := range o.prog.Packages {
			if fn, ok := pkg.Info.Defs[origin.Decl.Name].(*types2.Func); ok {
				return o.p.Lookup(eir.FuncKey{Obj: fn}), true
			}
		}
		return eir.NoFunc, true
	}
	name := f.Name
	for _, pkg := range o.prog.Packages {
		if name == pkg.Path+".init·pkg" {
			return o.p.Lookup(eir.FuncKey{Init: pkg.Types}), true
		}
	}
	if i := strings.LastIndex(name, "·defers"); i > 0 && i+len("·defers") == len(name) {
		if parent := o.byName[name[:i]]; parent != nil {
			return o.owner(parent)
		}
	}
	if i := strings.LastIndex(name, ".task"); i > 0 {
		if parent := o.byName[name[:i]]; parent != nil {
			return o.owner(parent)
		}
	}
	return eir.NoFunc, false
}

// sourceKey names a lowered function that is source code, or reports false.
func (o *oracle) sourceKey(sym *ssa.Sym) (oracleKey, bool) {
	origin := o.cf.Origin(sym)
	switch {
	case origin.Lit != nil:
		return oracleKey{lit: origin.Lit}, true
	case origin.Range != nil:
		return oracleKey{rng: origin.Range}, true
	case origin.Obj != nil:
		if k, ok := o.externKey(origin.Obj); ok {
			return k, true
		}
		return oracleKey{obj: origin.Obj}, true
	case origin.Decl != nil:
		for _, pkg := range o.prog.Packages {
			if fn, ok := pkg.Info.Defs[origin.Decl.Name].(*types2.Func); ok {
				return oracleKey{obj: fn}, true
			}
		}
	}
	return oracleKey{}, false
}

// ssaKeys returns what a lowered call reaches.
func (o *oracle) ssaKeys(c lower.Call, active map[*ssa.Sym]bool) []oracleKey {
	if c.Implicit {
		return nil
	}
	switch c.Kind {
	case lower.CallStatic:
		keys := o.symKeys(c.Static, active)
		if c.Value != nil && c.Value.Op == ssa.OpCallInd {
			// A call through a func value whose function the lowerer
			// knows (it stored the address just before): EIR calls
			// through the value.
			// it calls statically or through the value (func?).
			keys = append(keys, oracleKey{what: "func?"})
		}
		return keys
	case lower.CallIface:
		return []oracleKey{{what: "iface:" + lower.MethodName(c.Iface, c.Method)}}
	case lower.CallFuncVal:
		if isRegionDrop(c) {
			return nil
		}
		// A deferred call of an interface method is lowered as a call of
		// the method value, whose function calls the method through the
		// interface: EIR makes the interface call.
		if len(c.Targets) > 0 {
			var keys []oracleKey
			for _, t := range c.Targets {
				if !strings.HasSuffix(t.Name, "-fm") {
					keys = nil
					break
				}
				keys = append(keys, o.symKeys(t, active)...)
			}
			if keys = dedup(keys); len(keys) == 1 && strings.HasPrefix(keys[0].what, "iface:") {
				// EIR makes either call: through the func value, or
				// (for a deferred call) through the interface.
				return []oracleKey{{what: "func|" + keys[0].what}}
			}
		}
		return []oracleKey{{what: "func"}}
	case lower.CallDropAdapt:
		if c.Iface != nil && types2.IsCountedError(c.Iface) && !types2.IsOwner(c.Iface) {
			return nil // the release of a counted error
		}
		return []oracleKey{{what: "dropiface"}}
	}
	return []oracleKey{{what: "unknown"}}
}

// symKeys returns what a static call of sym reaches: sym if it is source,
// otherwise what the synthesized function's own calls reach.
func (o *oracle) symKeys(sym *ssa.Sym, active map[*ssa.Sym]bool) []oracleKey {
	if isRuntime(sym.Name) || isRelease(sym.Name) {
		return nil
	}
	if k, ok := o.sourceKey(sym); ok {
		return []oracleKey{o.lateHook(k)}
	}
	if !strings.Contains(sym.Name, ".") {
		// A C name: the //pu:extern function, whatever the lowerer made
		// for its C calling convention.
		return []oracleKey{{what: "extern " + sym.Name}}
	}
	f := o.cf.Func(sym)
	if f == nil {
		// Not lowered as a function of its own: a late hook whose body is
		// chosen at the end (rt.Sleep), a generated helper.
		if fn := o.funcByName(sym.Name); fn != nil {
			return []oracleKey{o.lateHook(oracleKey{obj: fn})}
		}
		switch sym.Name {
		case "pluto.noRegionDrop":
			return nil // the drop of a region that holds nothing to release
		case "pluto.dropGlobals":
			return []oracleKey{{what: "dropGlobals"}}
		}
		return []oracleKey{{what: "extern " + sym.Name}}
	}
	if keys, ok := o.ssaExp[sym]; ok {
		return keys
	}
	if active[sym] {
		return nil
	}
	active[sym] = true
	var out []oracleKey
	for _, c := range o.cf.Calls(f) {
		out = append(out, o.ssaKeys(c, active)...)
	}
	delete(active, sym)
	out = dedup(out)
	o.ssaExp[sym] = out
	return out
}

// eirKeys returns what an EIR call reaches.
func (o *oracle) eirKeys(c *eir.Call, active map[eir.FuncID]bool) []oracleKey {
	switch c.Kind {
	case eir.CallStatic:
		return o.funcKeys(c.Callee, active)
	case eir.CallIface:
		if c.Method == nil {
			return []oracleKey{{what: "dropiface"}}
		}
		return []oracleKey{{what: "iface:" + c.Method.Name()}}
	case eir.CallTypeParam:
		if c.Method == nil {
			return []oracleKey{{what: "tpdrop"}}
		}
		return []oracleKey{{what: "tp:" + c.Method.Name()}}
	case eir.CallFunc:
		return []oracleKey{{what: "func"}}
	}
	if contract.IsTrusted(c.Contract) {
		// A function the lowerer calls whose effects are the table's.
		if fn := o.funcByName(c.Contract); fn != nil {
			return []oracleKey{{obj: fn}}
		}
	}
	return nil // the contract table's inline expansions
}

// funcByName returns the package-level function of a qualified name.
func (o *oracle) funcByName(name string) *types2.Func {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		// A C name: the function declared //pu:extern name.
		for _, pkg := range o.prog.Packages {
			for _, file := range pkg.Files {
				for _, decl := range file.Decls {
					fd, ok := decl.(*ast.FuncDecl)
					if !ok || fd.Doc == nil {
						continue
					}
					for _, c := range fd.Doc.List {
						if f := strings.Fields(c.Text); len(f) == 2 && f[0] == "//pu:extern" && f[1] == name {
							fn, _ := pkg.Info.Defs[fd.Name].(*types2.Func)
							return fn
						}
					}
				}
			}
		}
		return nil
	}
	for _, p := range o.prog.Packages {
		if p.Path == name[:i] {
			fn, _ := p.Types.Scope().Lookup(name[i+1:]).(*types2.Func)
			return fn
		}
		// A method: "path.Type.Method".
		if j := strings.LastIndex(name[:i], "."); j > 0 && p.Path == name[:j] {
			if tn, ok := p.Types.Scope().Lookup(name[j+1 : i]).(*types2.TypeName); ok {
				obj, _, _ := types2.LookupFieldOrMethod(types2.NewPointer(tn.Type()), true, p.Types, name[i+1:])
				fn, _ := obj.(*types2.Func)
				return fn
			}
		}
	}
	return nil
}

// funcKeys returns what a static call of EIR function id reaches.
func (o *oracle) funcKeys(id eir.FuncID, active map[eir.FuncID]bool) []oracleKey {
	if id == eir.NoFunc {
		return []oracleKey{{what: "unknown"}}
	}
	f := o.p.Funcs[id]
	switch k := f.Key; {
	case k.Obj != nil && o.cnamed(k.Obj):
		key, _ := o.externKey(k.Obj)
		return []oracleKey{key}
	case k.Obj != nil && o.generated(k.Obj):
		return nil // a method the lowerer generates, which calls nothing
	case k.Obj != nil && (!o.bodyless[k.Obj] || len(f.Blocks) == 0 || f.External || isLateHook(k.Obj)):
		if isRuntime(k.Obj.Pkg().Path() + "." + k.Obj.Name()) {
			return nil
		}
		return []oracleKey{{obj: k.Obj}}
	case k.Lit != nil:
		return []oracleKey{{lit: k.Lit}}
	case k.Range != nil:
		return []oracleKey{{rng: k.Range}}
	}
	if s := f.Key.Synth; s != nil && s.Kind == eir.SynthDropGlue {
		if _, ok := types2.Unalias(s.Recv).(*types2.TypeParam); ok {
			return []oracleKey{{what: "tpdrop"}}
		}
	}
	if keys, ok := o.eirExp[id]; ok {
		return keys
	}
	if active[id] {
		return nil
	}
	active[id] = true
	var out []oracleKey
	for _, b := range f.Blocks {
		for i := range b.Instrs {
			if in := &b.Instrs[i]; in.Op == eir.OpCall {
				out = append(out, o.eirKeys(in.Call, active)...)
			}
		}
	}
	delete(active, id)
	out = dedup(out)
	o.eirExp[id] = out
	return out
}

func dedup(keys []oracleKey) []oracleKey {
	seen := map[oracleKey]bool{}
	var out []oracleKey
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// covered reports whether the EIR keys have one that stands for the
// lowered call k: the same key, or a type-parameter method call of its
// name.
func covered(k oracleKey, eirSet map[oracleKey]token.Pos) bool {
	if _, ok := eirSet[k]; ok {
		return true
	}
	if k.what == "func?" {
		return true // EIR may call the function statically
	}
	if alt, ok := strings.CutPrefix(k.what, "func|"); ok {
		_, f := eirSet[oracleKey{what: "func"}]
		_, i := eirSet[oracleKey{what: alt}]
		return f || i
	}
	m := k.methodName()
	if _, ok := eirSet[oracleKey{what: "tp:" + m}]; ok && m != "" {
		return true
	}
	if _, ok := eirSet[oracleKey{what: "tpdrop"}]; ok && (m == "Drop" || k.what == "dropiface") {
		// The drop glue of a type parameter: each instance's Drop methods.
		return true
	}
	if k.what == "dropGlobals" {
		// pluto.dropGlobals, which the lowerer builds once every package
		// is lowered and the oracle does not see: EIR's entry drops the
		// package variables itself.
		return true
	}
	return false
}

// where formats a position as file:line, the file's base name.
func (o *oracle) where(pos token.Pos) string {
	if !pos.IsValid() {
		return "-"
	}
	p := o.prog.Fset.Position(pos)
	return fmt.Sprintf("%s:%d", filepath.Base(p.Filename), p.Line)
}

// made reports whether some lowered call stands for the EIR key k.
func made(k oracleKey, ssaSet map[oracleKey]token.Pos, methods map[string]bool) bool {
	if _, ok := ssaSet[k]; ok || k.what == "tpdrop" {
		return true
	}
	if _, ok := ssaSet[oracleKey{what: "func?"}]; ok && k.what == "func" {
		return true
	}
	for s := range ssaSet {
		if alt, ok := strings.CutPrefix(s.what, "func|"); ok && (k.what == "func" || k.what == alt) {
			return true
		}
	}
	if _, ok := ssaSet[oracleKey{what: "dropGlobals"}]; ok && k.methodName() == "Drop" {
		return true // the package variables' drops, in pluto.dropGlobals
	}
	if strings.HasPrefix(k.what, "tp:") {
		return methods[k.what[3:]]
	}
	return false
}

func (o *oracle) compare() oracleDiff {
	o.byName = map[string]*ssa.Func{}
	for _, f := range o.cf.Funcs() {
		o.byName[f.Name] = f
	}
	ssaSets := map[eir.FuncID]map[oracleKey]token.Pos{}
	var d oracleDiff
	for _, f := range o.cf.Funcs() {
		id, ok := o.owner(f)
		if !ok {
			continue
		}
		if id == eir.NoFunc {
			d.noEIR = append(d.noEIR, f.Name)
			continue
		}
		set := ssaSets[id]
		if set == nil {
			set = map[oracleKey]token.Pos{}
			ssaSets[id] = set
		}
		for _, c := range o.cf.Calls(f) {
			for _, k := range o.ssaKeys(c, map[*ssa.Sym]bool{}) {
				if _, ok := set[k]; !ok {
					set[k] = c.Pos
				}
			}
		}
	}
	for id, ssaSet := range ssaSets {
		f := o.p.Funcs[id]
		// A function's call of itself is left out on both sides: the
		// lowerer wraps some functions (an allocator registration, a
		// static write check) in a function of the same origin that calls
		// the implementation.
		self := oracleKey{obj: f.Key.Obj, lit: f.Key.Lit, rng: f.Key.Range}
		delete(ssaSet, self)
		eirSet := map[oracleKey]token.Pos{}
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				if in := &b.Instrs[i]; in.Op == eir.OpCall {
					for _, k := range o.eirKeys(in.Call, map[eir.FuncID]bool{}) {
						if _, ok := eirSet[k]; !ok {
							eirSet[k] = in.Pos
						}
					}
				}
			}
		}
		delete(eirSet, self)
		methods := map[string]bool{}
		for k := range ssaSet {
			if m := k.methodName(); m != "" {
				methods[m] = true
			}
		}
		for k, pos := range ssaSet {
			if !covered(k, eirSet) {
				d.missing = append(d.missing, f.Name+": "+k.String()+" @ "+o.where(pos))
			}
		}
		generic := o.isGeneric(f)
		for k, pos := range eirSet {
			if !made(k, ssaSet, methods) {
				line := f.Name + ": " + k.String() + " @ " + o.where(pos)
				if generic {
					// A call of the generic body that no instance of this
					// program makes: a type switch or a type-dependent
					// drop folds per instance.
					d.generic = append(d.generic, line)
				} else {
					d.invented = append(d.invented, line)
				}
			}
		}
	}
	d.shapes = o.compareShapes()
	sort.Strings(d.missing)
	sort.Strings(d.invented)
	sort.Strings(d.noEIR)
	sort.Strings(d.shapes)
	return d
}

// compareShapes compares the arguments of the static calls of source
// functions: where the generated code passes the address of a place in the
// frame, EIR must pass an address too, not a value loaded from memory that
// is not a pointer (review 5: a ring's value passed for its address lost
// every pointer stored into the ring).
func (o *oracle) compareShapes() []string {
	var out []string
	for _, f := range o.cf.Funcs() {
		id, ok := o.owner(f)
		if !ok || id == eir.NoFunc {
			continue
		}
		ef := o.p.Funcs[id]
		defs := map[eir.Reg]*eir.Instr{}
		byPos := map[token.Pos][]*eir.Call{}
		for _, b := range ef.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				if in.Dst != 0 {
					defs[in.Dst] = in
				}
				if in.Op == eir.OpCall && in.Call.Kind == eir.CallStatic && in.Call.Implicit == eir.ImplNone {
					byPos[in.Pos] = append(byPos[in.Pos], in.Call)
				}
			}
		}
		for _, c := range o.cf.Calls(f) {
			if c.Kind != lower.CallStatic || c.Implicit || c.Params == nil || c.Static == nil {
				continue
			}
			k, ok := o.sourceKey(c.Static)
			if !ok || k.obj == nil {
				continue
			}
			for _, ec := range byPos[c.Pos] {
				g := o.p.Funcs[ec.Callee].Key.Obj
				if g == nil || g.Origin() != k.obj.Origin() || len(ec.Args) != len(c.Params) {
					continue
				}
				for i, a := range c.Params {
					if a.Shape != lower.ArgFrameAddr || a.ByRef || a.Leaves != 1 {
						continue
					}
					d := defs[ec.Args[i]]
					if d == nil || d.Op != eir.OpLoad || d.Type == nil || pointerLike(d.Type) {
						continue
					}
					out = append(out, fmt.Sprintf("%s: argument %d of %s: lowering passes a frame address, EIR a loaded %s @ %s", ef.Name, i, k, d.Type, o.where(c.Pos)))
				}
			}
		}
	}
	return out
}

// pointerLike reports whether a value of T is a pointer (or a word that
// may be one).
func pointerLike(T types2.Type) bool {
	switch u := T.Underlying().(type) {
	case *types2.Pointer, *types2.Signature, *types2.Map, *types2.Chan, *types2.Interface, *types2.TypeParam:
		return true
	case *types2.Basic:
		return u.Kind() == types2.UnsafePointer || u.Kind() == types2.Uintptr
	case *types2.Wrapper:
		return u.IsPointer()
	}
	return types2.PointerShaped(T)
}

// TestOracle compares the calls of EIR with those internal/lower emits
// over the corpus and the review programs (PLUTO_ESCAPE_ORACLE=1; it
// lowers every program). PLUTO_ESCAPE_ORACLE_OUT=file writes every
// difference found.
// oracleSample are the programs the normal suite runs the oracle over:
// each exercises desugarings that differ most from the source (defers and
// closures in range bodies, closure regions, generic methods and
// interfaces, rings, iterators of the library).
var oracleSample = map[string]bool{
	"run/rangefuncdefer":    true,
	"run/rangefuncmore":     true,
	"run/closureregions":    true,
	"run/closureregions2":   true,
	"run/closureifacechain": true,
	"run/tpmethods":         true,
	"run/tp_ifaceconv":      true,
	"lib/bytes":             true,
	"lib/hash":              true,
}

func TestOracle(t *testing.T) {
	all := os.Getenv("PLUTO_ESCAPE_ORACLE") != ""
	if !all && testing.Short() {
		t.Skip("lowers and builds the EIR of sample programs")
	}
	only := os.Getenv("PLUTO_ESCAPE_ORACLE_ONLY")
	var mu sync.Mutex
	var report []string
	progs := corpus(t)
	if dir := os.Getenv("PLUTO_ESCAPE_ORACLE_DIR"); dir != "" {
		// One program, such as a translated compiler (selfhost/demo/...).
		progs = []corpusProgram{{name: dir, dir: dir}}
	}
	for _, cp := range progs {
		if only != "" && !strings.Contains(cp.name, only) {
			continue
		}
		if !all && !oracleSample[cp.name] {
			continue
		}
		t.Run(cp.name, func(t *testing.T) {
			prog, ok := cp.load(t)
			if !ok {
				t.Skip("does not load")
			}
			d := runOracle(t, cp, prog)
			mu.Lock()
			for _, m := range d.missing {
				report = append(report, "missing "+m)
			}
			for _, m := range d.invented {
				report = append(report, "invented "+m)
			}
			for _, m := range d.noEIR {
				report = append(report, "noeir "+m)
			}
			for _, m := range d.shapes {
				report = append(report, "shape "+m)
			}
			for _, m := range d.generic {
				report = append(report, "generic "+m)
			}
			mu.Unlock()
			for _, m := range d.missing {
				t.Errorf("missing in EIR: %s", m)
			}
			for _, m := range d.invented {
				t.Errorf("invented by EIR: %s", m)
			}
			for _, m := range d.noEIR {
				t.Errorf("no EIR function: %s", m)
			}
			for _, m := range d.shapes {
				t.Errorf("argument shape: %s", m)
			}
		})
	}
	if out := os.Getenv("PLUTO_ESCAPE_ORACLE_OUT"); out != "" {
		sort.Strings(report)
		var b strings.Builder
		for _, r := range report {
			fmt.Fprintln(&b, r)
		}
		if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
			t.Error(err)
		}
	}
}
