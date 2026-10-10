package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// Variables whose address outlives their frame live on the heap in Go
// (escape analysis reports "moved to heap: x"). Plutonium keeps every local
// in the frame, so the translation allocates such a variable explicitly
// and refers to it through a pointer: the declaration x := e becomes
// x := new(e) (Go 1.26's new of a value, from the current allocator), each
// use of x becomes (*x), and &x becomes x.
// A parameter is renamed and copied into a new variable at the top of the
// body. Go's analysis is conservative (an address passed to an interface
// method counts as kept), so some of these allocations are unnecessary in
// Plutonium; they are correct, and can be removed by hand later.

// findHeapVars returns the variables of the package Go moves to the heap.
func (tc *translator) findHeapVars() map[*types.Var]bool {
	heap := map[*types.Var]bool{}
	for id, obj := range tc.info.Defs {
		v, ok := obj.(*types.Var)
		if !ok || v.IsField() || v.Parent() == tc.pkg.Scope() {
			continue
		}
		if tc.esc.has(tc.fset, id.Pos(), "moved to heap: "+id.Name, "") {
			heap[v] = true
		}
	}
	return heap
}

func (t *fileTr) isHeapDef(id *ast.Ident) bool {
	v, ok := t.info.Defs[id].(*types.Var)
	return ok && t.heap[v]
}

func (t *fileTr) isHeapUse(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := t.info.Uses[id].(*types.Var)
	return ok && t.heap[v]
}

// heapDefine translates x := e where x is moved to the heap.
func (t *fileTr) heapDefine(n *ast.AssignStmt) (string, bool) {
	found := false
	for _, l := range n.Lhs {
		if id, ok := l.(*ast.Ident); ok && t.isHeapDef(id) {
			found = true
		}
		if t.isHeapUse(l) {
			t.note(n, "variable %s, moved to heap, redeclared with :=: hand-fix", t.text(l))
			return "", false
		}
	}
	if !found {
		return "", false
	}
	if len(n.Lhs) != len(n.Rhs) {
		t.note(n, "multi-value := of a variable moved to heap: hand-fix")
		return "", false
	}
	lhs := make([]string, len(n.Lhs))
	rhs := make([]string, len(n.Rhs))
	for i, l := range n.Lhs {
		lhs[i] = t.text(l)
		rhs[i] = t.render(n.Rhs[i])
		if id, ok := l.(*ast.Ident); ok && t.isHeapDef(id) {
			rhs[i] = t.puNew(rhs[i])
		}
	}
	return strings.Join(lhs, ", ") + " := " + strings.Join(rhs, ", "), true
}

// heapVarDecl translates var x T [= e] inside a function where x is moved
// to the heap.
func (t *fileTr) heapVarDecl(d *ast.GenDecl) (string, bool) {
	if d.Tok != token.VAR {
		return "", false
	}
	if _, inFunc := t.parent[d].(*ast.DeclStmt); !inFunc {
		return "", false
	}
	var out []string
	found := false
	for _, s := range d.Specs {
		vs := s.(*ast.ValueSpec)
		heap := false
		for _, id := range vs.Names {
			heap = heap || t.isHeapDef(id)
		}
		if !heap {
			out = append(out, "var "+t.render(vs))
			continue
		}
		found = true
		if len(vs.Names) != 1 || len(vs.Values) > 1 {
			t.note(d, "var declaration of several variables, one moved to heap: hand-fix")
			return "", false
		}
		name := vs.Names[0].Name
		switch {
		case len(vs.Values) == 0:
			out = append(out, fmt.Sprintf("%s := %s.Alloc[%s]()", name, t.use(puPkg, "pu"), t.render(vs.Type)))
		case vs.Type != nil:
			out = append(out, fmt.Sprintf("%s := %s", name, t.puNew(convType(t.render(vs.Type))+"("+t.render(vs.Values[0])+")")))
		default:
			out = append(out, fmt.Sprintf("%s := %s", name, t.puNew(t.render(vs.Values[0]))))
		}
	}
	if !found {
		return "", false
	}
	return strings.Join(out, "\n"), true
}

// heapParams translates a function whose parameters or receiver are moved
// to the heap: each is renamed and copied into a heap variable of its
// original name at the top of the body.
func (t *fileTr) heapParams(typ *ast.FuncType, recv *ast.FieldList, body *ast.BlockStmt) (prologue []string, ids []*ast.Ident) {
	if body == nil {
		return nil, nil
	}
	lists := []*ast.FieldList{recv, typ.Params}
	for _, fl := range lists {
		if fl == nil {
			continue
		}
		for _, f := range fl.List {
			for _, id := range f.Names {
				if t.isHeapDef(id) {
					ids = append(ids, id)
					prologue = append(prologue, fmt.Sprintf("%s := %s", id.Name, t.puNew("go"+id.Name)))
				}
			}
		}
	}
	if typ.Results != nil {
		for _, f := range typ.Results.List {
			for _, id := range f.Names {
				if t.isHeapDef(id) {
					t.note(id, "named result %s moved to heap: hand-fix", id.Name)
				}
			}
		}
	}
	return prologue, ids
}

// funcWithHeapParams renders a function declaration or literal whose
// parameters move to the heap, or returns false if none do.
func (t *fileTr) funcWithHeapParams(n ast.Node, typ *ast.FuncType, recv *ast.FieldList, body *ast.BlockStmt) (string, bool) {
	prologue, ids := t.heapParams(typ, recv, body)
	if len(ids) == 0 {
		return "", false
	}
	b := t.render(body)
	t.subst[body] = "{\n" + strings.Join(prologue, "\n") + "\n" + strings.TrimPrefix(b, "{")
	for _, id := range ids {
		t.subst[id] = "go" + id.Name
	}
	s := t.splice(n)
	delete(t.subst, body)
	for _, id := range ids {
		delete(t.subst, id)
	}
	return s, true
}

// convType parenthesises a type for use in a conversion when it would
// otherwise parse differently (*T(x) is *(T(x))).
func convType(typ string) string {
	if strings.HasPrefix(typ, "*") || strings.HasPrefix(typ, "func") || strings.HasPrefix(typ, "<-") {
		return "(" + typ + ")"
	}
	return typ
}

// checkHeapRange notes range variables moved to the heap.
func (t *fileTr) checkHeapRange(r *ast.RangeStmt) {
	for _, e := range []ast.Expr{r.Key, r.Value} {
		if id, ok := e.(*ast.Ident); ok && r.Tok == token.DEFINE && t.isHeapDef(id) {
			t.note(r, "range variable %s moved to heap: hand-fix", id.Name)
		}
	}
}
