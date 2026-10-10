package lower

// Link-time binding of the scheduler backend (language spec section 4.7,
// abi.md section 12). A package-level variable marked //pu:scheduler, whose
// type implements pluto/rt.Scheduler, is the program's backend. The runtime
// declares body-less hooks marked //pu:schedhook (schedPark, schedWake and
// the rest); lowering gives each a body that calls the bound variable's
// method of the same name directly. pluto/rt binds the hosted backend in
// hosted builds; a program's own binding replaces it. A program that
// reaches a hook with nothing bound fails to build.

import (
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// A schedBinding is a //pu:scheduler variable.
type schedBinding struct {
	v   *types2.Var
	pos token.Pos
}

// findScheduler finds the program's //pu:scheduler variable: the program's
// own if it has one, otherwise the runtime's default. It reports bindings
// that are not package-level variables of a concrete type implementing
// rt.Scheduler, and more than one binding in the program's own packages.
func (l *Lowerer) findScheduler() {
	var own, dflt []schedBinding
	for _, p := range l.prog.Packages {
		for _, b := range schedulerVars(p) {
			if p.Path == "pluto/rt" {
				dflt = append(dflt, b)
			} else {
				own = append(own, b)
			}
		}
	}
	for _, b := range own[min(len(own), 1):] {
		l.errorf(b.pos, diag.BadScheduler, "%s is a second //pu:scheduler variable; the first is %s at %s: a program binds one scheduler (language spec section 4.7)",
			b.v.Name(), own[0].v.Name(), l.fset.Position(own[0].pos))
		l.schedMissing = true // no "none is bound" on top
	}
	var b *schedBinding
	switch {
	case len(own) > 0:
		b = &own[0]
	case len(dflt) > 0:
		b = &dflt[0]
	default:
		return
	}
	T := b.v.Type()
	bad := func(format string, args ...any) {
		l.errorf(b.pos, diag.BadScheduler, format, args...)
		l.schedMissing = true // no "none is bound" on top
	}
	if isInterface(T) {
		bad("//pu:scheduler variable %s has interface type %s; give it the backend's own type, so that the runtime calls its methods directly", b.v.Name(), T)
		return
	}
	iface := l.rtType("Scheduler").Underlying().(*types2.Interface)
	if !types2.Implements(T, iface) && !types2.Implements(types2.NewPointer(T), iface) {
		m, _ := types2.MissingMethod(types2.NewPointer(T), iface, true)
		name := "?"
		if m != nil {
			name = m.Name()
		}
		bad("//pu:scheduler variable %s of type %s does not implement rt.Scheduler (missing method %s)", b.v.Name(), T, name)
		return
	}
	if n, ok := types2.Unalias(T).(*types2.Named); ok && n.TypeArgs().Len() > 0 {
		bad("//pu:scheduler variable %s has generic type %s, which is not supported", b.v.Name(), T)
		return
	}
	l.sched = b
}

// schedulerVars returns the variables of p marked //pu:scheduler.
func schedulerVars(p *load.Package) []schedBinding {
	var out []schedBinding
	for _, f := range p.Files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				doc := vs.Doc
				if doc == nil {
					doc = gd.Doc
				}
				for _, dir := range directives(doc) {
					if dir != "scheduler" {
						continue
					}
					for _, n := range vs.Names {
						if v, ok := p.Info.Defs[n].(*types2.Var); ok {
							out = append(out, schedBinding{v: v, pos: n.Pos()})
						}
					}
				}
			}
		}
	}
	return out
}

// How a //pu:schedhook declaration treats a backend without the method.
type hookMode int

const (
	hookRequired hookMode = iota // a method of rt.Scheduler: every backend has it
	hookOptional                 // the hook does nothing, or returns zero values
	hookTimers                   // a timer method: a build error if the program reaches the hook
)

// schedHook returns the method a //pu:schedhook declaration calls and what
// happens if the backend lacks it ("optional" or "timers" after the name).
func schedHook(d *ast.FuncDecl) (method string, mode hookMode, ok bool) {
	for _, dir := range directives(d.Doc) {
		if rest, found := strings.CutPrefix(dir, "schedhook "); found {
			f := strings.Fields(rest)
			if len(f) == 0 {
				return "", hookRequired, false
			}
			mode := hookRequired
			if len(f) > 1 {
				switch f[1] {
				case "optional":
					mode = hookOptional
				case "timers":
					mode = hookTimers
				}
			}
			return f[0], mode, true
		}
	}
	return "", hookRequired, false
}

// schedMethod returns the method of the bound scheduler b named name, and
// the path to it through embedded fields, or nil.
func schedMethod(b *schedBinding, name string) (*types2.Func, []int) {
	if b == nil {
		return nil, nil
	}
	obj, index, _ := types2.LookupFieldOrMethod(b.v.Type(), true, b.v.Pkg(), name)
	m, _ := obj.(*types2.Func)
	return m, index
}

// schedHookSym returns the function implementing the runtime hook fn,
// which calls method of the bound scheduler.
func (l *Lowerer) schedHookSym(fn *types2.Func, method string, mode hookMode) *ssa.Sym {
	name := funcSymName(fn, nil)
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	sig := fn.Type().(*types2.Signature)
	if l.sched == nil && mode != hookOptional && !l.schedMissing {
		l.schedMissing = true
		l.errorf(l.prog.Main.Files[0].Package, diag.NoScheduler, "this program starts puroutines, uses channels or waits in pluto/sync, so it needs a scheduler, and none is bound: "+
			"mark a package-level variable whose type implements rt.Scheduler with //pu:scheduler (language spec section 4.7; hosted builds bind one by default)")
	}
	if b := l.sched; b != nil && mode == hookTimers && !l.schedNoTimers {
		var missing []string
		for _, want := range []string{"Now", "ArmTimer"} {
			if m, _ := schedMethod(b, want); m == nil {
				missing = append(missing, want)
			}
		}
		if len(missing) > 0 {
			l.schedNoTimers = true
			l.errorf(b.pos, diag.SchedulerTimers, "this program uses timers (time.Sleep, a time.Timer or Ticker, rt.ParkUntil or rt.Now), so its scheduler must provide a clock and a timer, and %s of type %s has no method %s (abi.md section 12.9)",
				b.v.Name(), b.v.Type(), strings.Join(missing, " or "))
		}
	}
	b := l.sched
	l.queue = append(l.queue, &funcJob{sym: sym, sig: sig, build: func(s *fnState) {
		in := s.incomingArgs(sig)
		m, path := schedMethod(b, method)
		if m == nil {
			// An optional method the backend lacks, or a build that has
			// already failed: return the zero values.
			var res []value
			for i := 0; i < sig.Results().Len(); i++ {
				res = append(res, s.zeroValue(sig.Results().At(i).Type()))
			}
			s.returnValues(res)
			return
		}
		T := b.v.Type()
		addr := s.symAddr(l.globalSym(b.v))
		cur := lvalue{ptr: addr, T: T}
		if isPointerLike(T) {
			cur = lvalue{ptr: s.load(ssa.TPtr, addr), T: pointerElem(T)}
		}
		_, wantPtr := m.Type().(*types2.Signature).Recv().Type().(*types2.Pointer)
		recv := s.receiverAt(cur, wantPtr, path)
		res := s.emitCall(callee{sym: l.funcSym(m, nil), noReturn: l.isNoReturn(m)}, l.methodSig(m, nil), &recv, in, token.NoPos)
		s.returnValues(res)
	}})
	return sym
}
