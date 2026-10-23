package main

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// Formatting calls of basic operands.
//
// pluto/fmt formats an operand by walking it through its type descriptor
// and calling the String, Error and other formatting methods of what it
// reaches through interfaces. The escape analysis takes that walk as a
// call of every such method in the program on everything the operand
// reaches; the translated compiler's String methods store (types2
// memoizes), so an operand in an interface that points into a frame looks
// kept, and a string or an integer operand too, once it is boxed. The
// translation keeps fmt away from the calls it can: a call of a fmt
// printing function whose operands are all strings, integers and booleans
// becomes a call of compat.Sprintfb (Fprintfb, Printfb) with each operand
// an Arg, which formats them without interfaces or methods
// (selfhost/compat/printfb.p):
//
//	fmt.Sprintf("%s: %d", name, n)  ->  compat.Sprintfb("%s: %d", compat.ArgS(name), compat.ArgI(int64(n)))
//
// An operand with a String or Error method that fmt would call counts as
// a string when the translation can call the method itself and print what
// fmt prints:
//
//	fmt.Sprintf("%v", obj)  ->  compat.Sprintfb("%v", compat.ArgS((obj).String()))
//
// Only where the output is sure to be the same:
//
//   - the format is a constant (Sprint, Sprintln and Println get one made
//     from their operands: %v each, with fmt's spaces and newline), with
//     one simple verb per operand: no explicit argument index, no * width
//     or precision, no # flag, %w left to fmt, and for each kind only the
//     verbs and flags compat's formatter implements (printfb.p);
//   - an operand's static type is not an interface or a type parameter,
//     has no Format method, and is a string, integer or boolean type, or
//     has a String or Error method fmt calls for its verb (%v %s %x %X %q),
//     Error first as fmt prefers it;
//   - the method's result cannot be a view of a copy the call makes: a
//     value receiver must be an integer or boolean type (a struct's String
//     may return one of its strings, as token.Position's does);
//   - for a pointer: fmt prints <nil> when the method panics on a nil
//     receiver, so a nil pointer becomes "<nil>" unless the method is
//     declared in this package with a pointer receiver that it compares
//     with nil (it handles nil itself) or does not use (it cannot panic on
//     it). A method declared elsewhere, of a pointer, is left to fmt.
//
// A call with any other operand is left to fmt as it is, its operands
// untouched: replacing some of them by strings would only give the walk
// more boxes.

// printfKind says how a formatting call takes its operands.
type printfKind uint8

const (
	printfFormat printfKind = iota + 1 // a format string, then the operands
	printfSpace                        // %v each, a space between two operands that are not strings (Sprint)
	printfLine                         // %v each, spaces between them and a newline after (Sprintln)
)

// printfFunc is one of fmt's printing functions.
type printfFunc struct {
	kind printfKind
	out  byte // 'S' a string (Sprintfb), 'E' an error of it, 'F' a write to the first argument (Fprintfb), 'P' to standard output (Printfb)
}

// fmtFuncs are fmt's printing functions go2pu routes, by name.
var fmtFuncs = map[string]printfFunc{
	"Sprintf": {printfFormat, 'S'}, "Errorf": {printfFormat, 'E'},
	"Fprintf": {printfFormat, 'F'}, "Printf": {printfFormat, 'P'},
	"Sprint": {printfSpace, 'S'}, "Fprint": {printfSpace, 'F'}, "Print": {printfSpace, 'P'},
	"Sprintln": {printfLine, 'S'}, "Fprintln": {printfLine, 'F'}, "Println": {printfLine, 'P'},
}

// stringerArg is the method a formatting operand is replaced by a call of.
type stringerArg struct {
	method string // "String" or "Error"
	nilStr bool   // a nil pointer formats as "<nil>"
}

// basicCall is a formatting call routed to compat's formatter.
type basicCall struct {
	pf printfFunc
}

// basicOp is an operand of a routed call.
type basicOp struct {
	kind   byte   // 'S' string, 'I' signed integer, 'U' unsigned, 'B' bool: compat.ArgS and so on
	conv   string // the conversion to the Arg constructor's type, or ""
	prefix string // text before it (the inserted format)
}

// planStringers finds the formatting calls in files to route to compat's
// formatter, and their operands to replace by a call of their String or
// Error method.
func (tc *translator) planStringers(files []*ast.File) {
	tc.stringerArgs = map[ast.Expr]stringerArg{}
	tc.stringering = map[ast.Expr]bool{}
	tc.basicCalls = map[ast.Node]*basicCall{}
	tc.basicOps = map[ast.Expr]basicOp{}
	tc.basicing = map[ast.Node]bool{}
	tc.basicingOp = map[ast.Expr]bool{}
	methods := map[*types.Func]*ast.FuncDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				if fn, ok := tc.info.Defs[fd.Name].(*types.Func); ok {
					methods[fn] = fd
				}
			}
		}
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				tc.planBasicCall(call, methods)
			}
			return true
		})
	}
}

// planBasicCall routes call if it is a fmt printing call of basic operands.
func (tc *translator) planBasicCall(call *ast.CallExpr, decls map[*types.Func]*ast.FuncDecl) {
	if call.Ellipsis.IsValid() {
		return
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return
	}
	fn, _ := tc.info.Uses[sel.Sel].(*types.Func)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "fmt" {
		return
	}
	pf, ok := fmtFuncs[fn.Name()]
	if !ok {
		return
	}
	first := 0
	if pf.out == 'F' {
		first = 1
	}
	var specs []verbSpec
	var ops []ast.Expr
	if pf.kind == printfFormat {
		if first >= len(call.Args) {
			return
		}
		tv := tc.info.Types[call.Args[first]]
		if tv.Value == nil || tv.Value.Kind() != constant.String {
			return
		}
		if specs, ok = parseVerbs(constant.StringVal(tv.Value)); !ok {
			return
		}
		ops = call.Args[first+1:]
	} else {
		ops = call.Args[min(first, len(call.Args)):]
		specs = make([]verbSpec, len(ops))
		for i := range specs {
			specs[i].verb = 'v'
		}
	}
	if len(ops) == 0 || len(specs) != len(ops) {
		return // nothing to route, or fmt's %!(EXTRA) and %!v(MISSING)
	}
	plan := make([]basicOp, len(ops))
	stringers := map[ast.Expr]stringerArg{}
	isStr := make([]bool, len(ops))
	for i, a := range ops {
		op, sa, str, ok := tc.basicOperand(a, specs[i], decls)
		if !ok {
			return
		}
		plan[i], isStr[i] = op, str
		if sa != nil {
			stringers[a] = *sa
		}
	}
	bc := &basicCall{pf: pf}
	if pf.kind != printfFormat {
		var b strings.Builder
		for i := range ops {
			// fmt's Sprint puts a space between two operands neither
			// of which is a string (by the operand's own type, not the
			// method's result); Sprintln between any two.
			if i > 0 && (pf.kind == printfLine || !isStr[i-1] && !isStr[i]) {
				b.WriteByte(' ')
			}
			b.WriteString("%v")
		}
		if pf.kind == printfLine {
			b.WriteByte('\n')
		}
		plan[0].prefix = strconv.Quote(b.String()) + ", "
	}
	tc.basicCalls[call] = bc
	tc.basicCalls[call.Fun] = bc
	for i, a := range ops {
		tc.basicOps[a] = plan[i]
	}
	for a, sa := range stringers {
		tc.stringerArgs[a] = sa
	}
}

// verbSpec is one verb of a format, with its flags.
type verbSpec struct {
	minus, zero, plus, space bool
	wid, prec                bool // present
	verb                     byte
}

// parseVerbs returns the verbs of a format that takes one operand per verb,
// or false when it uses what compat's formatter does not follow: an
// explicit argument index, a * width or precision, the # flag, a verb that
// is not ASCII, or a % at the end.
func parseVerbs(format string) ([]verbSpec, bool) {
	var specs []verbSpec
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		var s verbSpec
	flags:
		for ; i < len(format); i++ {
			switch format[i] {
			case '-':
				s.minus, s.zero = true, false
			case '0':
				s.zero = !s.minus
			case '+':
				s.plus = true
			case ' ':
				s.space = true
			case '#':
				return nil, false
			default:
				break flags
			}
		}
		for ; i < len(format) && format[i] >= '0' && format[i] <= '9'; i++ {
			s.wid = true
		}
		if i < len(format) && format[i] == '.' {
			s.prec = true
			for i++; i < len(format) && format[i] >= '0' && format[i] <= '9'; i++ {
			}
		}
		if i >= len(format) {
			return nil, false
		}
		switch c := format[i]; {
		case c == '%':
			continue
		case c == '*' || c == '[' || c >= 0x80:
			return nil, false
		default:
			s.verb = c
			specs = append(specs, s)
		}
	}
	return specs, true
}

// basicOperand returns how operand a, formatted by spec, is passed to
// compat's formatter, the method to replace it by a call of, and whether
// fmt takes it as a string (for Sprint's spaces); or false when it must be
// left to fmt.
func (tc *translator) basicOperand(a ast.Expr, spec verbSpec, decls map[*types.Func]*ast.FuncDecl) (basicOp, *stringerArg, bool, bool) {
	tv, ok := tc.info.Types[a]
	if !ok || tv.Type == nil || tv.IsNil() {
		return basicOp{}, nil, false, false
	}
	T := tv.Type
	if types.IsInterface(T) {
		return basicOp{}, nil, false, false
	}
	if _, isTP := types.Unalias(T).(*types.TypeParam); isTP {
		return basicOp{}, nil, false, false
	}
	b, _ := T.Underlying().(*types.Basic)
	isStr := b != nil && b.Info()&types.IsString != 0
	ms := types.NewMethodSet(T)
	if lookupMethod(ms, "Format") != nil {
		return basicOp{}, nil, false, false // a fmt.Formatter formats itself
	}
	switch spec.verb {
	case 'v', 's', 'x', 'X', 'q':
		// fmt calls the Error or String method for these verbs.
		if lookupMethod(ms, "Error") != nil || lookupMethod(ms, "String") != nil {
			sa, ok := tc.stringerOf(T, ms, decls)
			if !ok || !verbOK('S', spec) {
				return basicOp{}, nil, false, false
			}
			return basicOp{kind: 'S'}, &sa, isStr, true
		}
	}
	if b == nil {
		return basicOp{}, nil, false, false
	}
	var kind byte
	var target types.Type
	switch info := b.Info(); {
	case info&types.IsString != 0:
		kind, target = 'S', types.Typ[types.String]
	case info&types.IsInteger != 0 && info&types.IsUnsigned != 0:
		kind, target = 'U', types.Typ[types.Uint64]
	case info&types.IsInteger != 0:
		kind, target = 'I', types.Typ[types.Int64]
	case info&types.IsBoolean != 0:
		kind, target = 'B', types.Typ[types.Bool]
	default:
		return basicOp{}, nil, false, false
	}
	if !verbOK(kind, spec) {
		return basicOp{}, nil, false, false
	}
	op := basicOp{kind: kind}
	if info := b.Info(); info&types.IsUntyped == 0 && !types.Identical(T, target) {
		op.conv = target.String()
	}
	return op, nil, isStr, true
}

// verbOK reports whether compat's formatter prints an operand of kind with
// spec as fmt does (printfb.p).
func verbOK(kind byte, s verbSpec) bool {
	switch kind {
	case 'S':
		switch s.verb {
		case 'v', 's':
			return !s.zero && !s.plus && !s.space
		case 'q':
			return !s.zero && !s.plus && !s.space && !s.prec
		case 'x', 'X':
			return !s.minus && !s.zero && !s.plus && !s.space && !s.wid && !s.prec
		}
	case 'I', 'U':
		switch s.verb {
		case 'd':
			return !s.prec
		case 'v':
			// %+v is not %+d: fmt takes the + of %v for struct field names.
			return !s.prec && !s.plus && !s.space
		case 'x', 'X':
			return !s.prec && !s.plus && !s.space
		case 'c', 'q':
			return !s.prec && !s.plus && !s.space && !s.zero
		}
	case 'B':
		switch s.verb {
		case 'v', 't':
			return !s.prec && !s.plus && !s.space && !s.zero
		}
	}
	return false
}

// stringerOf returns the Error or String method fmt calls on an operand of
// type T (method set ms), when the translation can call it and print the
// same.
func (tc *translator) stringerOf(T types.Type, ms *types.MethodSet, decls map[*types.Func]*ast.FuncDecl) (stringerArg, bool) {
	var m *types.Func
	method := ""
	for _, name := range []string{"Error", "String"} {
		sel := lookupMethod(ms, name)
		if sel == nil {
			continue
		}
		fn := sel.Obj().(*types.Func)
		sig := fn.Type().(*types.Signature)
		if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
			continue
		}
		if b, ok := sig.Results().At(0).Type().(*types.Basic); !ok || b.Kind() != types.String {
			continue // not fmt.Stringer's or error's method
		}
		m, method = fn, name
		break
	}
	if m == nil {
		return stringerArg{}, false
	}
	recv := m.Type().(*types.Signature).Recv().Type()
	if _, isPtr := recv.(*types.Pointer); !isPtr {
		// A value receiver is a copy, which the result may be a view of
		// (a string field of a struct): only an integer or a bool, which
		// holds no memory to view.
		b, ok := recv.Underlying().(*types.Basic)
		if !ok || b.Info()&(types.IsInteger|types.IsBoolean) == 0 {
			return stringerArg{}, false
		}
	}
	arg := stringerArg{method: method}
	if _, isPtr := T.Underlying().(*types.Pointer); isPtr {
		switch tc.nilReceiver(m, decls) {
		case nilPanics:
			arg.nilStr = true
		case nilUnknown:
			return stringerArg{}, false
		}
	}
	return arg, true
}

// lookupMethod returns the exported method name of ms, or nil.
func lookupMethod(ms *types.MethodSet, name string) *types.Selection {
	for i := 0; i < ms.Len(); i++ {
		if s := ms.At(i); s.Obj().Name() == name {
			return s
		}
	}
	return nil
}

// basicText returns the translation of n as part of a routed formatting
// call: an operand (which may itself be a routed call), the call of an
// error, or the callee.
func (t *fileTr) basicText(n ast.Node) (string, bool) {
	if e, ok := n.(ast.Expr); ok && !t.basicingOp[e] {
		if op, ok := t.basicOps[e]; ok {
			t.basicingOp[e] = true
			defer delete(t.basicingOp, e)
			s := t.renderUnboxed(e)
			if op.conv != "" {
				s = op.conv + "(" + s + ")"
			}
			return fmt.Sprintf("%s%s.Arg%c(%s)", op.prefix, t.compat(), op.kind, s), true
		}
	}
	bc, ok := t.basicCalls[n]
	if !ok || t.basicing[n] {
		return "", false
	}
	if call, ok := n.(*ast.CallExpr); ok {
		if bc.pf.out != 'E' {
			return "", false
		}
		// fmt.Errorf without %w is an error of the text.
		t.basicing[n] = true
		defer delete(t.basicing, n)
		return fmt.Sprintf("%s.NewError(%s)", t.compat(), t.splice(call)), true
	}
	name := map[byte]string{'S': "Sprintfb", 'E': "Sprintfb", 'F': "Fprintfb", 'P': "Printfb"}[bc.pf.out]
	return t.compat() + "." + name, true
}

// renderUnboxed renders a, which is no longer converted to an interface:
// no box for it (boxing.go, arenabox.go).
func (t *fileTr) renderUnboxed(a ast.Expr) string {
	if t.arenaBoxing == nil {
		t.arenaBoxing = map[ast.Expr]bool{}
	}
	if t.boxing == nil {
		t.boxing = map[ast.Expr]bool{}
	}
	wasA, wasB := t.arenaBoxing[a], t.boxing[a]
	t.arenaBoxing[a], t.boxing[a] = true, true
	s := t.render(a)
	t.arenaBoxing[a], t.boxing[a] = wasA, wasB
	return s
}

type nilBehavior uint8

const (
	nilUnknown nilBehavior = iota // not known: leave the operand to fmt
	nilPanics                     // the method dereferences a nil receiver: fmt prints <nil>
	nilHandled                    // the method takes a nil receiver itself
)

// nilReceiver says what method m does with a nil receiver, called through a
// pointer: a value receiver is dereferenced (a panic, which fmt prints as
// <nil>); a pointer receiver that the body compares with nil, or never
// uses, is handled; any other use of it is taken to panic on nil. A method
// declared outside the package is unknown.
func (tc *translator) nilReceiver(m *types.Func, decls map[*types.Func]*ast.FuncDecl) nilBehavior {
	recv := m.Type().(*types.Signature).Recv()
	if recv == nil {
		return nilUnknown
	}
	if _, isPtr := recv.Type().(*types.Pointer); !isPtr {
		return nilPanics
	}
	fd := decls[m.Origin()]
	if fd == nil || fd.Body == nil {
		return nilUnknown
	}
	used, compared := false, false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if tc.info.Uses[n] == recv {
				used = true
			}
		case *ast.BinaryExpr:
			if n.Op == token.EQL || n.Op == token.NEQ {
				for _, pair := range [][2]ast.Expr{{n.X, n.Y}, {n.Y, n.X}} {
					if id, ok := ast.Unparen(pair[0]).(*ast.Ident); ok && tc.info.Uses[id] == recv && tc.info.Types[pair[1]].IsNil() {
						compared = true
					}
				}
			}
		}
		return true
	})
	switch {
	case compared || !used:
		return nilHandled
	}
	return nilPanics
}

// stringerText returns the translation of operand a replaced by the call of
// its method, or false.
func (t *fileTr) stringerText(a ast.Expr) (string, bool) {
	arg, ok := t.stringerArgs[a]
	if !ok || t.stringering[a] {
		return "", false
	}
	// The operand is no longer converted to an interface: no box for it
	// (boxing.go, arenabox.go).
	t.stringering[a] = true
	if t.arenaBoxing == nil {
		t.arenaBoxing = map[ast.Expr]bool{}
	}
	if t.boxing == nil {
		t.boxing = map[ast.Expr]bool{}
	}
	wasA, wasB := t.arenaBoxing[a], t.boxing[a]
	t.arenaBoxing[a], t.boxing[a] = true, true
	inner := t.render(a)
	t.arenaBoxing[a], t.boxing[a] = wasA, wasB
	delete(t.stringering, a)
	if !arg.nilStr {
		return fmt.Sprintf("(%s).%s()", inner, arg.method), true
	}
	// A nil pointer formats as <nil>, as fmt prints a panic of the method
	// on a nil receiver: compat.StringOf or ErrorOf (a generic function, not
	// a closure, so that no record is made in the frame per call).
	return fmt.Sprintf("%s.%sOf(%s)", t.compat(), arg.method, inner), true
}
