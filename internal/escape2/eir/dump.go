package eir

import (
	"fmt"
	"io"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Dump writes a function in a readable form, for tests and debugging.
func (p *Program) Dump(w io.Writer, fset *token.FileSet, f *Func) {
	_, _ = fmt.Fprintf(w, "func %s", f.Name)
	if f.External {
		_, _ = fmt.Fprint(w, " external")
	}
	if f.Bodyless {
		_, _ = fmt.Fprint(w, " bodyless")
	}
	_, _ = fmt.Fprintln(w)
	for i, in := range f.Inputs {
		name := "record"
		if in.Var != nil {
			name = in.Var.Name()
		}
		_, _ = fmt.Fprintf(w, "  input %d %s %s\n", i, name, typeStr(in.Type))
	}
	for i, o := range f.Objects {
		_, _ = fmt.Fprintf(w, "  obj %d %s %s %q life=%d\n", i, objKindNames[o.Kind], typeStr(o.Type), o.Name, o.Life)
	}
	for _, b := range f.Blocks {
		_, _ = fmt.Fprintf(w, "  b%d preds=%v succs=%v loop=%d\n", b.ID, b.Preds, b.Succs, b.Loop)
		for _, in := range b.Instrs {
			_, _ = fmt.Fprintf(w, "    %s", p.InstrString(&in))
			if fset != nil && in.Pos.IsValid() {
				pos := fset.Position(in.Pos)
				_, _ = fmt.Fprintf(w, "  @%d:%d", pos.Line, pos.Column)
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
		s = append(s, fmt.Sprintf("r%d", r))
	}
	return strings.Join(s, ", ")
}

// InstrString formats one instruction.
func (p *Program) InstrString(in *Instr) string {
	dst := ""
	if in.Dst != 0 {
		dst = fmt.Sprintf("r%d = ", in.Dst)
	}
	switch in.Op {
	case OpAddr:
		return fmt.Sprintf("%s&o%d%v", dst, in.Obj, in.Path)
	case OpHeap:
		return fmt.Sprintf("%sheap(%d)", dst, in.Heap)
	case OpInput:
		return fmt.Sprintf("%sinput %d", dst, in.Index)
	case OpLoad:
		return fmt.Sprintf("%sload r%d", dst, in.X)
	case OpStore:
		return fmt.Sprintf("store r%d <- r%d", in.X, in.Y)
	case OpField:
		return fmt.Sprintf("%sfield r%d%v", dst, in.X, in.Path)
	case OpElem:
		return fmt.Sprintf("%selem r%d", dst, in.X)
	case OpSelect:
		return fmt.Sprintf("%sselect r%d%v", dst, in.X, in.Path)
	case OpCompose:
		return fmt.Sprintf("%scompose %s (%s) %v", dst, typeStr(in.Type), regs(in.Args), in.Fields)
	case OpCopy:
		return fmt.Sprintf("%scopy r%d", dst, in.X)
	case OpConv:
		return fmt.Sprintf("%sconv r%d : %s -> %s box=o%d", dst, in.X, typeStr(in.Type), typeStr(in.To), int32(in.Obj))
	case OpClosure:
		return fmt.Sprintf("%sclosure %s rec=o%d (%s)", dst, p.funcName(in.Callee), int32(in.Obj), regs(in.Args))
	case OpMethodVal:
		return fmt.Sprintf("%smethodval r%d.%s %s rec=o%d", dst, in.X, in.Method.Name(), p.funcName(in.Callee), int32(in.Obj))
	case OpAssert:
		return fmt.Sprintf("%sassert r%d : %s", dst, in.X, typeStr(in.Type))
	case OpCall:
		return dst + p.callString(in.Call)
	case OpSink:
		return fmt.Sprintf("sink %d r%d [%d]", in.Sink, in.X, in.Index)
	case OpPhi:
		return fmt.Sprintf("%sphi %s", dst, regs(in.Args))
	case OpDrop:
		return fmt.Sprintf("enddrop o%d", in.Obj)
	}
	return fmt.Sprintf("op%d", in.Op)
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
		callee = fmt.Sprintf("iface r%d.%s (%s)", c.Target, m, typeStr(c.Iface))
	case CallTypeParam:
		m := "drop"
		if c.Method != nil {
			m = c.Method.Name()
		}
		callee = fmt.Sprintf("tparam %s.%s", typeStr(c.Iface), m)
		if c.RecvAddr != 0 {
			callee += fmt.Sprintf(" &r%d", c.RecvAddr)
		}
	case CallFunc:
		callee = fmt.Sprintf("func r%d", c.Target)
	case CallContract:
		callee = "contract " + c.Contract
	}
	impl := ""
	if c.Implicit != ImplNone {
		impl = " [" + implNames[c.Implicit] + "]"
	}
	return fmt.Sprintf("call %s(%s) -> (%s)%s", callee, regs(c.Args), regs(c.Results), impl)
}
