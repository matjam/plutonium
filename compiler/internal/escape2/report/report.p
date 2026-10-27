// Translated by go2pu from internal/escape2/report/report.go at the go-cutover tag; edited by hand since.

// Package report turns the findings of the escape analysis
// (internal/escape2) into diagnostics: the messages of plan
// docs/plans/2026-10-20-escape-redesign.md, stage 5.
//
// A finding says which frame object of which function reaches what place,
// and where. The message is written in the program's own terms: the
// expression that holds the view (from the source at the finding's
// position and the witness solve.Explain gives), the variable or literal it
// looks at, the statement that keeps it, and, for a view a callee keeps,
// the chain of calls down to the store or sink that keeps it, read from the
// callees' summary reasons. No register, token or key of the analysis
// appears in the text.
//
// The text is that of internal/escape's messages (its report.go), so that
// the diagnostics of a program do not change when this analysis replaces
// that one, except where that text was wrong; those cases are noted where
// they are written.
package report

import (
	"pluto/sort"
	"pluto/strconv"
	"pluto/strings"
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/sched"
	"compiler/internal/escape2/solve"
	"compiler/internal/escape2/summary"
	"compiler/internal/load"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
)

// Diagnostics returns the diagnostics of the findings of res, the analysis
// of p, the EIR of prog. They are sorted by position; a message reported
// twice at one position (two findings that read the same) is reported once.
func Diagnostics(prog *load.Program, p *eir.Program, res *sched.Result) []*diag.Diagnostic {
	r := newReporter(prog, p, res)
	var out []*diag.Diagnostic
	seen := map[string]bool{}
	var countedAs []*diag.Diagnostic
	for _, fd := range res.Findings {
		if r.isCountedAsFinding(fd) {
			countedAs = r.countedAsDiagnostics() // counted.go
			break
		}
	}
	out = compat.Append(out, countedAs...)
	// A view of a statement temporary is reported where it is first kept
	// (the findings are sorted by position): later stores of the place
	// that keeps it are the same error.
	tempKept := map[[2]int]bool{}
	for i, fd := range res.Findings {
		if r.explainedBy(fd, res.Findings, i) {
			continue
		}
		if fd.Code == diag.TempViewKept && fd.Obj != eir.NoObj {
			if fn := r.p.Funcs[fd.Func]; int(fd.Obj) < len(fn.Objects) && fn.Objects[fd.Obj].Life == eir.LifeStmt {
				k := [2]int{int(fd.Func), int(fd.Obj)}
				if tempKept[k] {
					continue
				}
				tempKept[k] = true
			}
		}
		if len(countedAs) > 0 && r.isCountedAsFinding(fd) {
			continue // reported at the Unwrap methods (counted.go)
		}
		d := r.safeDiagnostic(fd)
		key := compat.Concat(d.Pos.String(), "|", string(d.Code), "|", d.Msg)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = compat.Append(out, d)
	}
	sort.SortStableFunc(out, func(goA, goB *diag.Diagnostic) bool {
		a, b := goA.Pos, goB.Pos
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	return out
}

// Unwritten is the message of a finding whose message could not be
// written: an error of this package, never of the program. The error is
// still reported, at the finding's position with its code.
const Unwritten = "a view of this function's frame outlives the frame here (the compiler could not describe it further)"

// safeDiagnostic is diagnostic, with a failure to write the message turned
// into Unwritten: the verdict stands whatever the text.
func (r *reporter) safeDiagnostic(fd solve.Finding) (d *diag.Diagnostic) {
	/* go2pu: deferred recover dropped: a panic is not recovered in Plutonium */
	_ = d
	_ = r
	_ = fd
	return r.diagnostic(fd)
}

// explainedBy reports whether finding i, about an interface's copy of a
// value, is explained by another finding at the same place about what the
// copied value points to: the copy reaches the place only because it holds
// that pointer, and the other message names the variable the program wrote.
func (r *reporter) explainedBy(fd solve.Finding, all []solve.Finding, i int) bool {
	fn := r.p.Funcs[fd.Func]
	if mem, ok := fn.ArenaMem[fd.Obj]; ok {
		// A local arena whose memory is reported at the same place: a
		// string buffer or box allocated from it records the arena.
		for j, o := range all {
			if j != i && o.Func == fd.Func && o.Pos == fd.Pos && o.Code == fd.Code && o.Obj == mem {
				return true
			}
		}
	}
	if fd.Obj == eir.NoObj || int(fd.Obj) >= len(fn.Objects) || fn.Objects[fd.Obj].Kind != eir.ObjBox {
		return false
	}
	box := r.exprAt(fn.Objects[fd.Obj].Pos)
	if box == nil {
		return false
	}
	info := r.info(box.Pos())
	for j, o := range all {
		if j == i || o.Func != fd.Func || o.Pos != fd.Pos || o.Code != fd.Code || o.Obj == eir.NoObj || int(o.Obj) >= len(fn.Objects) {
			continue
		}
		ob := &fn.Objects[o.Obj]
		if ob.Kind == eir.ObjBox {
			continue
		}
		if ob.Var != nil && mentions(box, ob.Var, info) || ob.Var == nil && contains(box, ob.Pos) {
			return true
		}
	}
	return false
}

// A reporter writes the messages of one analysis.
type reporter struct {
	fset  *token.FileSet
	prog  *load.Program
	p     *eir.Program
	res   *sched.Result
	env   solve.Env // nil when the run has none: no witnesses or key paths then
	files []srcFile // sorted by start
	ws    *solve.Workspace
}

type srcFile struct {
	f   *ast.File
	pkg *load.Package
}

func newReporter(prog *load.Program, p *eir.Program, res *sched.Result) *reporter {
	r := pu.New(reporter{fset: prog.Fset, prog: prog, p: p, res: res, env: res.Env})
	for _, pkg := range prog.Packages {
		for _, f := range pkg.Files {
			r.files = compat.Append(r.files, srcFile{f, pkg})
		}
	}
	sort.SortFunc(r.files, func(a, b srcFile) bool { return a.f.FileStart < b.f.FileStart })
	return r
}

// file returns the source file holding pos, or nil.
func (r *reporter) file(pos token.Pos) *srcFile {
	i := sort.Search(len(r.files), func(i int) bool { return r.files[i].f.FileEnd >= pos })
	if i < len(r.files) && r.files[i].f.FileStart <= pos {
		return &r.files[i]
	}
	return nil
}

// info returns the type information of the package holding pos.
func (r *reporter) info(pos token.Pos) *types2.Info {
	if sf := r.file(pos); sf != nil {
		return sf.pkg.Info
	}
	return nil
}

// enclosing returns the nodes of the source whose extent holds pos,
// outermost first.
func (r *reporter) enclosing(pos token.Pos) []ast.Node {
	sf := r.file(pos)
	if sf == nil || !pos.IsValid() {
		return nil
	}
	var path []ast.Node
	ast.Inspect(sf.f, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if n.Pos() <= pos && pos < n.End() {
			path = compat.Append(path, n)
			return true
		}
		return false
	})
	return path
}

// innermost returns the innermost node of path of type T.
func innermost[T ast.Node](path []ast.Node) (T, bool) {
	for i := len(path) - 1; i >= 0; i-- {
		if n, ok := path[i].(T); ok {
			return n, true
		}
	}
	var zero T
	return zero, false
}

// exprAt returns the outermost expression that starts at pos, or nil.
func (r *reporter) exprAt(pos token.Pos) ast.Expr {
	for _, n := range r.enclosing(pos) {
		if e, ok := n.(ast.Expr); ok && e.Pos() == pos {
			return e
		}
	}
	return nil
}

// callExprFor returns the call the source writes for a call instruction,
// or nil when there is none at its position. Calls nested as f(x)() start at
// one position; the one chosen calls the instruction's callee, or for a
// call of a function value, a value that is no declared function.
func (r *reporter) callExprFor(in *eir.Instr) *ast.CallExpr {
	if in == nil {
		return nil
	}
	var calls []*ast.CallExpr
	for _, n := range r.enclosing(in.Pos) {
		if ce, ok := n.(*ast.CallExpr); ok && ce.Pos() == in.Pos {
			calls = compat.Append(calls, ce)
		}
	}
	if len(calls) <= 1 {
		if len(calls) == 1 {
			return calls[0]
		}
		return nil
	}
	var want *types2.Func
	if c := in.Call; c != nil && c.Kind == eir.CallStatic && int(c.Callee) < len(r.p.Funcs) {
		want = r.p.Funcs[c.Callee].Key.Obj
	}
	for i := len(calls) - 1; i >= 0; i-- {
		obj := r.calledFunc(calls[i])
		if want != nil && obj != nil && obj.Origin() == want.Origin() || want == nil && obj == nil {
			return calls[i]
		}
	}
	return calls[len(calls)-1]
}

// witness returns the positions of the finding's witness (solve.Explain), or
// nil when there is no environment to solve in.
func (r *reporter) witness(fn *eir.Func, fd solve.Finding) (w []token.Pos) {
	if r.env == nil {
		return nil
	}
	/* go2pu: deferred recover dropped: a panic is not recovered in Plutonium */
	_ = w
	if r.ws == nil {
		a := domain.NewArena()
		r.ws = pu.New(solve.Workspace{Arena: a, Sets: domain.NewSets(a)})
	}
	return solve.Explain(fn, r.env, r.ws, fd)
}

// summaryOf returns the final summary of a function, virtual functions of
// the dynamic-call rule included.
func (r *reporter) summaryOf(id eir.FuncID) *summary.Summary {
	if r.env != nil {
		return r.env.Summary(id)
	}
	if int(id) < len(r.res.Summaries) {
		return r.res.Summaries[id]
	}
	return nil
}

// callAt returns the call instruction of site in fn.
func callAt(fn *eir.Func, site eir.SiteID) *eir.Instr {
	for _, b := range fn.Blocks {
		for i := range b.Instrs {
			if in := &b.Instrs[i]; in.Op == eir.OpCall && in.Call != nil && in.Call.Site == site {
				return in
			}
		}
	}
	return nil
}

// defOf returns the instruction that defines register x in fn.
func defOf(fn *eir.Func, x eir.Reg) *eir.Instr {
	if x == 0 {
		return nil
	}
	for _, b := range fn.Blocks {
		for i := range b.Instrs {
			if in := &b.Instrs[i]; in.Dst == x {
				return in
			}
		}
	}
	return nil
}

// funcName names a declared function as messages do: "load", "(*T).M".
func funcName(obj *types2.Func) string {
	sig, _ := obj.Type().(*types2.Signature)
	if sig != nil && sig.Recv() != nil {
		t := sig.Recv().Type()
		ptr := ""
		if p, ok := t.(*types2.Pointer); ok {
			t = p.Elem()
			ptr = "*"
		}
		if n, ok := types2.Unalias(t).(*types2.Named); ok {
			return compat.Concat("(", ptr, n.Obj().Name(), ").", obj.Name())
		}
	}
	return obj.Name()
}

// frameName names the function whose frame an object of fn lives in: the
// function as declared, or "the function literal".
func (r *reporter) frameName(fn *eir.Func) string {
	switch k := fn.Key; {
	case k.Obj != nil:
		return funcName(k.Obj)
	case k.Lit != nil:
		return "the function literal"
	case k.Range != nil:
		// The body of a range loop is a closure, but its variables are, in
		// the source, those of the function the loop is in.
		return r.declaredAround(k.Range.Pos())
	case k.Synth != nil && k.Synth.Method != nil:
		return funcName(k.Synth.Method)
	}
	return fn.Name
}

// declaredAround names the declared function whose body holds pos.
func (r *reporter) declaredAround(pos token.Pos) string {
	if fd, ok := innermost[*ast.FuncDecl](r.enclosing(pos)); ok {
		if info := r.info(pos); info != nil {
			if obj, ok := info.Defs[fd.Name].(*types2.Func); ok {
				return funcName(obj)
			}
		}
		return fd.Name.Name
	}
	return "the package initialization"
}

// label names a callee as a message does: relative to the package of the
// call, "elem", "(*Arr).first", "vec.(*Vec).Push".
func (r *reporter) label(g *eir.Func, from *types2.Package) string {
	switch k := g.Key; {
	case k.Obj != nil:
		name := funcName(k.Obj)
		if k.Obj.Pkg() != nil && k.Obj.Pkg() != from {
			return compat.Concat(k.Obj.Pkg().Name(), ".", name)
		}
		return name
	case k.Lit != nil:
		return compat.Concat("the function literal at ", r.basePos(k.Lit.Pos()))
	case k.Range != nil:
		return compat.Concat("the loop body at ", r.basePos(k.Range.Pos()))
	case k.Synth != nil && k.Synth.Method != nil:
		name := funcName(k.Synth.Method)
		if k.Synth.Kind == eir.SynthDropGlue {
			return dropName(k.Synth.Recv, from)
		}
		if p := k.Synth.Method.Pkg(); p != nil && p != from {
			return compat.Concat(p.Name(), ".", name)
		}
		return name
	case k.Synth != nil && k.Synth.Kind == eir.SynthDropGlue:
		return dropName(k.Synth.Recv, from)
	}
	return g.Name
}

func dropName(T types2.Type, from *types2.Package) string {
	return compat.Concat("the drop of ", types2.TypeString(T, types2.RelativeTo(from)))
}

// candidateName names a function a dynamic call may reach, as messages do
// after "which may call": a method with its package when withPkg is set
// ("main.(*S).String"), a literal as "func literal in keepAll at line 159".
func (r *reporter) candidateName(g *eir.Func, withPkg bool) string {
	switch k := g.Key; {
	case k.Obj != nil:
		if withPkg && k.Obj.Pkg() != nil {
			return compat.Concat(k.Obj.Pkg().Name(), ".", funcName(k.Obj))
		}
		return funcName(k.Obj)
	case k.Lit != nil:
		return compat.Concat("func literal in ", r.declaredAround(k.Lit.Pos()), " at line ", strconv.Itoa(r.line(k.Lit.Pos())))
	case k.Range != nil:
		return compat.Concat("the loop body at ", r.basePos(k.Range.Pos()))
	case k.Synth != nil && k.Synth.Method != nil && k.Synth.Recv != nil && !isInterface(k.Synth.Recv):
		// A wrapper of a method of a concrete type: that type's method.
		name := compat.Concat(recvName(k.Synth.Recv), ".", k.Synth.Method.Name())
		if p := recvPkg(k.Synth.Recv); withPkg && p != nil {
			return compat.Concat(p.Name(), ".", name)
		}
		return name
	case k.Synth != nil && k.Synth.Method != nil:
		return r.candidateName(&eir.Func{Key: eir.FuncKey{Obj: k.Synth.Method}, Name: g.Name}, withPkg)
	}
	return g.Name
}

// recvPkg returns the package that declares a receiver type, or nil.
func recvPkg(T types2.Type) *types2.Package {
	if p, ok := types2.Unalias(T).(*types2.Pointer); ok {
		T = p.Elem()
	}
	if n, ok := types2.Unalias(T).(*types2.Named); ok {
		return n.Obj().Pkg()
	}
	return nil
}

// recvName writes a receiver type as a method's name does: "(*T)", "(T)".
func recvName(T types2.Type) string {
	ptr := ""
	if p, ok := types2.Unalias(T).(*types2.Pointer); ok {
		T, ptr = p.Elem(), "*"
	}
	if n, ok := types2.Unalias(T).(*types2.Named); ok {
		return compat.Concat("(", ptr, n.Obj().Name(), ")")
	}
	return compat.Concat("(", ptr, types2.TypeString(T, nil), ")")
}

// destOf describes memory a store writes, as messages do: "n.next",
// "package variable G".
func destOf(lhs ast.Expr, info *types2.Info) (text string, global bool) {
	lhs = ast.Unparen(lhs)
	if info != nil {
		if id, ok := lhs.(*ast.Ident); ok {
			if v, ok := info.Uses[id].(*types2.Var); ok && isGlobal(v) {
				return compat.Concat("package variable ", v.Name()), true
			}
		}
		if se, ok := lhs.(*ast.SelectorExpr); ok && info.Selections[se] == nil {
			if v, ok := info.Uses[se.Sel].(*types2.Var); ok && isGlobal(v) {
				return compat.Concat("package variable ", types2.ExprString(se)), true
			}
		}
	}
	return types2.ExprString(lhs), false
}

func isGlobal(v *types2.Var) bool {
	return v != nil && !v.IsField() && v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
}

func refNoun(T types2.Type) string {
	if T == nil {
		return ""
	}
	switch T.Underlying().(type) {
	case *types2.Map:
		return "map"
	case *types2.Chan:
		return "channel"
	}
	return ""
}

func isBoxPtr(T types2.Type) bool {
	if T == nil {
		return false
	}
	b, ok := T.Underlying().(*types2.Wrapper)
	return ok && b.IsPointer()
}

// mentions reports whether e uses variable v.
func mentions(e ast.Node, v *types2.Var, info *types2.Info) bool {
	if e == nil || v == nil || info == nil {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && (info.Uses[id] == v || info.Defs[id] == v) {
			found = true
		}
		return !found
	})
	return found
}

// contains reports whether n's extent holds pos.
func contains(n ast.Node, pos token.Pos) bool {
	return n != nil && pos.IsValid() && n.Pos() <= pos && pos < n.End()
}

// captured returns the variables a function literal (or range body) uses
// that are declared outside it and are not package variables, in the order
// of their first use.
func captured(body ast.Node, from, to token.Pos, info *types2.Info) []*types2.Var {
	var out []*types2.Var
	if info == nil || body == nil {
		return nil
	}
	seen := map[*types2.Var]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		v, ok := info.Uses[id].(*types2.Var)
		if !ok || seen[v] || v.IsField() || isGlobal(v) || (v.Pos() >= from && v.Pos() < to) {
			return true
		}
		seen[v] = true
		out = compat.Append(out, v)
		return true
	})
	return out
}

// shortText trims the text of an expression for a label: "s.String".
func shortText(e ast.Expr) string {
	return strings.ReplaceAll(types2.ExprString(e), "…", "...")
}
