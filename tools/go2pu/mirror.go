package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/format"
)

// Some standard packages are used by the compiler only for names: the
// constants and the types of the constants. Their Plutonium counterpart is
// a mirror that go2pu generates, holding the names the translated packages
// refer to, with Go's values (the same values by construction, which no
// hand-written copy guarantees). constMirrors lists them, by Go import path.
// The package directory of a mirror is the last element of its Plutonium
// import path in stdMap.
var constMirrors = map[string]bool{
	"debug/dwarf": true,
}

// noteMirrored records that the translation refers to name of the mirrored
// package p.
func (t *fileTr) noteMirrored(p string, name *ast.Ident) {
	if !constMirrors[p] {
		return
	}
	obj := t.info.Uses[name]
	switch obj.(type) {
	case *types.Const, *types.TypeName:
	default:
		t.note(name, "%s.%s: a mirrored package holds constants and their types only", p, name.Name)
		return
	}
	if t.res.Mirrored == nil {
		t.res.Mirrored = map[string]map[string]types.Object{}
	}
	if t.res.Mirrored[p] == nil {
		t.res.Mirrored[p] = map[string]types.Object{}
	}
	t.res.Mirrored[p][name.Name] = obj
}

// MirrorPackages returns the mirrors of the standard packages the results
// refer to.
func MirrorPackages(cfg *Config, results []*Result) []*Result {
	all := map[string]map[string]types.Object{}
	for _, r := range results {
		for p, names := range r.Mirrored {
			if all[p] == nil {
				all[p] = map[string]types.Object{}
			}
			for n, o := range names {
				all[p][n] = o
			}
		}
	}
	var paths []string
	for p := range all {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []*Result
	for _, p := range paths {
		out = append(out, mirror(cfg, p, all[p]))
	}
	return out
}

// mirror generates the package that mirrors the names of the Go package p.
func mirror(cfg *Config, p string, names map[string]types.Object) *Result {
	res := &Result{GoPath: p, OutPath: strings.ReplaceAll(stdMap[p], "$MOD", cfg.Module)}
	pkgName := p[strings.LastIndex(p, "/")+1:]
	typeNames := map[string]*types.TypeName{}
	var consts []*types.Const
	for _, o := range names {
		switch o := o.(type) {
		case *types.TypeName:
			typeNames[o.Name()] = o
		case *types.Const:
			consts = append(consts, o)
			if n, ok := o.Type().(*types.Named); ok {
				typeNames[n.Obj().Name()] = n.Obj()
			}
		}
	}
	sort.Slice(consts, func(i, j int) bool { return consts[i].Name() < consts[j].Name() })
	var tnames []string
	for n := range typeNames {
		tnames = append(tnames, n)
	}
	sort.Strings(tnames)

	var b bytes.Buffer
	fmt.Fprintf(&b, "%sthe constants of %s. DO NOT EDIT.\n\n", generatedHeader, p)
	fmt.Fprintf(&b, "// Package %s holds the names of Go's %s that the compiler's packages use,\n", pkgName, p)
	b.WriteString("// with the values Go gives them (tools/go2pu, mirror.go).\n")
	fmt.Fprintf(&b, "package %s\n", pkgName)
	for _, n := range tnames {
		fmt.Fprintf(&b, "\ntype %s %s\n", n, types.TypeString(typeNames[n].Type().Underlying(), nil))
	}
	if len(consts) > 0 {
		b.WriteString("\nconst (\n")
		for _, c := range consts {
			typ := ""
			if n, ok := c.Type().(*types.Named); ok {
				typ = " " + n.Obj().Name()
			}
			fmt.Fprintf(&b, "\t%s%s = %s\n", c.Name(), typ, c.Val().ExactString())
		}
		b.WriteString(")\n")
	}
	src, err := format.Source(b.Bytes())
	if err != nil {
		res.Notes = append(res.Notes, fmt.Sprintf("%s: formatting the mirror: %v", p, err))
		src = b.Bytes()
	}
	res.Files = []OutFile{{Name: "constants.p", Src: src}}
	return res
}
