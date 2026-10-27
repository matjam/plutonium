// Translated by go2pu from internal/escape2/eir/dump.go at the go-cutover tag; edited by hand since.

package eir

import (
	"compiler/compat"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"pluto/fmt"
	"pluto/io"
)

// Dump writes a function in a readable form, for tests and debugging.
func (p *Program) Dump(w io.Writer, fset *token.FileSet, f *Func) {
	_, _ = compat.Fprintfb(w, "func %s", compat.ArgS(f.Name))
	if f.External {
		_, _ = compat.Fprintfb(w, "%v", compat.ArgS(" external"))
	}
	if f.Bodyless {
		_, _ = compat.Fprintfb(w, "%v", compat.ArgS(" bodyless"))
	}
	_, _ = fmt.Fprintln(w)
	for i, in := range f.Inputs {
		name := "record"
		if in.Var != nil {
			name = in.Var.Name()
		}
		_, _ = compat.Fprintfb(w, "  input %d %s %s\n", compat.ArgI(int64(i)), compat.ArgS(name), compat.ArgS(typeStr(in.Type)))
	}
	for i, o := range f.Objects {
		_, _ = compat.Fprintfb(w, "  obj %d %s %s %q life=%d\n", compat.ArgI(int64(i)), compat.ArgS(objKindNames[o.Kind]), compat.ArgS(typeStr(o.Type)), compat.ArgS(o.Name), compat.ArgU(uint64(o.Life)))
	}
	for _, b := range f.Blocks {
		_, _ = fmt.Fprintf(w, "  b%d preds=%v succs=%v loop=%d\n", b.ID, b.Preds, b.Succs, b.Loop)
		for _, in := range b.Instrs {
			_, _ = compat.Fprintfb(w, "    %s", compat.ArgS(p.InstrString(&in)))
			if fset != nil && in.Pos.IsValid() {
				pos := fset.Position(in.Pos)
				_, _ = compat.Fprintfb(w, "  @%d:%d", compat.ArgI(int64(pos.Line)), compat.ArgI(int64(pos.Column)))
			}
			_, _ = fmt.Fprintln(w)
		}
	}
}

var objKindNames = map[ObjKind]string{
	ObjVar: "var", ObjParam: "param", ObjTemp: "temp", ObjBox: "box", ObjClosure: "closure",
	ObjMethodVal: "methodval", ObjSliceLit: "slicelit", ObjVarargs: "varargs", ObjStmtTemp: "stmttemp",
	ObjRegion: "region", ObjArenaWindow: "arenawindow",
}

var implNames = map[Implicit]string{
	ImplDrop: "drop", ImplIndex: "index", ImplSetIndex: "setindex", ImplIndexOk: "indexok",
	ImplOpAssign: "op=", ImplLen: "len", ImplAll: "all", ImplRangeIter: "rangeiter",
	ImplDefer: "defer", ImplTask: "task", ImplWrapper: "wrapper",
}

func typeStr(T types2.Type) string {
	if T == nil {
		return "-"
	}
	return types2.TypeString(T, nil)
}

func regs(rs []Reg) string {
	var s []string
	for _, r := range rs {
		s = compat.Append(s, compat.Sprintfb("r%d", compat.ArgU(uint64(r))))
	}
	return compat.Join(s, ", ")
}

// InstrString formats one instruction.
func (p *Program) InstrString(in *Instr) string {
	dst := ""
	if in.Dst != 0 {
		dst = compat.Sprintfb("r%d = ", compat.ArgU(uint64(in.Dst)))
	}
	switch in.Op {
	case OpAddr:
		return compat.Sprintf("%s&o%d%v", dst, in.Obj, in.Path)
	case OpHeap:
		return compat.Sprintfb("%sheap(%d)", compat.ArgS(dst), compat.ArgU(uint64(in.Heap)))
	case OpInput:
		return compat.Sprintfb("%sinput %d", compat.ArgS(dst), compat.ArgI(int64(in.Index)))
	case OpLoad:
		return compat.Sprintfb("%sload r%d", compat.ArgS(dst), compat.ArgU(uint64(in.X)))
	case OpStore:
		return compat.Sprintfb("store r%d <- r%d", compat.ArgU(uint64(in.X)), compat.ArgU(uint64(in.Y)))
	case OpField:
		return compat.Sprintf("%sfield r%d%v", dst, in.X, in.Path)
	case OpElem:
		return compat.Sprintfb("%selem r%d", compat.ArgS(dst), compat.ArgU(uint64(in.X)))
	case OpSelect:
		return compat.Sprintf("%sselect r%d%v", dst, in.X, in.Path)
	case OpCompose:
		return compat.Sprintf("%scompose %s (%s) %v", dst, typeStr(in.Type), regs(in.Args), in.Fields)
	case OpCopy:
		return compat.Sprintfb("%scopy r%d", compat.ArgS(dst), compat.ArgU(uint64(in.X)))
	case OpConv:
		return compat.Sprintfb("%sconv r%d : %s -> %s box=o%d", compat.ArgS(dst), compat.ArgU(uint64(in.X)), compat.ArgS(typeStr(in.Type)), compat.ArgS(typeStr(in.To)), compat.ArgI(int64(int32(in.Obj))))
	case OpClosure:
		return compat.Sprintfb("%sclosure %s rec=o%d (%s)", compat.ArgS(dst), compat.ArgS(p.funcName(in.Callee)), compat.ArgI(int64(int32(in.Obj))), compat.ArgS(regs(in.Args)))
	case OpMethodVal:
		return compat.Sprintfb("%smethodval r%d.%s %s rec=o%d", compat.ArgS(dst), compat.ArgU(uint64(in.X)), compat.ArgS(in.Method.Name()), compat.ArgS(p.funcName(in.Callee)), compat.ArgI(int64(int32(in.Obj))))
	case OpAssert:
		return compat.Sprintfb("%sassert r%d : %s", compat.ArgS(dst), compat.ArgU(uint64(in.X)), compat.ArgS(typeStr(in.Type)))
	case OpCall:
		return compat.Concat(dst, p.callString(in.Call))
	case OpSink:
		return compat.Sprintfb("sink %d r%d [%d]", compat.ArgU(uint64(in.Sink)), compat.ArgU(uint64(in.X)), compat.ArgI(int64(in.Index)))
	case OpPhi:
		return compat.Sprintfb("%sphi %s", compat.ArgS(dst), compat.ArgS(regs(in.Args)))
	case OpDrop:
		return compat.Sprintfb("enddrop o%d", compat.ArgU(uint64(in.Obj)))
	case OpBorrow:
		return compat.Sprintfb("%sborrow r%d r%d", compat.ArgS(dst), compat.ArgU(uint64(in.X)), compat.ArgU(uint64(in.Y)))
	case OpRecount:
		if in.Type != nil {
			return compat.Sprintf("%srecount r%d %s", dst, in.X, in.Type)
		}
		return compat.Sprintfb("%srecount r%d", compat.ArgS(dst), compat.ArgU(uint64(in.X)))
	case OpLend:
		return compat.Sprintfb("%slend r%d r%d", compat.ArgS(dst), compat.ArgU(uint64(in.X)), compat.ArgU(uint64(in.Y)))
	case OpArenaView:
		return compat.Sprintfb("%sarenaview r%d o%d", compat.ArgS(dst), compat.ArgU(uint64(in.X)), compat.ArgI(int64(int32(in.Obj))))
	}
	return compat.Sprintfb("op%d", compat.ArgU(uint64(in.Op)))
}

func (p *Program) funcName(id FuncID) string {
	if id == NoFunc || int(id) >= len(p.Funcs) {
		return "?"
	}
	return p.Funcs[id].Name
}

func (p *Program) callString(c *Call) string {
	var callee string
	switch c.Kind {
	case CallStatic:
		callee = p.funcName(c.Callee)
	case CallIface:
		m := "drop"
		if c.Method != nil {
			m = c.Method.Name()
		}
		callee = compat.Sprintfb("iface r%d.%s (%s)", compat.ArgU(uint64(c.Target)), compat.ArgS(m), compat.ArgS(typeStr(c.Iface)))
	case CallTypeParam:
		m := "drop"
		if c.Method != nil {
			m = c.Method.Name()
		}
		callee = compat.Sprintfb("tparam %s.%s", compat.ArgS(typeStr(c.Iface)), compat.ArgS(m))
		if c.RecvAddr != 0 {
			callee = compat.Concat(callee, compat.Sprintfb(" &r%d", compat.ArgU(uint64(c.RecvAddr))))
		}
	case CallFunc:
		callee = compat.Sprintfb("func r%d", compat.ArgU(uint64(c.Target)))
	case CallContract:
		callee = compat.Concat("contract ", c.Contract)
	}
	impl := ""
	if c.Implicit != ImplNone {
		impl = compat.Concat(" [", implNames[c.Implicit], "]")
	}
	return compat.Sprintfb("call %s(%s) -> (%s)%s", compat.ArgS(callee), compat.ArgS(regs(c.Args)), compat.ArgS(regs(c.Results)), compat.ArgS(impl))
}
