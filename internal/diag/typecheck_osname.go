package diag

import (
	"fmt"
	"strings"

	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/types2"
)

// Names from the operating system (language spec sections 7.3 and F.9). The
// functions of pluto/os that return a name or value the system gave the
// program as a string return an error as well, a *os.NameError when the
// bytes are not valid UTF-8, so a call used as a single value is an
// assignment mismatch (E0017) or a multiple-value use (E0041). The
// explanation says why and names the two other forms: the display form and
// the exact bytes.

// osNameForm describes a function or method of pluto/os that returns a name
// or value under its Go name: the form for display, the form that returns
// the exact bytes, the variables of the two-value use and what the results
// are.
type osNameForm struct {
	lossy, raw string
	vars       string
	results    string
}

// osNameForms is keyed by the function's name, or "Type.Name" for a method.
var osNameForms = map[string]osNameForm{
	"DirEntry.Name": {"NameLossy", "RawName", "name, err", "the name and an error"},
	"FileInfo.Name": {"NameLossy", "RawName", "name, err", "the name and an error"},
	"File.Name":     {"NameLossy", "RawName", "name, err", "the name and an error"},
	"Getenv":        {"GetenvLossy", "GetenvBytes", "v, err", "the value and an error"},
	"LookupEnv":     {"LookupEnvLossy", "LookupEnvBytes", "v, ok, err", "the value, whether the variable is set, and an error"},
	"Arg":           {"ArgLossy", "ArgBytes", "arg, err", "the argument and an error"},
	"Args":          {"ArgsLossy", "ArgsBytes", "args, err", "the arguments and an error"},
	"Environ":       {"EnvironLossy", "EnvironBytes", "env, err", "the environment and an error"},
	"TempDir":       {"TempDirLossy", "TempDirBytes", "dir, err", "the directory and an error"},
}

// explainAssignCount specialises E0017: a call of one of the os name
// functions on the right of an assignment with the wrong number of
// variables, or else the or statement's counts.
func explainAssignCount(s *spec) {
	if explainOSName(s) {
		return
	}
	explainOrCount(s)
}

// explainTooMany specialises E0041: a call of one of the os name functions
// where one value is expected.
func explainTooMany(s *spec) {
	explainOSName(s)
}

// osNameCall returns the call at the error position if it is one of the
// functions of osNameForms, with the key of the function in that table.
func (s *spec) osNameCall() (*ast.CallExpr, string, bool) {
	if s.Info == nil {
		return nil, "", false
	}
	for _, n := range s.path {
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() != s.pos {
			continue
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			return nil, "", false
		}
		fn, ok := s.objOf(sel.Sel).(*types2.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "pluto/os" {
			return nil, "", false
		}
		key := fn.Name()
		if sig, ok := fn.Type().(*types2.Signature); ok && sig.Recv() != nil {
			T := sig.Recv().Type()
			if p, ok := T.(*types2.Pointer); ok {
				T = p.Elem()
			}
			named, ok := T.(*types2.Named)
			if !ok {
				return nil, "", false
			}
			key = named.Obj().Name() + "." + key
		}
		if _, ok := osNameForms[key]; !ok {
			return nil, "", false
		}
		return call, key, true
	}
	return nil, "", false
}

// explainOSName explains the single-value use of an os name function. It
// reports whether the error was one.
func explainOSName(s *spec) bool {
	call, key, ok := s.osNameCall()
	if !ok {
		return false
	}
	f := osNameForms[key]
	sel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	recv := types2.ExprString(sel.X) + "."
	var args []string
	for _, a := range call.Args {
		args = append(args, types2.ExprString(a))
	}
	list := strings.Join(args, ", ")
	callText := types2.ExprString(call)

	what := "os." + key
	if typ, name, found := strings.Cut(key, "."); found {
		what = typ + "." + name
	}
	s.d.Text = fmt.Sprintf("%s returns %s. A name or value that the operating system gives the program is bytes, and the system does not promise that they are valid UTF-8, which a string must be. Plutonium never changes the bytes into a string without saying so: when they are not valid UTF-8 the call returns a *os.NameError that holds them, and a use that takes fewer values would leave that error out. (Go's version returns a string holding the bytes as they are, which a Plutonium string cannot hold.)\nAssign every result and check the error. If you only display the value, %s returns a string with each invalid byte replaced by U+FFFD, and its name says so. If you need the exact bytes, for example to open the file, %s returns them.",
		what, f.results, f.lossy, f.raw)
	s.d.Fix = &Fix{
		Intro: "To use the value and handle a name that is not valid UTF-8",
		Code:  fmt.Sprintf("%s := %s\nif err != nil {\n\t// err is a *os.NameError: err.Bytes holds the exact bytes\n}", f.vars, callText),
	}
	rawCode := fmt.Sprintf("b := %s%s(%s)", recv, f.raw, list)
	if key == "Getenv" || key == "LookupEnv" {
		rawCode += " // the name is a []byte"
	}
	s.d.More = []*Fix{
		{Intro: "To display it, with U+FFFD for each invalid byte", Code: fmt.Sprintf("s := %s%s(%s)", recv, f.lossy, list)},
		{Intro: "To get the exact bytes", Code: rawCode},
	}
	return true
}
