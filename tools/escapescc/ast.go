package main

// The call graph from the typed AST, for programs too large to lower in the
// memory a measurement may use (the whole translated compiler): static calls
// by the callee the type checker resolved, and dynamic calls by class
// hierarchy analysis, every method of that name and signature for a call
// through an interface or type parameter, every function or literal used as
// a value of that signature for a call through a func value. That is an
// upper bound of the lowered graph's dynamic edges (which only count types
// the program converts). Implicit calls (drops, index sugar) are left out.
//
// Sizes are AST nodes; the lowered measurement of a program both can handle
// gives the ratio of SSA values to AST nodes.

import (
	"sort"

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

func newASTGraph(prog *load.Program) *graph {
	g := &graph{}
	ids := map[any]int{}
	idOf := func(k any, name string) int {
		if id, ok := ids[k]; ok {
			return id
		}
		id := len(g.nodes)
		ids[k] = id
		g.nodes = append(g.nodes, &node{name: name})
		return id
	}
	type body struct {
		id   int
		node ast.Node // *ast.FuncDecl or *ast.FuncLit
		info *types2.Info
	}
	var bodies []body
	methods := map[string][]int{}            // by name: methods declared in source
	methodSig := map[int]*types2.Signature{} // their signatures
	var values []int                         // functions and literals used as values
	valueSig := map[int]*types2.Signature{}
	for _, p := range prog.Packages {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				obj, _ := p.Info.Defs[fd.Name].(*types2.Func)
				if obj == nil {
					continue
				}
				id := idOf(obj.Origin(), obj.FullName())
				bodies = append(bodies, body{id, fd, p.Info})
				sig := obj.Type().(*types2.Signature)
				if sig.Recv() != nil {
					methods[obj.Name()] = append(methods[obj.Name()], id)
					methodSig[id] = sig
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					id := idOf(lit, "func literal at "+prog.Fset.Position(lit.Pos()).String())
					bodies = append(bodies, body{id, lit, p.Info})
				}
				return true
			})
		}
	}
	type dyn struct {
		from   int
		method string
		sig    *types2.Signature
		fv     bool
	}
	var dyns []dyn
	type edgeKey struct{ from, to int }
	seen := [5]map[edgeKey]bool{{}, {}, {}, {}, {}}
	addEdge := func(ms []mode, from, to int) {
		for _, m := range ms {
			k := edgeKey{from, to}
			if !seen[m][k] {
				seen[m][k] = true
				g.nodes[from].edges[m] = append(g.nodes[from].edges[m], to)
			}
		}
	}
	all := []mode{modeStatic, modeContract, modeIface, modeFunc, modeDynamic}
	for _, b := range bodies {
		info := b.info
		called := map[ast.Expr]bool{}
		walk := func(n ast.Node) bool {
			if n == nil {
				return false
			}
			g.nodes[b.id].values++
			switch n := n.(type) {
			case *ast.FuncLit:
				if n != b.node {
					addEdge(all, b.id, ids[n]) // it builds the closure
					if !called[n] {
						if sig, ok := info.Types[n].Type.(*types2.Signature); ok {
							values = append(values, ids[n])
							valueSig[ids[n]] = sig
						}
					}
					return false // a body of its own
				}
			case *ast.CallExpr:
				fun := ast.Unparen(n.Fun)
				called[fun] = true
				if ix, ok := fun.(*ast.IndexExpr); ok {
					fun = ast.Unparen(ix.X)
					called[fun] = true
				} else if ix, ok := fun.(*ast.IndexListExpr); ok {
					fun = ast.Unparen(ix.X)
					called[fun] = true
				}
				if tv, ok := info.Types[n.Fun]; ok && (tv.IsType() || tv.IsBuiltin()) {
					break
				}
				switch f := fun.(type) {
				case *ast.Ident:
					if fn, ok := info.Uses[f].(*types2.Func); ok {
						addEdge(all, b.id, idOf(fn.Origin(), fn.FullName()))
						return true
					}
				case *ast.SelectorExpr:
					if sel := info.Selections[f]; sel != nil {
						if fn, ok := sel.Obj().(*types2.Func); ok {
							recv := sel.Recv()
							_, isTP := types2.Unalias(recv).(*types2.TypeParam)
							if p, ok := recv.(*types2.Pointer); ok {
								_, isTP = types2.Unalias(p.Elem()).(*types2.TypeParam)
							}
							if sel.Kind() == types2.MethodVal && (types2.IsInterface(recv) || isTP) {
								dyns = append(dyns, dyn{from: b.id, method: fn.Name(), sig: fn.Type().(*types2.Signature)})
								return true
							}
							if fn.Pkg() != nil {
								addEdge(all, b.id, idOf(fn.Origin(), fn.FullName()))
							}
							return true
						}
					} else if fn, ok := info.Uses[f.Sel].(*types2.Func); ok {
						addEdge(all, b.id, idOf(fn.Origin(), fn.FullName()))
						return true
					}
				}
				if sig, ok := info.Types[n.Fun].Type.Underlying().(*types2.Signature); ok {
					dyns = append(dyns, dyn{from: b.id, sig: sig, fv: true})
				}
			case *ast.Ident:
				if called[n] {
					break
				}
				if fn, ok := info.Uses[n].(*types2.Func); ok && fn.Pkg() != nil {
					id := idOf(fn.Origin(), fn.FullName())
					addEdge(all, b.id, id) // it takes the function's address
					values = append(values, id)
					valueSig[id] = plain(fn.Type().(*types2.Signature))
				}
			case *ast.SelectorExpr:
				if called[n] {
					break
				}
				if sel := info.Selections[n]; sel != nil {
					if fn, ok := sel.Obj().(*types2.Func); ok && fn.Pkg() != nil {
						id := idOf(fn.Origin(), fn.FullName())
						addEdge(all, b.id, id)
						values = append(values, id)
						s := fn.Type().(*types2.Signature)
						if sel.Kind() == types2.MethodExpr {
							s = plain(s)
						}
						valueSig[id] = s
					}
				}
			}
			return true
		}
		switch n := b.node.(type) {
		case *ast.FuncDecl:
			ast.Inspect(n.Body, walk)
		case *ast.FuncLit:
			ast.Inspect(n.Body, walk)
		}
	}
	// Dynamic edges by class hierarchy analysis.
	sort.Ints(values)
	for _, d := range dyns {
		if d.fv {
			for _, v := range values {
				if sameShape(valueSig[v], d.sig) {
					addEdge([]mode{modeFunc, modeDynamic}, d.from, v)
				}
			}
			continue
		}
		for _, m := range methods[d.method] {
			if sameShape(methodSig[m], d.sig) {
				addEdge([]mode{modeIface, modeDynamic}, d.from, m)
			}
		}
	}
	return g
}

// plain returns sig with its receiver as the first parameter.
func plain(sig *types2.Signature) *types2.Signature {
	if sig.Recv() == nil {
		return sig
	}
	params := []*types2.Var{sig.Recv()}
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i))
	}
	return types2.NewSignatureType(nil, nil, nil, types2.NewTuple(params...), sig.Results(), sig.Variadic())
}

// sameShape reports whether two signatures may be the same func type or
// method signature (receivers aside): identical, or, where type parameters
// are involved, of the same arity, as an upper bound.
func sameShape(a, b *types2.Signature) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Params().Len() != b.Params().Len() || a.Results().Len() != b.Results().Len() || a.Variadic() != b.Variadic() {
		return false
	}
	for i := 0; i < a.Params().Len(); i++ {
		if !maySame(a.Params().At(i).Type(), b.Params().At(i).Type()) {
			return false
		}
	}
	for i := 0; i < a.Results().Len(); i++ {
		if !maySame(a.Results().At(i).Type(), b.Results().At(i).Type()) {
			return false
		}
	}
	return true
}

func maySame(x, y types2.Type) bool {
	if hasTP(x) || hasTP(y) {
		return true
	}
	return types2.Identical(x, y)
}

func hasTP(t types2.Type) bool {
	switch t := types2.Unalias(t).(type) {
	case *types2.TypeParam:
		return true
	case *types2.Pointer:
		return hasTP(t.Elem())
	case *types2.Slice:
		return hasTP(t.Elem())
	case *types2.Named:
		if ta := t.TypeArgs(); ta != nil {
			for i := 0; i < ta.Len(); i++ {
				if hasTP(ta.At(i)) {
					return true
				}
			}
		}
	}
	return false
}
