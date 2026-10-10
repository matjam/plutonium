package diag

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
	typerr "github.com/matjam/plutonium/internal/types2/errors"
)

// A CheckContext is what the type checker recorded about a package, used
// to explain its errors with the names, types and lines involved.
type CheckContext struct {
	Fset  *token.FileSet
	Files []*ast.File
	Pkg   *types2.Package // may be nil
	Info  *types2.Info    // may be nil
}

// FromChecker returns the diagnostic for an error the type checker
// reported (a types2.Error). With ctx, the most common errors get an
// explanation specialised to the code at fault; the rest use the catalog.
func FromChecker(err error, ctx *CheckContext) *Diagnostic {
	te, ok := err.(types2.Error)
	if !ok {
		return &Diagnostic{Code: InternalError, Msg: err.Error()}
	}
	code := te.Code()
	d := &Diagnostic{Pos: te.Fset.Position(te.Pos), Msg: te.Msg, Code: CheckerCode(code)}
	if code == typerr.InvalidSyntaxTree {
		d.Code = InternalError
	}
	if ex := te.Explain; ex != nil {
		// The checker explained the error itself, with the program's names.
		d.Text = ex.Text
		for i, f := range ex.Fixes {
			fix := &Fix{Intro: f.Intro, Code: f.Code}
			if i == 0 {
				d.Fix = fix
			} else {
				d.More = append(d.More, fix)
			}
		}
		return d
	}
	if f := specialisers[code]; f != nil && ctx != nil && ctx.Info != nil {
		s := &spec{CheckContext: ctx, pos: te.Pos, d: d, first: strings.SplitN(te.Msg, "\n", 2)[0]}
		s.path = s.enclosing(te.Pos)
		s.run(f)
	}
	return d
}

// A spec is the state of explaining one type error.
type spec struct {
	*CheckContext
	pos   token.Pos
	d     *Diagnostic
	first string     // the first line of the message
	path  []ast.Node // the nodes enclosing pos, innermost first
}

// run calls f, keeping the catalog's explanation if f fails: an
// explanation is not worth a crash.
func (s *spec) run(f func(*spec)) {
	text, fix := s.d.Text, s.d.Fix
	defer func() {
		if recover() != nil {
			s.d.Text, s.d.Fix = text, fix
		}
	}()
	f(s)
}

var specialisers map[typerr.Code]func(*spec)

func init() {
	specialisers = map[typerr.Code]func(*spec){
		typerr.IncompatibleAssign:     explainAssign,
		typerr.InvalidIfaceAssign:     explainAssign,
		typerr.NumericOverflow:        explainOverflow,
		typerr.UndeclaredName:         explainUndeclared,
		typerr.UndeclaredImportedName: explainImportedName,
		typerr.MissingFieldOrMethod:   explainFieldOrMethod,
		typerr.WrongArgCount:          explainArgCount,
		typerr.WrongResultCount:       explainResultCount,
		typerr.UnusedVar:              explainUnusedVar,
		typerr.UnusedImport:           explainUnusedImport,
		typerr.MissingReturn:          explainMissingReturn,
		typerr.MismatchedTypes:        explainMismatched,
		typerr.UndefinedOp:            explainUndefinedOp,
		typerr.CannotInferTypeArgs:    explainInfer,
		typerr.AppendNotPermitted:     explainAppend,
		typerr.MakeSliceNotPermitted:  explainMakeSlice,
	}
}

// Specialised is the number of type checker codes whose errors get an
// explanation of their own.
func Specialised() []Code {
	var codes []Code
	for c := range specialisers {
		codes = append(codes, CheckerCode(c))
	}
	slices.Sort(codes)
	return codes
}

// ----------------------------------------------------------------------------
// Helpers

// enclosing returns the nodes of the file holding pos that contain it,
// innermost first.
func (s *spec) enclosing(pos token.Pos) []ast.Node {
	var path []ast.Node
	for _, f := range s.Files {
		if f.Pos() > pos || pos > f.End() {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil || n.Pos() > pos || pos >= n.End() {
				return false
			}
			path = append(path, n)
			return true
		})
	}
	slices.Reverse(path)
	return path
}

// find returns the innermost enclosing node of type T.
func find[T ast.Node](s *spec) (T, bool) {
	for _, n := range s.path {
		if t, ok := n.(T); ok {
			return t, true
		}
	}
	var zero T
	return zero, false
}

// exprAt returns the outermost expression that starts at the error's
// position: the operand the error is about.
func (s *spec) exprAt() ast.Expr {
	var found ast.Expr
	for _, n := range s.path {
		if e, ok := n.(ast.Expr); ok && e.Pos() == s.pos {
			found = e
		}
	}
	return found
}

// where says where pos is: "line 4", or "line 4 of util.p" in another file.
func (s *spec) where(pos token.Pos) string {
	p := s.Fset.Position(pos)
	if p.Filename != s.d.Pos.Filename {
		return fmt.Sprintf("line %d of %s", p.Line, filepath.Base(p.Filename))
	}
	return fmt.Sprintf("line %d", p.Line)
}

func (s *spec) typeOf(e ast.Expr) types2.Type {
	if tv, ok := s.Info.Types[e]; ok {
		return tv.Type
	}
	if id, ok := e.(*ast.Ident); ok {
		if obj := s.objOf(id); obj != nil {
			return obj.Type()
		}
	}
	return nil
}

func (s *spec) objOf(id *ast.Ident) types2.Object {
	if obj := s.Info.Uses[id]; obj != nil {
		return obj
	}
	return s.Info.Defs[id]
}

func (s *spec) str(T types2.Type) string {
	return types2.TypeString(T, types2.RelativeTo(s.Pkg))
}

// describe says in words what values of T are.
func describe(T types2.Type) string {
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		switch info := u.Info(); {
		case info&types2.IsString != 0:
			return "text"
		case info&types2.IsInteger != 0:
			return "whole numbers"
		case info&types2.IsFloat != 0:
			return "numbers with a fraction"
		case info&types2.IsBoolean != 0:
			return "true or false"
		}
	case *types2.Pointer:
		return "pointers"
	case *types2.Slice:
		return "slices"
	}
	return ""
}

func isKind(T types2.Type, flag types2.BasicInfo) bool {
	if T == nil {
		return false
	}
	b, ok := T.Underlying().(*types2.Basic)
	return ok && b.Info()&flag != 0
}

func isUntyped(T types2.Type) bool { return isKind(T, types2.IsUntyped) }

// closest returns the candidate most like name, if one is close enough to
// be a likely misspelling.
func closest(name string, candidates []string) string {
	best, bestDist := "", 0
	limit := 1
	if len(name) > 4 {
		limit = 2
	}
	for _, c := range candidates {
		if c == name || c == "_" {
			continue
		}
		d := editDistance(strings.ToLower(name), strings.ToLower(c))
		if d < len(c) && d <= limit && (best == "" || d < bestDist) {
			best, bestDist = c, d
		}
	}
	return best
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// zeroValue returns an expression for the zero value of T.
func (s *spec) zeroValue(T types2.Type) string {
	switch u := T.Underlying().(type) {
	case *types2.Basic:
		switch {
		case u.Info()&types2.IsString != 0:
			return `""`
		case u.Info()&types2.IsBoolean != 0:
			return "false"
		case u.Info()&types2.IsNumeric != 0:
			return "0"
		}
	case *types2.Struct, *types2.Array:
		return s.str(T) + "{}"
	}
	return "nil"
}

// signature formats a function's parameters and results, without "func".
func (s *spec) signature(sig *types2.Signature) string {
	return strings.TrimPrefix(s.str(sig), "func")
}

// plural returns "1 thing" or "2 things".
func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// ----------------------------------------------------------------------------
// Assignability

var (
	cannotUse      = regexp.MustCompile(`^cannot use (.+) as (.+?) value in (.+?)(?:: (.+))?$`)
	consumesRecv   = regexp.MustCompile(`^(.+) does not implement (.+?) \(method (\w+) has a value receiver of owner type`)
	doesNotImpl    = regexp.MustCompile(`^(.+) does not implement (.+?) \((missing method|method) (\w+)(\)| has pointer receiver\))`)
	overflowSuffix = regexp.MustCompile(` \((overflows|truncated)\)$`)
)

// explainAssign explains "cannot use x (variable of type V) as T value in
// context": what the target holds, what the value is, and how to convert.
func explainAssign(s *spec) {
	m := cannotUse.FindStringSubmatch(s.first)
	if m == nil {
		return
	}
	context, cause := m[3], m[4]
	rhs := s.exprAt()
	if rhs == nil {
		return
	}
	V := s.typeOf(rhs)
	src := types2.ExprString(rhs)
	target, T := s.assignTarget(rhs, context)
	if T == nil || V == nil {
		return
	}
	var text []string
	if cause != "" {
		if mm := consumesRecv.FindStringSubmatch(cause); mm != nil {
			s.explainConsumes(src, V, T, mm[3])
			if target != "" && s.d.Text != "" {
				s.d.Text = fmt.Sprintf("%s, so it can only hold values whose type has every method of %s.\n%s", target, s.str(T), s.d.Text)
			}
			return
		}
		if mm := doesNotImpl.FindStringSubmatch(cause); mm != nil {
			s.explainImplements(src, V, T, mm[4], mm[5] != ")")
			if target != "" && s.d.Text != "" {
				s.d.Text = fmt.Sprintf("%s, so it can only hold values whose type has every method of %s.\n%s", target, s.str(T), s.d.Text)
			}
			return
		}
	}
	dt, dv := describe(T), describe(V)
	what := "a " + s.str(V)
	if isUntyped(V) {
		what = "an " + s.str(V) + " constant"
	}
	switch {
	case dt != "" && dv != "" && dt != dv:
		text = append(text, fmt.Sprintf("%s, so it can only hold %s; %s holds %s.", target, dt, src, dv))
	default:
		text = append(text, fmt.Sprintf("%s, so it can only hold values of that type; %s is %s.", target, src, what))
	}
	textual := isKind(T, types2.IsString) != isKind(V, types2.IsString)
	numeric := isKind(T, types2.IsNumeric) && isKind(V, types2.IsNumeric)
	switch {
	case textual && (isKind(T, types2.IsNumeric) || isKind(V, types2.IsNumeric)):
		text = append(text, "Plutonium, like Go, never converts between text and numbers automatically.")
		if isKind(T, types2.IsInteger) && !isUntyped(V) {
			if s.str(T) == "int" {
				s.d.Fix = &Fix{Intro: "To parse a number from text", Code: fmt.Sprintf("n, err := strconv.Atoi(%s)", src)}
			} else {
				s.d.Fix = &Fix{Intro: "To parse a number from text", Code: fmt.Sprintf("n, err := strconv.ParseInt(%s, 10, 64)", src)}
			}
		}
		if isKind(T, types2.IsString) {
			text = append(text, "To print a number, pass it to fmt.Printf with %d. To make its text, use strconv.Itoa or fmt.Sprintf, or append it to a vec.Vec or vec.Fixed with strconv.AppendInt or fmt.Appendf.")
		}
	case numeric && !isUntyped(V):
		text = append(text, "Plutonium, like Go, never converts between number types automatically, even when the value would fit. Write the conversion, so that the change of type is visible.")
		conv := fmt.Sprintf("%s(%s)", s.str(T), src)
		if as, ok := find[*ast.AssignStmt](s); ok && len(as.Lhs) == 1 && context == "assignment" {
			conv = types2.ExprString(as.Lhs[0]) + " " + as.Tok.String() + " " + conv
		}
		s.d.Fix = &Fix{Intro: "To convert explicitly", Code: conv}
	case isPointerTo(V, T):
		text = append(text, fmt.Sprintf("%s is a pointer to a %s, not a %s itself.", src, s.str(T), s.str(T)))
		s.d.Fix = &Fix{Intro: "To use the value it points to", Code: "*" + src}
	case isPointerTo(T, V):
		text = append(text, fmt.Sprintf("A pointer to a %s is needed, and %s is the %s itself.", s.str(V), src, s.str(V)))
		if _, ok := rhs.(*ast.Ident); ok {
			s.d.Fix = &Fix{Intro: "To pass its address", Code: "&" + src}
		}
	default:
		text = append(text, "Plutonium, like Go, does not convert between different types automatically.")
	}
	s.d.Text = strings.Join(text, " ")
}

// isPointerTo reports whether P is a pointer to T.
func isPointerTo(P, T types2.Type) bool {
	p, ok := P.Underlying().(*types2.Pointer)
	return ok && types2.Identical(p.Elem(), T)
}

// assignTarget says what the value is assigned to, in words, and returns
// its type.
func (s *spec) assignTarget(rhs ast.Expr, context string) (string, types2.Type) {
	switch {
	case context == "assignment":
		as, ok := find[*ast.AssignStmt](s)
		if !ok {
			return "", nil
		}
		for i, r := range as.Rhs {
			if r != rhs || i >= len(as.Lhs) {
				continue
			}
			lhs := as.Lhs[i]
			T := s.typeOf(lhs)
			if id, ok := lhs.(*ast.Ident); ok {
				if obj := s.objOf(id); obj != nil && obj.Pos().IsValid() {
					return fmt.Sprintf("%s is declared as %s on %s", id.Name, s.str(obj.Type()), s.where(obj.Pos())), obj.Type()
				}
			}
			if T != nil {
				return fmt.Sprintf("%s has type %s", types2.ExprString(lhs), s.str(T)), T
			}
		}
	case context == "variable declaration":
		vs, ok := find[*ast.ValueSpec](s)
		if !ok {
			return "", nil
		}
		for i, v := range vs.Values {
			if v == rhs && i < len(vs.Names) {
				if obj := s.Info.Defs[vs.Names[i]]; obj != nil {
					return fmt.Sprintf("%s is declared as %s", vs.Names[i].Name, s.str(obj.Type())), obj.Type()
				}
			}
		}
	case strings.HasPrefix(context, "argument to "):
		call, ok := find[*ast.CallExpr](s)
		if !ok {
			return "", nil
		}
		sig, _ := s.typeOf(call.Fun).(*types2.Signature)
		if sig == nil {
			return "", nil
		}
		for i, a := range call.Args {
			if a != rhs {
				continue
			}
			params := sig.Params()
			if params.Len() == 0 {
				return "", nil
			}
			j := min(i, params.Len()-1)
			p := params.At(j)
			T := p.Type()
			if sig.Variadic() && j == params.Len()-1 && call.Ellipsis == token.NoPos {
				T = T.(*types2.Slice).Elem()
			}
			name := p.Name()
			if name == "" {
				name = fmt.Sprintf("#%d", j+1)
			}
			callee := types2.ExprString(call.Fun)
			if fn := s.callee(call); fn != nil && fn.Pos().IsValid() {
				callee += " (declared on " + s.where(fn.Pos()) + ")"
			}
			return fmt.Sprintf("Parameter %s of %s has type %s", name, callee, s.str(T)), T
		}
	case context == "return statement":
		ret, ok := find[*ast.ReturnStmt](s)
		if !ok {
			return "", nil
		}
		name, sig, decl := s.enclosingFunc()
		if sig == nil {
			return "", nil
		}
		for i, r := range ret.Results {
			if r == rhs && i < sig.Results().Len() {
				T := sig.Results().At(i).Type()
				return fmt.Sprintf("%s is declared on %s to return %s", name, s.where(decl), s.str(T)), T
			}
		}
	}
	return "", nil
}

// callee returns the function a call names, if it names one.
func (s *spec) callee(call *ast.CallExpr) types2.Object {
	fun := call.Fun
	if ix, ok := fun.(*ast.IndexExpr); ok {
		fun = ix.X
	}
	if ix, ok := fun.(*ast.IndexListExpr); ok {
		fun = ix.X
	}
	switch f := fun.(type) {
	case *ast.Ident:
		return s.objOf(f)
	case *ast.SelectorExpr:
		return s.objOf(f.Sel)
	}
	return nil
}

// enclosingFunc returns the name, signature and position of the function
// around the error.
func (s *spec) enclosingFunc() (string, *types2.Signature, token.Pos) {
	for _, n := range s.path {
		switch f := n.(type) {
		case *ast.FuncLit:
			sig, _ := s.typeOf(f).(*types2.Signature)
			return "The function literal", sig, f.Pos()
		case *ast.FuncDecl:
			if obj := s.Info.Defs[f.Name]; obj != nil {
				sig, _ := obj.Type().(*types2.Signature)
				return f.Name.Name, sig, f.Pos()
			}
		}
	}
	return "", nil, token.NoPos
}

// explainImplements explains why a value of type V does not satisfy the
// interface T: its method is missing or has a pointer receiver.
func (s *spec) explainImplements(src string, V, T types2.Type, method string, pointer bool) {
	iface, _ := T.Underlying().(*types2.Interface)
	if iface == nil {
		return
	}
	var m *types2.Func
	for i := range iface.NumMethods() {
		if iface.Method(i).Name() == method {
			m = iface.Method(i)
		}
	}
	if m == nil {
		return
	}
	sig := m.Type().(*types2.Signature)
	where := ""
	if m.Pos().IsValid() {
		where = " (declared on " + s.where(m.Pos()) + ")"
	}
	recv := s.str(V)
	if pointer {
		s.d.Text = fmt.Sprintf("%s needs a method %s%s%s. %s has it, but the method is declared on *%s, so only a pointer to a %s has it, not a %s value. A type satisfies an interface only if its own method set has every method.", s.str(T), method, s.signature(sig), where, recv, recv, recv, recv)
		s.d.Fix = &Fix{Intro: "Use a pointer", Code: "&" + src}
		return
	}
	s.d.Text = fmt.Sprintf("%s needs a method %s%s%s, and %s has no method of that name. A type satisfies an interface by having all its methods; nothing else needs to be declared.", s.str(T), method, s.signature(sig), where, recv)
	if named, ok := types2.Unalias(V).(*types2.Named); ok && named.Obj().Pkg() == s.Pkg {
		r := strings.ToLower(named.Obj().Name()[:1])
		s.d.Fix = &Fix{Intro: "Add the method to " + recv, Code: fmt.Sprintf("func (%s %s) %s%s {\n\t...\n}", r, recv, method, s.signature(sig))}
	}
}

// explainConsumes explains why an owner V does not satisfy the interface T:
// its method has a value receiver, which consumes the owner (language spec
// section 6.4).
func (s *spec) explainConsumes(src string, V, T types2.Type, method string) {
	recv := s.str(V)
	r := strings.ToLower(recv[:1])
	s.d.Text = fmt.Sprintf("%s needs a method %s. %s has one, but with a value receiver: %s is an owner, so calling the method moves the value into it, and the method drops it when it returns. An interface does not own what it holds, so a call through the interface could not give the value up; such a method never satisfies an interface method.", s.str(T), method, recv, recv)
	s.d.Fix = &Fix{Intro: "Declare the method on a pointer, so that it borrows the value, and convert a pointer", Code: fmt.Sprintf("func (%s *%s) %s(...) ...\n\n... = &%s", r, recv, method, src)}
}

// intRanges gives the range of each integer type.
var intRanges = map[string][2]string{
	"int8": {"-128", "127"}, "int16": {"-32768", "32767"},
	"int32": {"-2147483648", "2147483647"}, "int64": {"-9223372036854775808", "9223372036854775807"},
	"int":   {"-9223372036854775808", "9223372036854775807"},
	"uint8": {"0", "255"}, "byte": {"0", "255"}, "uint16": {"0", "65535"},
	"uint32": {"0", "4294967295"}, "uint64": {"0", "18446744073709551615"},
	"uint": {"0", "18446744073709551615"}, "uintptr": {"0", "18446744073709551615"},
	"rune": {"-2147483648", "2147483647"},
}

// explainOverflow explains a constant that does not fit its type.
func explainOverflow(s *spec) {
	line := overflowSuffix.ReplaceAllString(s.first, "")
	m := cannotUse.FindStringSubmatch(line)
	if m == nil {
		return
	}
	T := m[2]
	r, ok := intRanges[T]
	if !ok {
		return
	}
	rhs := s.exprAt()
	if rhs == nil {
		return
	}
	s.d.Text = fmt.Sprintf("%s holds whole numbers from %s to %s, and %s is outside that range. A constant must fit the type it is used as; the compiler will not cut it down silently.\nUse a wider type, or a value inside the range.", T, r[0], r[1], types2.ExprString(rhs))
}

// ----------------------------------------------------------------------------
// Names

// stdPackages maps the names of standard library packages to their import
// paths, to suggest an import for an undeclared name.
var stdPackages = map[string]string{
	"alloc": "pluto/alloc", "atomic": "pluto/sync/atomic", "bits": "pluto/bits",
	"bytes": "pluto/bytes", "errors": "pluto/errors", "fmt": "pluto/fmt",
	"hash": "pluto/hash", "io": "pluto/io", "math": "pluto/math", "os": "pluto/os",
	"rt": "pluto/rt", "sort": "pluto/sort", "strconv": "pluto/strconv",
	"strings": "pluto/strings", "sync": "pluto/sync", "syscall": "pluto/syscall",
	"testing": "pluto/testing", "utf8": "pluto/unicode/utf8", "vec": "pluto/vec",
	"x86": "pluto/arch/x86",
}

var undefinedName = regexp.MustCompile(`^undefined: (\w+)`)

// explainUndeclared explains "undefined: x": a missing import, a name
// declared after its use, or a misspelling of a name in scope.
func explainUndeclared(s *spec) {
	m := undefinedName.FindStringSubmatch(s.first)
	if m == nil {
		return
	}
	name := m[1]
	line := s.where(s.pos)
	if path, ok := stdPackages[name]; ok {
		s.d.Text = fmt.Sprintf("%s is not declared in this file. If you meant the standard library package %s, the file must import it; each file lists the packages it uses.", name, path)
		s.d.Fix = &Fix{Intro: "Add the import", Code: fmt.Sprintf("import %q", path)}
		return
	}
	// A local declared later in the same function.
	if fn, _ := find[*ast.FuncDecl](s); fn != nil {
		for id := range s.Info.Defs {
			if id.Name == name && id.Pos() > s.pos && id.Pos() < fn.End() && id.Pos() > fn.Pos() {
				s.d.Text = fmt.Sprintf("%s is declared on %s, after this use on %s. A local variable can only be used after its declaration. Move the declaration up, or the use down.", name, s.where(id.Pos()), line)
				return
			}
		}
	}
	var scope *types2.Scope
	if s.Pkg != nil {
		scope = s.Pkg.Scope().Innermost(s.pos)
	}
	var names []string
	declared := map[string]types2.Object{}
	for sc := scope; sc != nil; sc = sc.Parent() {
		for _, n := range sc.Names() {
			obj := sc.Lookup(n)
			local := sc != s.Pkg.Scope() && sc.Parent() != nil
			if local && obj.Pos() > s.pos {
				continue
			}
			if _, seen := declared[n]; !seen {
				declared[n] = obj
				names = append(names, n)
			}
		}
	}
	base := fmt.Sprintf("%s is not declared in any scope that reaches %s.", name, line)
	if alt := closest(name, names); alt != "" {
		obj := declared[alt]
		where := "a builtin"
		if obj.Pos().IsValid() {
			where = "declared on " + s.where(obj.Pos())
		}
		s.d.Text = fmt.Sprintf("%s Did you mean %s (%s)? Names are case-sensitive.", base, alt, where)
		return
	}
	s.d.Text = base + " Check the spelling (names are case-sensitive), declare it before this point with var or :=, or import the package it belongs to."
}

var undefinedQualified = regexp.MustCompile(`^undefined: (\w+)\.(\w+)`)

// explainImportedName explains "undefined: pkg.Name".
func explainImportedName(s *spec) {
	m := undefinedQualified.FindStringSubmatch(s.first)
	if m == nil {
		return
	}
	sel, ok := find[*ast.SelectorExpr](s)
	if !ok {
		return
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}
	pn, ok := s.objOf(x).(*types2.PkgName)
	if !ok {
		return
	}
	pkg := pn.Imported()
	var exported []string
	for _, n := range pkg.Scope().Names() {
		if ast.IsExported(n) {
			exported = append(exported, n)
		}
	}
	text := fmt.Sprintf("Package %s has no exported name %s.", pkg.Path(), m[2])
	if alt := closest(m[2], exported); alt != "" {
		text += fmt.Sprintf(" Did you mean %s.%s?", m[1], alt)
	} else if !ast.IsExported(m[2]) {
		text += " Only names that start with an upper-case letter can be used from another package."
	}
	s.d.Text = text
}

// explainFieldOrMethod explains "x.f undefined (type T has no field or
// method f)": the type of x, where it is declared, and its fields and
// methods.
func explainFieldOrMethod(s *spec) {
	sel, ok := find[*ast.SelectorExpr](s)
	if !ok || sel.Sel.Pos() != s.pos {
		return
	}
	T := s.typeOf(sel.X)
	if T == nil {
		return
	}
	var names []string
	base := T
	if p, ok := T.Underlying().(*types2.Pointer); ok {
		base = p.Elem()
	}
	if st, ok := base.Underlying().(*types2.Struct); ok {
		for i := range st.NumFields() {
			names = append(names, st.Field(i).Name())
		}
	}
	for _, t := range []types2.Type{base, types2.NewPointer(base)} {
		ms := types2.NewMethodSet(t)
		for i := range ms.Len() {
			if n := ms.At(i).Obj().Name(); !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	x := types2.ExprString(sel.X)
	text := fmt.Sprintf("%s has type %s", x, s.str(T))
	if named, ok := types2.Unalias(base).(*types2.Named); ok && named.Obj().Pos().IsValid() {
		text += fmt.Sprintf(", declared on %s", s.where(named.Obj().Pos()))
	}
	text += fmt.Sprintf(", which has no field or method %s.", sel.Sel.Name)
	if alt := closest(sel.Sel.Name, names); alt != "" {
		text += fmt.Sprintf(" Did you mean %s.%s? Names are case-sensitive.", x, alt)
	} else if len(names) > 0 && len(names) <= 8 {
		text += fmt.Sprintf(" Its fields and methods are %s.", strings.Join(names, ", "))
	} else if len(names) == 0 {
		text += " It has no fields or methods at all."
	}
	s.d.Text = text
}

// ----------------------------------------------------------------------------
// Calls and returns

// explainArgCount explains a call with too many or too few arguments, with
// the callee's parameters and where it is declared.
func explainArgCount(s *spec) {
	call, ok := find[*ast.CallExpr](s)
	if !ok {
		return
	}
	sig, _ := s.typeOf(call.Fun).(*types2.Signature)
	if sig == nil {
		return
	}
	name := types2.ExprString(call.Fun)
	text := name + " is declared"
	if fn := s.callee(call); fn != nil && fn.Pos().IsValid() {
		text += " on " + s.where(fn.Pos())
	}
	text += fmt.Sprintf(" with %s: %s. This call passes %s.", plural(sig.Params().Len(), "parameter"), s.signature(sig), plural(len(call.Args), "argument"))
	if strings.HasPrefix(s.first, "too many") {
		text += " Remove the extra arguments, or check that you are calling the function you meant."
	} else {
		text += " Pass a value for each parameter that has no default value."
	}
	s.d.Text = text
}

// explainResultCount explains a return statement with the wrong number of
// values.
func explainResultCount(s *spec) {
	name, sig, decl := s.enclosingFunc()
	if sig == nil {
		return
	}
	var types []string
	for i := range sig.Results().Len() {
		types = append(types, s.str(sig.Results().At(i).Type()))
	}
	switch len(types) {
	case 0:
		s.d.Text = fmt.Sprintf("%s is declared on %s with no results, so its return statements cannot give values.", name, s.where(decl))
	default:
		s.d.Text = fmt.Sprintf("%s is declared on %s to return %s (%s), so each return statement gives exactly that many values, in that order.", name, s.where(decl), plural(len(types), "value"), strings.Join(types, ", "))
	}
}

var inferMsg = regexp.MustCompile(`^in call to (.+), cannot infer (\w+)`)

// explainInfer explains type arguments that a call does not determine.
func explainInfer(s *spec) {
	m := inferMsg.FindStringSubmatch(s.first)
	if m == nil {
		return
	}
	call, ok := find[*ast.CallExpr](s)
	if !ok {
		return
	}
	fn := m[1]
	var tparams []string
	if sig, _ := s.typeOf(call.Fun).(*types2.Signature); sig != nil {
		for i := range sig.TypeParams().Len() {
			tparams = append(tparams, sig.TypeParams().At(i).Obj().Name())
		}
	}
	if len(tparams) == 0 {
		tparams = []string{m[2]}
	}
	var args []string
	for _, a := range call.Args {
		args = append(args, types2.ExprString(a))
	}
	s.d.Text = fmt.Sprintf("%s is generic: it has the type parameter list [%s], and a call normally works the types out from its arguments. %s does not appear in the type of any argument given here (it may appear only in the results, or in parameters left out), so the compiler cannot tell which type to use.", fn, strings.Join(tparams, ", "), m[2])
	s.d.Fix = &Fix{Intro: "Give the type arguments in square brackets, with the types you want", Code: fmt.Sprintf("%s[%s](%s)", fn, strings.Join(tparams, ", "), strings.Join(args, ", "))}
}

// explainMissingReturn explains a function whose end can be reached
// without a return statement, and suggests a return of zero values.
func explainMissingReturn(s *spec) {
	var name string
	var sig *types2.Signature
	var decl token.Pos
	for _, n := range s.path {
		switch f := n.(type) {
		case *ast.FuncDecl:
			if f.Body != nil && f.Body.Rbrace == s.pos && sig == nil {
				if obj := s.Info.Defs[f.Name]; obj != nil {
					name, decl = f.Name.Name, f.Pos()
					sig, _ = obj.Type().(*types2.Signature)
				}
			}
		case *ast.FuncLit:
			if f.Body.Rbrace == s.pos && sig == nil {
				name, decl = "The function literal", f.Pos()
				sig, _ = s.typeOf(f).(*types2.Signature)
			}
		}
	}
	if sig == nil || sig.Results().Len() == 0 {
		return
	}
	var types, zeros []string
	named := false
	for i := range sig.Results().Len() {
		r := sig.Results().At(i)
		types = append(types, s.str(r.Type()))
		zeros = append(zeros, s.zeroValue(r.Type()))
		named = named || r.Name() != ""
	}
	s.d.Text = fmt.Sprintf("%s is declared on %s to return %s, but the end of its body on %s can be reached without a return statement. Every path through a function with results must end in a return, a panic, or a loop that never exits.", name, s.where(decl), strings.Join(types, ", "), s.where(s.pos))
	code := "return " + strings.Join(zeros, ", ")
	if named {
		code = "return"
	}
	s.d.Fix = &Fix{Intro: "Return a value at the end, for example", Code: code}
}

// ----------------------------------------------------------------------------
// Unused names

var unusedVar = regexp.MustCompile(`declared and not used: (\w+)|^(\w+) declared and not used`)

// explainUnusedVar explains a variable whose value is never used.
func explainUnusedVar(s *spec) {
	m := unusedVar.FindStringSubmatch(s.first)
	if m == nil {
		return
	}
	name := m[1] + m[2]
	s.d.Text = fmt.Sprintf("%s is declared on %s, but its value is never used. Plutonium, like Go, treats this as an error, because an unused variable often means a mistake, such as assigning to the wrong variable or forgetting a check.\nDelete the variable, use it, or replace it with _ where it is declared if you only need the other values.", name, s.where(s.pos))
	s.d.Fix = &Fix{Intro: "To keep it for now", Code: "_ = " + name}
}

var unusedImport = regexp.MustCompile(`^"([^"]+)" imported (?:as (\w+) )?and not used`)

// explainUnusedImport explains an import the file does not use.
func explainUnusedImport(s *spec) {
	m := unusedImport.FindStringSubmatch(s.first)
	if m == nil {
		return
	}
	path, name := m[1], m[2]
	if name == "" {
		name = path[strings.LastIndex(path, "/")+1:]
	}
	s.d.Text = fmt.Sprintf("This file imports %s on %s, but nothing in the file uses %s. Plutonium, like Go, treats an unused import as an error, so that each file's imports show what it depends on. Delete the import, or use the package.", path, s.where(s.pos), name)
}

// ----------------------------------------------------------------------------
// Operators

var mismatched = regexp.MustCompile(`\(mismatched types (.+) and (.+)\)$`)

// explainMismatched explains a binary operation on operands of two types.
func explainMismatched(s *spec) {
	if !mismatched.MatchString(s.first) {
		return
	}
	var bin *ast.BinaryExpr
	for _, n := range s.path {
		if b, ok := n.(*ast.BinaryExpr); ok {
			bin = b
			break
		}
	}
	if bin == nil {
		return
	}
	X, Y := s.typeOf(bin.X), s.typeOf(bin.Y)
	if X == nil || Y == nil {
		return
	}
	x, y := types2.ExprString(bin.X), types2.ExprString(bin.Y)
	what := func(e string, T types2.Type) string {
		if isUntyped(T) {
			return fmt.Sprintf("%s is an %s constant", e, s.str(T))
		}
		return fmt.Sprintf("%s has type %s", e, s.str(T))
	}
	text := fmt.Sprintf("%s and %s. The operands of %s must have the same type, and Plutonium, like Go, never converts one to match the other.", what(x, X), what(y, Y), bin.Op)
	numeric := isKind(X, types2.IsNumeric) && isKind(Y, types2.IsNumeric)
	switch {
	case isKind(X, types2.IsString) != isKind(Y, types2.IsString) && (isKind(X, types2.IsNumeric) || isKind(Y, types2.IsNumeric)):
		text += " Text and numbers never mix: a number is not turned into its digits, nor text into a number."
	case isKind(X, types2.IsBoolean) != isKind(Y, types2.IsBoolean):
		text += " A bool is true or false; it is not a number."
	case numeric:
		text += " This holds even between number types such as int and int64."
		if !isUntyped(X) && !isUntyped(Y) {
			s.d.Fix = &Fix{Intro: "Convert one operand to the other's type", Code: fmt.Sprintf("%s(%s) %s %s", s.str(Y), x, bin.Op, y)}
		}
	}
	s.d.Text = text
}

var undefinedOp = regexp.MustCompile(`operator (\S+) not defined on (.+)$`)

// explainUndefinedOp explains an operator applied to a type without it.
func explainUndefinedOp(s *spec) {
	m := undefinedOp.FindStringSubmatch(s.first)
	if m == nil {
		return
	}
	op := m[1]
	var operand ast.Expr
	for _, n := range s.path {
		switch e := n.(type) {
		case *ast.BinaryExpr:
			operand = e.X
		case *ast.UnaryExpr:
			operand = e.X
		}
		if operand != nil {
			break
		}
	}
	if operand == nil {
		return
	}
	T := s.typeOf(operand)
	if T == nil {
		return
	}
	var kinds string
	switch op {
	case "+":
		kinds = "numbers and constant strings"
	case "-", "*", "/":
		kinds = "numbers"
	case "%", "&", "|", "^", "&^", "<<", ">>":
		kinds = "integers"
	case "<", "<=", ">", ">=":
		kinds = "numbers and strings"
	case "!", "&&", "||":
		kinds = "bools"
	default:
		return
	}
	s.d.Text = fmt.Sprintf("%s has type %s, and the operator %s works only on %s.", types2.ExprString(operand), s.str(T), op, kinds)
}

// Closest returns the candidate most like name, if one is close enough to
// be a likely misspelling, and "" otherwise.
func Closest(name string, candidates []string) string { return closest(name, candidates) }

// ----------------------------------------------------------------------------
// append

// explainAppend replaces the catalog's fix for append with the Plutonium
// form of the idiom the call is: growing a variable (Push or Append on a
// vec.Vec), appending a string's bytes (vec.AppendString), copying
// (slices.Clone), concatenating (slices.Concat), deleting a range
// (slices.Delete), or filling a local array (vec.Fixed).
func explainAppend(s *spec) {
	var call *ast.CallExpr
	for _, n := range s.path {
		if c, ok := n.(*ast.CallExpr); ok && c.Fun.Pos() == s.pos {
			call = c
			break
		}
	}
	if call == nil || len(call.Args) == 0 {
		return
	}
	// A fix of its own needs the text with it (Explanation takes both
	// from the catalog when the text is empty).
	if e := Lookup(s.d.Code); e != nil && s.d.Text == "" {
		s.d.Text = e.Text
	}
	x := call.Args[0]
	xs := types2.ExprString(x)
	rest := call.Args[1:]
	dots := call.Ellipsis.IsValid()
	elem := "T"
	if sl, ok := s.typeOf(x).Underlying().(*types2.Slice); ok {
		elem = s.str(sl.Elem())
	}
	var args []string
	for _, a := range rest {
		args = append(args, types2.ExprString(a))
	}
	list := strings.Join(args, ", ")
	// The variable the result goes to, if the call is the right side of an
	// assignment of one.
	lhs := ""
	if as, ok := find[*ast.AssignStmt](s); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 && as.Rhs[0] == ast.Expr(call) {
		lhs = types2.ExprString(as.Lhs[0])
	}
	result := lhs
	if result == "" || result == xs || result == "_" {
		result = "c"
	}

	// append(s[:i], s[j:]...): deleting s[i:j].
	if lo, ok := x.(*ast.SliceExpr); ok && dots && len(rest) == 1 && lo.Low == nil && lo.High != nil && lo.Max == nil {
		if hi, ok := rest[0].(*ast.SliceExpr); ok && hi.Low != nil && hi.High == nil &&
			types2.ExprString(hi.X) == types2.ExprString(lo.X) {
			base := types2.ExprString(lo.X)
			s.d.Fix = &Fix{Intro: "To delete elements from a slice in place, use slices.Delete",
				Code: fmt.Sprintf("import \"pluto/slices\"\n\n%s = slices.Delete(%s, %s, %s)", base, base,
					types2.ExprString(lo.High), types2.ExprString(hi.Low))}
			return
		}
	}
	// append(buf[:0], ...) of an array: filling the array.
	if sl, ok := x.(*ast.SliceExpr); ok && sl.Low == nil && isZero(sl.High) && sl.Max == nil {
		if _, ok := s.typeOf(sl.X).Underlying().(*types2.Array); ok {
			arr := types2.ExprString(sl.X)
			add := "b.Push(" + list + ")"
			if len(rest) != 1 || dots {
				add = "b.Append(" + list + dotsText(dots) + ")"
			}
			if dots && len(rest) == 1 && isKind(s.typeOf(rest[0]), types2.IsString) {
				add = "vec.AppendString(&b, " + list + ")"
			}
			s.d.Fix = &Fix{Intro: fmt.Sprintf("To fill %s without allocating, use a vec.Fixed over it, which keeps what fits", arr),
				Code: fmt.Sprintf("import \"pluto/vec\"\n\nb := vec.NewFixed(%s[:])\n%s\n// b.Slice() holds the elements; b.Truncated() reports what did not fit", arr, add)}
			return
		}
	}
	// append([]T(nil), s...) and append([]T{}, s...): copying.
	if dots && len(rest) == 1 && isEmptySlice(x) {
		s.d.Fix = &Fix{Intro: "To copy a slice into new storage, use slices.Clone, which returns a vec.Vec that owns the copy",
			Code: fmt.Sprintf("import \"pluto/slices\"\n\n%s := slices.Clone(%s)\ndefer %s.Free()", result, list, result)}
		return
	}
	// append(b, s...) with a string s.
	if dots && len(rest) == 1 && isKind(s.typeOf(rest[0]), types2.IsString) {
		s.d.Fix = &Fix{Intro: fmt.Sprintf("Keep %s in a vec.Vec[byte], which grows, and append the string's bytes with vec.AppendString", xs),
			Code: fmt.Sprintf("import \"pluto/vec\"\n\n%s := vec.New[byte]()\ndefer %s.Free()\nvec.AppendString(&%s, %s)", xs, xs, xs, list)}
		return
	}
	// x = append(x, ...): growing x.
	if lhs != xs && dots && len(rest) == 1 {
		s.d.Fix = &Fix{Intro: "To join two slices into new storage, use slices.Concat, which returns a vec.Vec that owns the result",
			Code: fmt.Sprintf("import \"pluto/slices\"\n\n%s := slices.Concat(%s, %s)\ndefer %s.Free()", result, xs, list, result)}
		return
	}
	add := fmt.Sprintf("%s.Push(%s)", xs, list)
	if len(rest) != 1 || dots {
		add = fmt.Sprintf("%s.Append(%s%s)", xs, list, dotsText(dots))
	}
	if _, ok := x.(*ast.Ident); !ok {
		s.d.Fix = &Fix{Intro: fmt.Sprintf("Make %s a vec.Vec[%s], which grows from an allocator", xs, elem), Code: add}
		return
	}
	s.d.Fix = &Fix{Intro: fmt.Sprintf("Keep %s in a vec.Vec[%s], which grows from an allocator", xs, elem),
		Code: fmt.Sprintf("import \"pluto/vec\"\n\n%s := vec.New[%s]()\ndefer %s.Free()\n%s", xs, elem, xs, add)}
}

func dotsText(dots bool) string {
	if dots {
		return "..."
	}
	return ""
}

// isZero reports whether e is the literal 0.
func isZero(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Value == "0"
}

// isEmptySlice reports whether e is []T(nil) or []T{}.
func isEmptySlice(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.CallExpr:
		if _, ok := e.Fun.(*ast.ArrayType); ok && len(e.Args) == 1 {
			id, ok := e.Args[0].(*ast.Ident)
			return ok && id.Name == "nil"
		}
	case *ast.CompositeLit:
		_, ok := e.Type.(*ast.ArrayType)
		return ok && len(e.Elts) == 0
	}
	return false
}

// explainMakeSlice replaces the catalog's fix for make([]T, ...) with the
// call written with vec.Make or vec.WithCapacity, and an array when the
// size is a constant.
func explainMakeSlice(s *spec) {
	var call *ast.CallExpr
	for _, n := range s.path {
		if c, ok := n.(*ast.CallExpr); ok && c.Fun.Pos() == s.pos {
			call = c
			break
		}
	}
	if call == nil || len(call.Args) < 2 {
		return
	}
	if e := Lookup(s.d.Code); e != nil && s.d.Text == "" {
		s.d.Text = e.Text
	}
	elem := sliceElemOf(s.typeOf(call.Args[0]))
	if elem == nil {
		return
	}
	el := s.str(elem)
	name := "v"
	if as, ok := find[*ast.AssignStmt](s); ok && len(as.Lhs) == 1 && len(as.Rhs) == 1 && as.Rhs[0] == ast.Expr(call) {
		if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			name = id.Name
		}
	}
	// Sizes, and the allocator if the last argument is one.
	var sizes []ast.Expr
	alloc := ""
	for _, a := range call.Args[1:] {
		if isKind(s.typeOf(a), types2.IsInteger) || isUntyped(s.typeOf(a)) {
			sizes = append(sizes, a)
		} else {
			alloc = ", " + types2.ExprString(a)
		}
	}
	if len(sizes) == 0 {
		return
	}
	n := types2.ExprString(sizes[0])
	if tv, ok := s.Info.Types[sizes[0]]; ok && tv.Value != nil && len(sizes) == 1 && alloc == "" && name != "v" {
		s.d.Fix = &Fix{Intro: "The size is a constant, so an array needs no allocation",
			Code: fmt.Sprintf("var %s [%s]%s", name, n, el)}
		return
	}
	if ret, ok := find[*ast.ReturnStmt](s); ok && len(ret.Results) == 1 && ret.Results[0] == ast.Expr(call) {
		mk := fmt.Sprintf("vec.Make[%s](%s%s)", el, n, alloc)
		if len(sizes) == 2 && n == "0" {
			mk = fmt.Sprintf("vec.WithCapacity[%s](%s%s)", el, types2.ExprString(sizes[1]), alloc)
		}
		s.d.Fix = &Fix{Intro: fmt.Sprintf("Return a vec.Vec[%s], which owns the array, and have the caller free it", el),
			Code: "import \"pluto/vec\"\n\nreturn " + mk}
		return
	}
	var code string
	switch {
	case len(sizes) == 1 || n == types2.ExprString(sizes[1]):
		code = fmt.Sprintf("%s := vec.Make[%s](%s%s)\ndefer %s.Free()", name, el, n, alloc, name)
	case n == "0":
		code = fmt.Sprintf("%s := vec.WithCapacity[%s](%s%s)\ndefer %s.Free()", name, el, types2.ExprString(sizes[1]), alloc, name)
	default:
		code = fmt.Sprintf("%s := vec.Make[%s](%s%s)\ndefer %s.Free()\n%s.Reserve(%s - %s)", name, el, n, alloc, name, name, types2.ExprString(sizes[1]), n)
	}
	s.d.Fix = &Fix{Intro: "Make a vec.Vec, which owns the array", Code: "import \"pluto/vec\"\n\n" + code}
}

// sliceElemOf returns the element type of a slice type, or of the slice
// type that every type in a type parameter's set has, or nil.
func sliceElemOf(T types2.Type) types2.Type {
	if T == nil {
		return nil
	}
	if tp, ok := T.(*types2.TypeParam); ok {
		iface, ok := tp.Constraint().Underlying().(*types2.Interface)
		if !ok {
			return nil
		}
		var elem types2.Type
		for i := 0; i < iface.NumEmbeddeds(); i++ {
			u, ok := iface.EmbeddedType(i).(*types2.Union)
			if !ok || u.Len() != 1 {
				return nil
			}
			sl, ok := u.Term(0).Type().Underlying().(*types2.Slice)
			if !ok {
				return nil
			}
			elem = sl.Elem()
		}
		return elem
	}
	if sl, ok := T.Underlying().(*types2.Slice); ok {
		return sl.Elem()
	}
	return nil
}
