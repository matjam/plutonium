package escape

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
	"github.com/matjam/plutonium/internal/types2"
)

// Access paths. A path is a sequence of steps, each a field offset ("3."
// for field 3) or a load ("*"). The trailing dot keeps string prefixes on
// step boundaries. Selecting a field of a struct value, as opposed to
// offsetting a pointer, is not a step: values are tracked as a whole.
//
// maxDepth bounds the loads and maxSteps the steps a path may have; a
// longer one is cut short and marked ge, "this path or any extension".
const (
	maxDepth = 3
	maxSteps = 8
)

func pathDepth(p string) int { return strings.Count(p, "*") }

// maxFieldNest is how many nested fields a path tells apart between loads.
const maxFieldNest = 2

// fieldTail counts the field steps after a path's last load.
func fieldTail(p string) int { return strings.Count(p[strings.LastIndexByte(p, '*')+1:], ".") }

func pathSteps(p string) int { return strings.Count(p, "*") + strings.Count(p, ".") }

// A tokKind says where a pointer may point.
type tokKind uint8

const (
	tHeap  tokKind = iota // the heap, static memory, or a device: never a frame
	tFrame                // a location in the frame being analysed (or an enclosing one)
	tParam                // whatever parameter idx of scope sc held, followed along path
)

// A tok is one thing a pointer-carrying value may point at. A value's
// abstract form is a set of them; a value with no pointers has none.
type tok struct {
	kind tokKind
	loc  *loc   // tFrame
	path string // tFrame: the field within loc; tParam: the access path from the parameter
	sc   *scope // tParam
	idx  int    // tParam: parameter index, receiver first
	ge   bool   // tParam: the path or any extension of it

	// In a struct value, the field path (as in locations, up to
	// maxFieldNest fields) of the field that holds the pointer; empty when
	// unknown or the value is not a struct. Struct values are told apart
	// field by field, like locations.
	tag string
}

// untag drops the field tag: the token as a pointer in its own right.
func (t tok) untag() tok {
	t.tag = ""
	return t
}

// tagField marks every token in ts as held in field i of a struct value.
func tagField(ts tset, i int) tset {
	var out tset
	for _, t := range ts {
		t.tag = capFields(fieldStep(i) + t.tag)
		out.add(t)
	}
	return out
}

// retag marks every token in ts with field path tag.
func retag(ts tset, tag string) tset {
	if tag == "" {
		return ts
	}
	var out tset
	for _, t := range ts {
		t.tag = tag
		out.add(t)
	}
	return out
}

// selectField returns the tokens of struct value ts that field i may hold.
func selectField(ts tset, i int) tset {
	var out tset
	step := fieldStep(i)
	for _, t := range ts {
		switch {
		case t.tag == "":
			out.add(t)
		case strings.HasPrefix(t.tag, step):
			t.tag = t.tag[len(step):]
			out.add(t)
		}
	}
	return out
}

// capFields cuts a field path to maxFieldNest fields.
func capFields(p string) string {
	n := 0
	for i := 0; i < len(p); i++ {
		if p[i] == '.' {
			if n++; n == maxFieldNest {
				return p[:i+1]
			}
		}
	}
	return p
}

var heapTok = tok{kind: tHeap}

func frameTok(l *loc) tok { return tok{kind: tFrame, loc: l} }

func paramTok(sc *scope, idx int) tok { return tok{kind: tParam, sc: sc, idx: idx} }

// extend appends a step to a parameter token's path.
func (t tok) extend(step string) tok {
	if t.ge {
		return t
	}
	p := t.path + step
	if pathDepth(p) > maxDepth || pathSteps(p) > maxSteps {
		// Too long: cut the path back to its first load and one more,
		// so that the tokens past the limit stay few. A long path has at
		// least two loads, so the pointers it reaches are all reachable
		// from the memory the first load's result points to.
		t.ge = true
		if i := strings.IndexByte(t.path, '*'); i >= 0 {
			t.path = t.path[:i+1] + "*"
		} else {
			t.path = ""
		}
		return t
	}
	t.path = p
	return t
}

// widen makes a parameter token stand for its path and every extension.
func (t tok) widen() tok {
	t.ge = true
	return t
}

// fieldStep is the path step for field i.
func fieldStep(i int) string { return strconv.Itoa(i) + "." }

// A tset is a small set of tokens.
type tset []tok

func (s tset) has(t tok) bool {
	for _, u := range s {
		if u == t {
			return true
		}
	}
	return false
}

// add adds t and reports whether it was new.
func (s *tset) add(t tok) bool {
	if s.has(t) {
		return false
	}
	*s = append(*s, t)
	return true
}

// addAll adds every token of u and reports whether any was new.
func (s *tset) addAll(u tset) bool {
	grew := false
	for _, t := range u {
		if s.add(t) {
			grew = true
		}
	}
	return grew
}

func union(sets ...tset) tset {
	var out tset
	for _, s := range sets {
		out.addAll(s)
	}
	return out
}

// A loc is a piece of frame storage: a local variable, a parameter's slot,
// a result, or a temporary the compiler places in the frame (an interface
// box, a closure record, a slice literal's backing array).
type loc struct {
	desc     string          // for messages: "a pointer to local variable x"
	hint     hint            // what to do instead, for messages
	level    int             // nesting depth of the function literal that owns it; 0 for the declared function
	contents map[string]tset // what the storage may hold, by field path within it

	// The type stored, when known, and whether the location is an
	// interface box. A pointer to the whole location used as an interface
	// data word then has a known dynamic type: the boxed type, or a
	// pointer to the stored one.
	typ types2.Type
	box bool
}

// put records that v is stored at field path p of l and reports whether
// that added anything.
func (l *loc) put(p string, v tset) bool {
	if len(v) == 0 {
		return false
	}
	if l.contents == nil {
		l.contents = map[string]tset{}
	}
	grew := false
	for _, t := range v {
		k := capFields(p + t.tag)
		s := l.contents[k]
		if s.add(t.untag()) {
			grew = true
		}
		l.contents[k] = s
	}
	return grew
}

// get returns what a load of field path p of l may yield: whatever was
// stored at p, at a field inside it, or at a whole that contains it.
func (l *loc) get(p string) tset {
	keys := make([]string, 0, len(l.contents))
	for k := range l.contents {
		if strings.HasPrefix(p, k) || strings.HasPrefix(k, p) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out tset
	for _, k := range keys {
		// What was stored in a field inside p comes back tagged with the
		// field's path relative to p.
		tag, ok := strings.CutPrefix(k, p)
		if !ok {
			tag = ""
		}
		for _, t := range l.contents[k] {
			t.tag = tag
			out.add(t)
		}
	}
	return out
}

// A hint selects the advice an escape message ends with.
type hint uint8

const (
	hintNone hint = iota
	hintLocal
	hintBox
	hintClosure
)

func (l *loc) hintText() string {
	switch l.hint {
	case hintLocal:
		return " (Plutonium never moves variables to the heap: allocate with new or alloc.New if it must outlive the frame)"
	case hintBox:
		return " (an interface holding a value larger than a word points at a copy in the frame, section 4.4: convert a pointer to memory that lives long enough instead)"
	case hintClosure:
		return " (a closure that captures variables or binds a receiver lives in its frame, section 4.5)"
	}
	return ""
}

// A scope is the body of a declared function, a function literal inside
// it, or the initialisers of a package's variables.
type scope struct {
	fn      *function
	parent  *scope
	level   int
	node    ast.Node // the FuncDecl, FuncLit or nil for package initialisation
	results []*loc
	named   bool // the results have names
}

// contains reports whether pos lies inside the scope's source.
func (sc *scope) contains(pos token.Pos) bool {
	return sc.node != nil && sc.node.Pos() <= pos && pos < sc.node.End()
}

// A pkey names pointers relative to a parameter: those found by following
// path from parameter idx, or any extension of it when ge is set.
type pkey struct {
	idx  int
	path string
	ge   bool
}

func keyOf(t tok) pkey { return pkey{idx: t.idx, path: t.path, ge: t.ge} }

// An edge records that a function stores pointers derived from one
// parameter into memory reached from another.
type edge struct{ src, dst pkey }

// A summary says what a function does with the pointers its caller passes.
type summary struct {
	escapes map[pkey]*reason // stored where they outlive the call
	stores  map[edge]*reason // stored into memory a parameter points to
	results []resultSum
}

// A resultSum says what a result may hold: pointers derived from
// parameters and pointers to the heap, each with the field path of a
// struct result that holds it (or "").
type resultSum struct {
	from map[rkey]bool
	heap map[string]bool
}

type rkey struct {
	pkey
	tag string
}

func (s *summary) addEscape(k pkey, r *reason) bool {
	if s.escapes == nil {
		s.escapes = map[pkey]*reason{}
	}
	if _, ok := s.escapes[k]; ok {
		return false
	}
	s.escapes[k] = r
	return true
}

func (s *summary) addStore(e edge, r *reason) bool {
	if s.stores == nil {
		s.stores = map[edge]*reason{}
	}
	if _, ok := s.stores[e]; ok {
		return false
	}
	s.stores[e] = r
	return true
}

func (s *summary) addResult(i int, k pkey, tag string) bool {
	for len(s.results) <= i {
		s.results = append(s.results, resultSum{})
	}
	r := &s.results[i]
	if r.from == nil {
		r.from = map[rkey]bool{}
	}
	rk := rkey{k, tag}
	if r.from[rk] {
		return false
	}
	r.from[rk] = true
	return true
}

func (s *summary) addResultHeap(i int, tag string) bool {
	for len(s.results) <= i {
		s.results = append(s.results, resultSum{})
	}
	r := &s.results[i]
	if r.heap == nil {
		r.heap = map[string]bool{}
	}
	if r.heap[tag] {
		return false
	}
	r.heap[tag] = true
	return true
}

// A reason explains, for messages, how a parameter escapes inside a
// function: what the function does with it and where.
type reason struct {
	text string // "stores p in package variable G (a.p:12)"
}

func (r *reason) String() string { return r.text }

// shortPos prints a position as file:line, which is enough to find it and
// keeps chained explanations short.
func shortPos(p token.Position) string {
	return fmt.Sprintf("%s:%d", p.Filename, p.Line)
}

// A function is a declared function, method or function literal, with the
// summary the analysis has computed for it so far.
type function struct {
	name     string
	obj      *types2.Func // declared functions
	pkg      *pkgInfo
	decl     *ast.FuncDecl
	lit      *ast.FuncLit
	sig      *types2.Signature
	params   []*types2.Var // receiver first
	sum      summary
	bodyless bool // implemented in assembly or C
	noescape bool // //pu:noescape: arguments are not kept past the call
	inits    bool // the initialisers of a package's variables
}

// paramDesc names the memory a pkey denotes inside f, for messages.
func (f *function) paramDesc(k pkey) string {
	name := fmt.Sprintf("argument %d", k.idx+1)
	if k.idx < len(f.params) && f.params[k.idx].Name() != "" && f.params[k.idx].Name() != "_" {
		name = f.params[k.idx].Name()
	}
	switch {
	case k.ge:
		return "a pointer reachable from " + name
	case pathDepth(k.path) == 0:
		return name
	}
	return "a pointer loaded from " + f.renderPath(k)
}

// renderPath writes a parameter access path in Go syntax, with the loads
// implicit as Go writes them: p.next.val.
func (f *function) renderPath(k pkey) string {
	// expr is the current pointer value, or with isAddr the expression the
	// current pointer points at; T is expr's type.
	expr := f.params[k.idx].Name()
	T := f.params[k.idx].Type()
	isAddr := false
	for _, step := range splitPath(k.path) {
		if step == "*" {
			switch {
			case isAddr:
				isAddr = false
			case pointerElem(T) != nil:
				expr, T = "(*"+expr+")", pointerElem(T)
			case elemType(T) != nil:
				expr, T = expr+"[i]", elemType(T)
			default:
				expr = "*" + expr
			}
			continue
		}
		S := T
		if !isAddr {
			if el := pointerElem(T); el != nil {
				S = el
			}
		}
		i, _ := strconv.Atoi(strings.TrimSuffix(step, "."))
		s, ok := underlying(S).(*types2.Struct)
		if !ok || i >= s.NumFields() {
			expr, isAddr = expr+".?", true
			continue
		}
		expr, T, isAddr = expr+"."+s.Field(i).Name(), s.Field(i).Type(), true
	}
	if isAddr {
		return "&" + expr
	}
	return expr
}

func splitPath(p string) []string {
	var out []string
	for p != "" {
		if p[0] == '*' {
			out = append(out, "*")
			p = p[1:]
			continue
		}
		i := strings.IndexByte(p, '.')
		out = append(out, p[:i+1])
		p = p[i+1:]
	}
	return out
}

// elemType returns the element type of a slice, array or string, or nil.
func elemType(T types2.Type) types2.Type {
	switch u := underlying(T).(type) {
	case *types2.Slice:
		return u.Elem()
	case *types2.Array:
		return u.Elem()
	}
	return nil
}
