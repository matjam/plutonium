package race

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Messages of the check of scoped tasks (scope.go).

// placeName writes a place as the program would.
func (fc *frameCheck) placeName(p vpath) string {
	switch p.root.kind {
	case rGlobal:
		return fc.a.placeText(p)
	case rLocal:
		return p.root.v.Name() + pathText(p.path)
	case rParam:
		params := fc.starter.params
		if d := fc.starter.unit.fns[len(fc.starter.unit.fns)-1]; p.root.idx >= len(params) && p.root.idx < len(d.params) {
			params = d.params
		}
		if p.root.idx < len(params) && params[p.root.idx].Name() != "" {
			return params[p.root.idx].Name() + pathText(p.path)
		}
		return "the memory a parameter points to"
	case rUnknown:
		if p.root.v != nil {
			return "what " + p.root.v.Name() + " points to"
		}
	case rFresh:
		return "the memory allocated at " + fc.a.short(p.root.node.Pos())
	}
	return "memory"
}

// partyName names a party in a message.
func (fc *frameCheck) partyName(p *party, plural bool) string {
	if p.task == nil {
		return fc.starter.name
	}
	if plural {
		return "the tasks started at " + fc.a.short(p.task.Call.Pos())
	}
	return "the task started at " + fc.a.short(p.task.Call.Pos())
}

// accWhere describes where a use is: its line, and the call that leads to
// it when it is in a function the party calls.
func (fc *frameCheck) accWhere(x *frameAcc) string {
	if x.via.IsValid() && x.via != x.pos {
		return fmt.Sprintf("%s, in %s called at %s", fc.a.short(x.pos), fc.a.funcAt(x.pos), fc.a.short(x.via))
	}
	return fc.a.short(x.pos)
}

func passive(x *frameAcc) string {
	switch {
	case x.move:
		return "is moved"
	case x.write:
		return "is written"
	}
	return "is read"
}

func verb(x *frameAcc) string {
	switch {
	case x.move:
		return "moves"
	case x.write:
		return "writes"
	}
	return "reads"
}

// joinText says where the Scope of task q waits for it after pos.
func (fc *frameCheck) joinText(q *party, pos token.Pos) string {
	sc := q.task.Scope
	name := sc.Var.Name()
	for _, c := range sc.Waits {
		if c.Pos() > pos {
			return fmt.Sprintf("%s waits for it at %s", name, fc.a.short(c.Pos()))
		}
	}
	if sc.Node != nil {
		return fmt.Sprintf("%s waits for it where its block ends, at %s", name, fc.a.short(sc.Node.End()-1))
	}
	return name + " waits for it when its block ends"
}

// element reports whether a place is an element of an array or slice.
func element(p vpath) bool {
	ss := steps(stripParts(p.path))
	return len(ss) > 0 && (ss[len(ss)-1] == "[]" || ss[len(ss)-1] == "%")
}

// report reports a race.
func (fc *frameCheck) report(c conflictRec) {
	x, y, p, q := c.x, c.y, c.p, c.q
	if p.task != nil && !x.write && y.write {
		x, y, p, q = y, x, q, p
	}
	place := fc.placeName(x.place)
	key := fmt.Sprintf("scope|%s|%d|%d", stripParts(x.place.key()), p.idx, q.idx)
	var d *diag.Diagnostic
	switch {
	case p.task == nil && x.move:
		what := "move"
		if x.drop {
			what = "drop"
		}
		d = diag.New(fc.a.pos(x.pos), diag.ScopeMoveInUse, "cannot %s %s here: %s may still be using it", what, place, fc.partyName(q, false))
		d.Text = fmt.Sprintf("%s %s %s at %s, and %s, so the task could use %s after it has moved or been freed.",
			capitalize(fc.partyName(q, false)), verb(y), fc.placeName(y.place), fc.accWhere(y), fc.joinText(q, x.pos), place)
		d.Fix = &diag.Fix{Intro: "Wait for the task first", Code: q.task.Scope.Var.Name() + ".Wait()"}
	case p.task == nil:
		if x.write {
			d = diag.New(fc.a.pos(x.pos), diag.ScopeRace, "%s is written here while %s may use it", place, fc.partyName(q, false))
		} else {
			d = diag.New(fc.a.pos(x.pos), diag.ScopeRace, "%s is read here while %s may write it", place, fc.partyName(q, false))
		}
		d.Text = fmt.Sprintf("%s %s %s at %s, and %s. Meanwhile %s %s at %s in %s, so the two may run at the same time, one of them writing: a data race.",
			capitalize(fc.partyName(q, false)), verb(y), fc.placeName(y.place), fc.accWhere(y), fc.joinText(q, x.pos), place, passive(x), fc.accWhere(x), fc.starter.name)
		fc.aliasText(d, c)
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Wait for the tasks before using %s", place), Code: q.task.Scope.Var.Name() + ".Wait()"}
		d.More = []*diag.Fix{{Intro: "Or lock one sync.Mutex around every use of it, in the tasks and here", Code: "mu.Lock()\n...\nmu.Unlock()"}}
	case c.self && element(x.place) && element(y.place) && !c.alias:
		d = diag.New(fc.a.pos(x.pos), diag.ScopeNotPartitioned, "the tasks started at %s may write the same element of %s", fc.a.short(p.task.Call.Pos()), fc.placeName(vpath{root: x.place.root, path: trimElem(x.place.path)}))
		d.Text = fmt.Sprintf("Each task %s %s at %s, and several of them run at the same time. Tasks may write one array or slice at once only when each writes elements no other can reach, and the compiler can see that only for x[i], or a window such as x[i*k:(i+1)*k] or x[i:i+k], where i is the variable of the loop that starts them (each task takes its own i), or for x[:m] and x[m:].", verb(x), place, fc.accWhere(x))
		d.Fix = &diag.Fix{Intro: "Index by the loop variable", Code: q.task.Scope.Var.Name() + ".Task(func() { out[i] = f(i) })"}
		d.More = []*diag.Fix{{Intro: "Or give each task a window and index within it", Code: "row := out[i*w : (i+1)*w]"}, {Intro: "Or send the results on a channel and store them in the function that starts the tasks."}}
	case c.self:
		d = diag.New(fc.a.pos(x.pos), diag.ScopeRace, "%s is written by %s at the same time as each other", place, fc.partyName(p, true))
		d.Text = fmt.Sprintf("Each call of %s.Task at %s starts a task, and they run at the same time. Each %s %s (%s), and another %s it (%s). Two tasks using one place at once, one of them writing, is a data race: an update can be lost, and a value larger than a word can be read half written.",
			p.task.Scope.Var.Name(), fc.a.short(p.task.Call.Pos()), verb(x), place, fc.accWhere(x), verb(y), fc.accWhere(y))
		fc.aliasText(d, c)
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Lock a mutex around every use of %s", place), Code: "mu.Lock()\n...\nmu.Unlock()"}
		d.More = []*diag.Fix{{Intro: "Or, for a number, use an atomic type such as atomic.Int64."}, {Intro: "Or give each task its own element: results[i] = ..., where i is the loop variable."}}
	default:
		d = diag.New(fc.a.pos(x.pos), diag.ScopeRace, "%s is written by %s while %s uses it", place, fc.partyName(p, false), fc.partyName(q, false))
		d.Text = fmt.Sprintf("%s %s %s at %s, and %s %s %s at %s. The two tasks may run at the same time, one of them writing, which is a data race.",
			capitalize(fc.partyName(p, false)), verb(x), place, fc.accWhere(x), fc.partyName(q, false), verb(y), fc.placeName(y.place), fc.accWhere(y))
		fc.aliasText(d, c)
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("Lock a mutex around every use of %s", place), Code: "mu.Lock()\n...\nmu.Unlock()"}
		d.More = []*diag.Fix{{Intro: "Or, for a number, use an atomic type such as atomic.Int64."}}
	}
	fc.a.add(key, d)
}

// aliasText explains a conflict between two views that may point to the
// same memory.
func (fc *frameCheck) aliasText(d *diag.Diagnostic, c conflictRec) {
	if !c.alias {
		return
	}
	d.Text += fmt.Sprintf("\n%s and %s are reached through different views, which may point to the same memory: the compiler cannot tell where the memory they look at comes from, and both may hold values of type %s. Two slices of one type are accepted when the task uses each only as x[i], with the same loop variable i: the program then checks, before each task starts, that the slices start at the same element or do not overlap.",
		fc.placeName(c.x.place), fc.placeName(c.y.place), types2.TypeString(c.x.typ, fc.a.qualifier))
}

// trimElem cuts a path after its last element step, for naming the array
// or slice.
func trimElem(p string) string {
	p = stripParts(p)
	ss := steps(p)
	for len(ss) > 0 {
		last := ss[len(ss)-1]
		ss = ss[:len(ss)-1]
		if last == "[]" || last == "%" {
			break
		}
	}
	return strings.Join(ss, "")
}

// checkTakenWrites reports the variables task p takes and changes that the
// starter uses afterwards (E3407): the starter would not see the change.
func (fc *frameCheck) checkTakenWrites(p *party) {
	for _, x := range p.takenW {
		v := x.place.root.v
		if v == nil || isOwnerVar(v) {
			continue
		}
		use := fc.usedAfter(p, v)
		if use == nil {
			continue
		}
		name := v.Name()
		d := diag.New(fc.a.pos(x.pos), diag.ScopeTakenWrite, "the task started at %s changes %s, but has its own copy of it, so the change is not seen at %s", fc.a.short(p.task.Call.Pos()), name, fc.a.short(use.Pos()))
		d.Text = fmt.Sprintf("%s is declared in a block that may end before %s waits for the task (%s), so the task takes its own copy of %s when %s.Task runs, as each iteration of a Go loop has its own variables. The task changes its copy at %s, and %s uses %s again at %s, where it still holds its old value.",
			name, p.task.Scope.Var.Name(), fc.a.short(v.Pos()), name, p.task.Scope.Var.Name(), fc.a.short(x.pos), fc.starter.name, name, fc.a.short(use.Pos()))
		d.Fix = &diag.Fix{Intro: fmt.Sprintf("If only the task needs %s, declare it inside the function literal; if %s needs the result, give each task its own element of a variable declared before the loop", name, fc.starter.name), Code: "results[i] = ..."}
		fc.a.add(fmt.Sprintf("taken|%d|%p", p.idx, v), d)
	}
}

// usedAfter returns a use of v by the starter that may come after task p
// starts: later in the source, or in a loop around the call.
func (fc *frameCheck) usedAfter(p *party, v *types2.Var) *ast.Ident {
	call := p.task.Call
	var loop ast.Node
	if len(p.task.Loops) > 0 {
		loop = p.task.Loops[0].Stmt
	}
	var found *ast.Ident
	ast.Inspect(fc.starter.body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			for _, l := range p.lits {
				if l == lit {
					return false
				}
			}
		}
		id, ok := n.(*ast.Ident)
		if !ok || fc.a.uses[id] != v {
			return true
		}
		inCall := call.Pos() <= id.Pos() && id.Pos() < call.End()
		if id.Pos() > call.End() || loop != nil && loop.Pos() <= id.Pos() && id.Pos() < loop.End() && !inCall {
			found = id
		}
		return true
	})
	return found
}

func isOwnerVar(v *types2.Var) bool { return types2.IsOwner(v.Type()) }

// reportConc reports E3406: c.f, called from several tasks at once by
// c.name, writes x without a lock.
func (fc *frameCheck) reportConc(c concCall, x *frameAcc) {
	place := fc.placeName(x.place)
	what := "the function literal passed here"
	if c.f.lit == nil {
		what = c.f.name + ", passed here,"
	}
	at := c.pos
	if c.f.lit != nil {
		at = c.f.lit.Pos()
	}
	d := diag.New(fc.a.pos(at), diag.ScopeConcurrentCall, "%s calls %s from several tasks at once, and %s writes %s", c.name, c.param, what, place)
	d.Text = fmt.Sprintf("%s starts tasks that call %s, several at the same time. %s writes %s at %s with no lock held, so the calls would write it at the same time: a data race.",
		c.name, c.param, capitalize(strings.TrimSuffix(what, ",")), place, fc.accWhere(x))
	d.Fix = &diag.Fix{Intro: "Use an atomic type for it, or lock a mutex around the write", Code: "mu.Lock()\n...\nmu.Unlock()"}
	fc.a.add(fmt.Sprintf("conc|%d|%p", c.pos, c.f), d)
}
