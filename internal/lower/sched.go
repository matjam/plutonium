package lower

// Link-time binding of the scheduler backend (language spec section 19.5,
// abi.md section 12). A package-level variable marked //pu:scheduler, whose
// type implements pluto/rt.Scheduler, is the program's backend. The runtime
// declares body-less hooks (schedPark, schedWake and the rest, listed in
// schedHooks); lowering gives each a body that calls the bound variable's
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
		l.errorf(b.pos, diag.BadScheduler, "%s is a second //pu:scheduler variable; the first is %s at %s: a program binds one scheduler (language spec section 19.5)",
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

// How a scheduler hook treats a backend without the method.
type hookMode int

const (
	hookRequired hookMode = iota // a method of rt.Scheduler: every backend has it
	hookOptional                 // the hook does nothing, or returns zero values
	hookTimers                   // a timer method: a build error if the program reaches the hook
)

// A schedHookInfo is what a scheduler hook calls: the method of the bound
// scheduler, and what happens if the backend lacks it.
type schedHookInfo struct {
	method string
	mode   hookMode
}

// schedHooks are pluto/rt's body-less scheduler hooks (sched.p, timer.p),
// by name. The runtime and the compiler agree on them by name, as on the
// panic hook (callPanicHandler) and the intrinsics. Yield is optional only
// for the build check: runtime locks, which every program uses, may yield
// when they release, but only once tasks have started, which needs the
// other hooks.
var schedHooks = map[string]schedHookInfo{
	"schedNewTask":     {"NewTask", hookRequired},
	"schedFreeTask":    {"FreeTask", hookRequired},
	"schedSpawn":       {"Spawn", hookRequired},
	"schedPark":        {"Park", hookRequired},
	"schedWake":        {"Wake", hookRequired},
	"schedYield":       {"Yield", hookOptional},
	"schedInit":        {"Init", hookOptional},
	"schedInInterrupt": {"InInterrupt", hookOptional},
	"schedNow":         {"Now", hookTimers},
	"schedNowOpt":      {"Now", hookOptional},
	"schedArmTimer":    {"ArmTimer", hookTimers},
}

// isRuntime reports whether fn is declared in pluto/rt.
func isRuntime(fn *types2.Func) bool { return fn.Pkg() != nil && fn.Pkg().Path() == "pluto/rt" }

// schedHook returns the method the scheduler hook fn calls and what
// happens if the backend lacks it, if fn is one.
func schedHook(fn *types2.Func) (method string, mode hookMode, ok bool) {
	h, ok := schedHooks[fn.Name()]
	if !ok || !isRuntime(fn) {
		return "", hookRequired, false
	}
	return h.method, h.mode, true
}

// A lateHook is a body-less runtime function listed in lateHooks
// (rt.Sleep). Whether the program starts the
// scheduler is known only once every function it reaches has been lowered,
// so the hook gets its body then: a call of direct, a function that needs
// no scheduler, if the program does not start one, otherwise a call of
// parked. A program that sleeps and does nothing else with tasks,
// channels or timers thus links no scheduler thread (language spec section
// 19.5).
type lateHook struct {
	sym            *ssa.Sym
	sig            *types2.Signature
	direct, parked *types2.Func
	idle           bool // direct whenever the program starts no scheduler (lateIdleHooks)
}

// lateHooks are pluto/rt's body-less functions whose body is chosen once
// the whole program is lowered, by name: the function that needs no
// scheduler, then the one that parks.
var lateHooks = map[string][2]string{
	"Sleep":    {"sleepDirect", "sleepParked"},
	"mainWait": {"mainWaitDirect", "mainWaitParked"},
}

// lateIdleHooks are the late hooks whose first function is chosen by any
// program that does not start the scheduler, whatever backend it binds or
// leaves unbound (rt.mainWait: a program with no scheduler has no other task
// to wait for, language spec section 19.5).
var lateIdleHooks = map[string]bool{"mainWait": true}

// lateHookNames returns the names of the functions the late hook fn
// chooses between, if fn is one.
func lateHookNames(fn *types2.Func) (direct, parked string, ok bool) {
	h, ok := lateHooks[fn.Name()]
	if !ok || !isRuntime(fn) {
		return "", "", false
	}
	return h[0], h[1], true
}

// lateHookSym returns the symbol of the hook fn, whose body is chosen by
// resolveLateHooks (at once, if that has run).
func (l *Lowerer) lateHookSym(fn *types2.Func, direct, parked string) *ssa.Sym {
	name := funcSymName(fn, nil)
	if sym := l.funcs[name]; sym != nil {
		return sym
	}
	sym := l.out.Sym(name, ssa.SymFunc)
	l.funcs[name] = sym
	l.gxWrapped[sym] = fn
	h := &lateHook{sym: sym, sig: fn.Type().(*types2.Signature), direct: l.rtFunc(direct), parked: l.rtFunc(parked), idle: lateIdleHooks[fn.Name()]}
	if l.lateDone {
		l.queueLateHook(h)
	} else {
		l.late = append(l.late, h)
	}
	return sym
}

// startsScheduler reports whether the lowered program starts the
// scheduler: it reaches rt.startTasks, from a task statement, a blocking
// channel operation or wait, a timer, or the poller.
func (l *Lowerer) startsScheduler() bool {
	return l.funcs[funcSymName(l.rtFunc("startTasks"), nil)] != nil
}

// resolveLateHooks gives the pending late hooks their bodies, once the
// queue of functions is empty, and reports whether there were any. The
// direct functions are for the default hosted backend only: a program that
// binds its own scheduler, as a kernel does, gets the parked ones, which
// use that scheduler's clock and timers.
func (l *Lowerer) resolveLateHooks() bool {
	if len(l.late) == 0 {
		return false
	}
	l.lateIdle = !l.startsScheduler()
	l.lateDirect = l.sched != nil && l.sched.v.Pkg().Path() == "pluto/rt" && l.lateIdle
	l.lateDone = true
	late := l.late
	l.late = nil
	for _, h := range late {
		l.queueLateHook(h)
	}
	return true
}

func (l *Lowerer) queueLateHook(h *lateHook) {
	target := h.parked
	if l.lateDirect || h.idle && l.lateIdle {
		target = h.direct
		l.lateChoseDirect = true
	}
	l.queue = append(l.queue, &funcJob{sym: h.sym, sig: h.sig, build: func(s *fnState) {
		in := s.incomingArgs(h.sig)
		res := s.emitCall(callee{sym: l.funcSym(target, nil)}, h.sig, nil, in, token.NoPos)
		s.returnValues(res)
	}})
}

// checkLateHooks reports a compiler failure: a hook bound to a function
// that needs no scheduler in a program that, as it turned out, starts one.
func (l *Lowerer) checkLateHooks() {
	if l.lateChoseDirect && l.startsScheduler() {
		l.internalf(l.prog.Main.Files[0].Package, "a call chosen to avoid the scheduler reaches it")
	}
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
	l.gxWrapped[sym] = fn
	if l.sched == nil && mode != hookOptional && !l.schedMissing {
		l.schedMissing = true
		l.errorf(l.prog.Main.Files[0].Package, diag.NoScheduler, "this program starts tasks, uses channels or waits in pluto/sync, so it needs a scheduler, and none is bound: "+
			"mark a package-level variable whose type implements rt.Scheduler with //pu:scheduler (language spec section 19.5; hosted builds bind one by default)")
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
			cur = s.deref(s.load(ssa.TPtr, addr), pointerElem(T), token.NoPos)
		}
		_, wantPtr := m.Type().(*types2.Signature).Recv().Type().(*types2.Pointer)
		recv := s.receiverAt(cur, wantPtr, path)
		res := s.emitCall(callee{sym: l.funcSym(m, nil), noReturn: l.isNoReturn(m)}, l.methodSig(m, nil), &recv, in, token.NoPos)
		s.returnValues(res)
	}})
	return sym
}
