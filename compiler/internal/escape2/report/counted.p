// Translated by go2pu from internal/escape2/report/counted.go at the go-cutover tag; edited by hand since.

package report

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/escape2/solve"
	"compiler/internal/types2"
	"compiler/pu"
)

// The library's recursive borrow failure in errors.As, written about the
// program's wrapping errors (the countedAsReport of internal/escape).
// errors.As walks a tree of errors through their Unwrap methods and keeps,
// in the caller's target, a pointer taken out of a child; a child that an
// Unwrap method makes afresh (a counted error the wrapping error does not
// hold) is released when the walk leaves it (eir/borrow.go). The finding
// is in As, at its recursive call; the program's mistake is in the Unwrap
// method, which is where it is reported.

// isCountedAsFinding reports whether fd is a finding of errors.As's own
// borrow of an Unwrap result.
func (r *reporter) isCountedAsFinding(fd solve.Finding) bool {
	return r.p.Funcs[fd.Func].Name == "pluto/errors.As"
}

// countedAsDiagnostics returns the diagnostics of the Unwrap methods whose
// result may hold a counted error they make afresh, or nil when there is
// none (the As finding is reported as it is then).
func (r *reporter) countedAsDiagnostics() []*diag.Diagnostic {
	var out []*diag.Diagnostic
	target := r.asTarget()
	for id, f := range r.p.Funcs {
		fn := f.Key.Obj
		if fn == nil || fn.Name() != "Unwrap" || f.Key.Synth != nil {
			continue
		}
		sig, ok := fn.Type().(*types2.Signature)
		if !ok || sig.Recv() == nil || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
			continue
		}
		if int(id) >= len(r.res.Summaries) {
			continue
		}
		sm := r.res.Summaries[id]
		if sm == nil || len(sm.Res) == 0 || len(sm.Res[0].CountedHeap) == 0 {
			continue
		}
		d := diag.New(r.fset.Position(fn.Pos()), diag.PassFrameView, "cannot borrow an error returned by %s: the wrapping error may not keep it alive", pu.Box(methodName(fn)))
		d.Text = "Unwrap returns a counted error that is not held by the wrapping error. A pointer target of errors.As only borrows that child. The child is released when the search leaves it, so keeping the wrapping error alive does not keep the target alive."
		d.Fix = pu.New(diag.Fix{Intro: "Use a counted target matching the child's dynamic type (shared[T] or synced[T]), or store the child in a field of the wrapping error"})
		if target != nil {
			d.Fix.Code = compat.Sprintfb("var target synced[%s]\nerrors.As(err, &target)", compat.ArgS(types2.TypeString(target, types2.RelativeTo(fn.Pkg()))))
		}
		out = compat.Append(out, d)
	}
	return out
}

// asTarget returns the element type of a pointer target errors.As is
// instantiated with in the program, or nil.
func (r *reporter) asTarget() types2.Type {
	for _, pkg := range r.prog.Packages {
		if pkg.Info == nil {
			continue
		}
		for id, inst := range pkg.Info.Instances {
			obj, ok := pkg.Info.Uses[id].(*types2.Func)
			if !ok || obj.Pkg() == nil || obj.Pkg().Path() != "pluto/errors" || obj.Name() != "As" || inst.TypeArgs.Len() == 0 {
				continue
			}
			if p, ok := types2.Unalias(inst.TypeArgs.At(0)).(*types2.Pointer); ok {
				return p.Elem()
			}
		}
	}
	return nil
}

// methodName is fn's name with its receiver type: (T).M or (*T).M.
func methodName(fn *types2.Func) string {
	sig := fn.Type().(*types2.Signature)
	T := sig.Recv().Type()
	star := ""
	if p, ok := types2.Unalias(T).(*types2.Pointer); ok {
		star, T = "*", p.Elem()
	}
	name := types2.TypeString(T, types2.RelativeTo(fn.Pkg()))
	return compat.Concat("(", star, name, ").", fn.Name())
}
