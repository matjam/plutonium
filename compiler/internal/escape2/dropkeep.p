package escape2

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/sched"
	"compiler/internal/load"
	"compiler/internal/syntax/token"
	"compiler/pu"
)

// dropKeepErrors reports the Drop methods that keep their receiver, or a
// part of the owner it points to, where it outlives the call: in package
// memory, the heap, a channel or a task (E3002). A Drop method's receiver
// is the owner being destroyed: its fields are dropped and its memory
// released when Drop returns (section 18.6, What dropping does), so what
// Drop keeps of it is a view of memory that is gone. The callers cannot
// check this, since the drop glue that calls Drop may run on memory the
// analysis does not see, such as a vector's heap buffer.
func dropKeepErrors(prog *load.Program, p *eir.Program, res *sched.Result) []*diag.Diagnostic {
	if p == nil || res == nil || res.Env == nil {
		return nil
	}
	env := res.Env
	ps := env.Paths()
	var out []*diag.Diagnostic
	for _, f := range p.Funcs {
		if f.Bodyless || f.Key.Obj == nil || f.Key.Obj.Name() != "Drop" || f.Sig == nil || f.Sig.Recv() == nil || len(f.Inputs) == 0 {
			continue
		}
		sum := env.Summary(f.ID)
		if sum == nil {
			continue
		}
		for e, k := range sum.Esc {
			if k.Input() != 0 || k.Deep() || ps.Loads(k.Path()) != 0 {
				continue
			}
			pos := f.Pos
			if len(sum.Reasons) == len(sum.Esc)+len(sum.Store) && sum.Reasons[e].Pos != 0 {
				pos = token.Pos(sum.Reasons[e].Pos)
			}
			recv := f.Sig.Recv().Name()
			if recv == "" || recv == "_" {
				recv = "the receiver"
			}
			d := diag.New(prog.Fset.Position(pos), diag.StoreFrameView, "%s keeps %s past the call, but the owner it points to is destroyed when Drop returns", pu.Box(f.Name), pu.Box(recv))
			d.Text = compat.Sprintfb("A Drop method's receiver points to the owner being dropped: its fields are dropped and its memory released when Drop returns (language spec section 18.6, What dropping does). Keeping %s, or a part of the owner, in package memory, the heap, a channel or a task leaves a view of memory that is gone.", compat.ArgS(recv))
			d.Fix = pu.New(diag.Fix{Intro: "Copy the values Drop needs to keep out of the owner, rather than keeping a pointer into it"})
			out = compat.Append(out, d)
			break
		}
	}
	return out
}
