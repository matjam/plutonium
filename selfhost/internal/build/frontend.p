// Translated by go2pu from internal/build/frontend.go at the go-cutover tag; edited by hand since.

package build

import (
	"selfhost/compat"
	"selfhost/internal/load"
	"selfhost/internal/ssa"
	"selfhost/internal/ssawrite"
	"selfhost/internal/syntax/token"
	"selfhost/pu"
)

// A Lowered is a program after the front end: its SSA, before optimisation.
// The mixed pipeline (self-hosting plan, section 6.3) writes it as text for a
// back end built from the Plutonium translation, and compares the object that
// back end writes with the one Object returns.
type Lowered struct {
	prog *load.Program
	SSA  *ssa.Program
}

// Frontend loads, checks and lowers the program in cfg.Dir.
func Frontend(cfg *Config) (*Lowered, error) {
	lcfg, err := loadConfig(cfg)
	if err != nil {
		return nil, err
	}
	prog, err := load.Load(lcfg, cfg.Dir)
	if err != nil {
		return nil, err
	}
	sp, err := lowerProgram(cfg, prog)
	if err != nil {
		return nil, err
	}
	return pu.New(Lowered{prog: prog, SSA: sp}), nil
}

// Fset returns the file set of the program's positions.
func (l *Lowered) Fset() *token.FileSet { return l.prog.Fset }

// Text returns the program in the textual form of internal/ssatext, with
// the names of its files as the object records them (trimmed, unless
// cfg.NoTrimPath).
func (l *Lowered) Text(cfg *Config) ([]byte, error) {
	fset := l.prog.Fset
	if !cfg.NoTrimPath {
		// Record the trimmed names in the text itself, so that the back end
		// needs no mapping from names to names.
		fset = renamed(fset, l.prog.TrimmedName)
	}
	buf := pu.Alloc[compat.Buffer]()
	ssawrite.Write(buf, l.SSA, fset) or return
	return buf.Bytes(), nil
}

// Object optimises the program and returns its object file, as the driver
// would link it. It changes the program: call Text first.
func (l *Lowered) Object(cfg *Config) ([]byte, error) {
	if err := optimize(cfg, l.SSA, l.prog); err != nil {
		return nil, internal(err)
	}
	obj, err := emitObject(cfg, l.SSA, l.prog)
	if err != nil {
		return nil, internal(err)
	}
	buf := pu.Alloc[compat.Buffer]()
	obj.Write(buf) or return
	return buf.Bytes(), nil
}

// renamed returns a copy of fset whose file names, and those of its line
// directives, are mapped by name, as the debug information maps them.
func renamed(fset *token.FileSet, name func(string) string) *token.FileSet {
	out := token.NewFileSet()
	fset.Iterate(func(f *token.File) bool {
		g := out.AddFile(name(f.Name()), f.Base(), f.Size())
		g.SetLines(f.Lines())
		for _, in := range f.LineInfos() {
			g.AddLineColumnInfo(in.Offset, name(in.Filename), in.Line, in.Column)
		}
		return true
	})
	return out
}
