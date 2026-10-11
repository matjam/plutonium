// Package directive checks the //pu: directives of a source file.
//
// A directive is a line comment that starts with //pu: and no space, in the
// doc comment of the declaration it applies to (or before the package clause,
// for //pu:build). The compiler looks for each directive by name where it
// matters, so a misspelt one, a removed one or one in the wrong place would
// otherwise be ignored without a word, and a typo in //pu:interrupt would
// silently change nothing. Check reports each such comment as an error.
package directive

import (
	"slices"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/syntax/ast"
	"github.com/matjam/plutonium/internal/syntax/token"
)

// Prefix starts every directive.
const Prefix = "//pu:"

// GoPrefix starts a Go directive, which is an error (language spec sections
// 4.1 and 20.3).
const GoPrefix = "//go:"

// goEquivalents maps the Go directives that Plutonium has under its own
// prefix to the //pu: directive, with where it applies when that differs.
var goEquivalents = map[string]string{
	"build":    "//pu:build",
	"noinline": "//pu:noinline",
	"noescape": "//pu:noescape, on a function without a body",
}

// A place is a kind of declaration a directive can apply to.
type place int

const (
	atFile place = 1 << iota // the file, before the package clause
	atFunc                   // a function or method declaration
	atVar                    // a package-level variable
)

// An argument says whether a directive takes text after its name.
type argument int

const (
	noArg       argument = iota // takes none
	needsArg                    // needs one
	optionalArg                 // may have one
	checkedElse                 // checked where it is used, so not here
)

type info struct {
	where place
	arg   argument
	need  string // what the argument is, for needsArg
}

// directives lists every directive the compiler knows. The spec's table
// (docs/spec/language/20-system-considerations.md, section 20.3) and the hover text (internal/lsp) say
// what each one means.
var directives = map[string]info{
	"build":     {atFile, needsArg, "a build constraint such as amd64 && !kernel"},
	"inline":    {atFunc, noArg, ""},
	"noinline":  {atFunc, noArg, ""},
	"nochecks":  {atFunc, noArg, ""},
	"extern":    {atFunc, needsArg, "the name of the C symbol"},
	"export":    {atFunc, optionalArg, ""},
	"section":   {atFunc | atVar, needsArg, "the name of the section"},
	"interrupt": {atFunc, noArg, ""},
	"noreturn":  {atFunc, noArg, ""},
	"noescape":  {atFunc, noArg, ""},
	// resultholds is for the standard library; the escape analysis
	// reports it elsewhere (E3008) and checks the names.
	"resultholds": {atFunc, checkedElse, ""},
	"panic":       {atFunc, noArg, ""},
	"blocking":    {atFunc, noArg, ""},
	"scheduler":   {atVar, noArg, ""},
	"nosimd":      {atFunc, noArg, ""},
	"fpu":         {atFunc, noArg, ""},
	"percpu":      {atVar, noArg, ""},
	"stack":       {atFunc, checkedElse, ""},
}

// retired maps a removed directive to why it was removed, finishing the
// sentence "//pu:name was removed: ".
var retired = map[string]string{
	"alloc":     "kernel code may allocate strings anywhere the allocator may be called",
	"bytewise":  "the compiler proves that string slice bounds fall on rune boundaries",
	"gofunc":    "the compiler finds which functions start tasks with their func arguments",
	"schedhook": "the compiler knows the runtime's scheduler hooks by name",
	"schedlate": "the compiler knows the runtime's scheduler hooks by name",
	"nobounds":  "nochecks removes the bounds checks and the generation checks from the function's body, and marks the function unsafe code (language spec section 18.12, Safe and unsafe code).",
}

// replacedBy marks the removed directives that another directive replaces,
// mapping the old name to the new one.
var replacedBy = map[string]string{
	"nobounds": "nochecks",
}

// Names returns the names of the directives the compiler knows, sorted.
func Names() []string {
	names := make([]string, 0, len(directives))
	for n := range directives {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// Retired returns the names of the directives that were removed, sorted.
func Retired() []string {
	names := make([]string, 0, len(retired))
	for n := range retired {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// Split returns the name and the argument of the directive in a comment's
// text, and whether the comment is a directive at all.
func Split(text string) (name, arg string, ok bool) {
	rest, ok := strings.CutPrefix(text, Prefix)
	if !ok {
		return "", "", false
	}
	name, arg, _ = strings.Cut(strings.TrimSpace(rest), " ")
	if i := strings.IndexByte(name, '\t'); i >= 0 {
		name, arg = name[:i], name[i+1:]
	}
	return name, strings.TrimSpace(arg), true
}

// Check reports every //pu: comment of f that the compiler would otherwise
// ignore or misread: an unknown name, a removed directive, a directive
// where it applies to nothing, and an argument it does not take.
func Check(fset *token.FileSet, f *ast.File) []*diag.Diagnostic {
	// Find what each doc comment is the doc of.
	owner := map[*ast.CommentGroup]ownerInfo{}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				owner[d.Doc] = ownerInfo{atFunc, "a function", d}
			}
		case *ast.GenDecl:
			kind, what := atVar, "a package-level variable"
			if d.Tok != token.VAR {
				kind, what = 0, article(d.Tok.String())+" declaration"
			}
			if d.Doc != nil {
				owner[d.Doc] = ownerInfo{kind, what, nil}
			}
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.ValueSpec:
					if s.Doc != nil {
						owner[s.Doc] = ownerInfo{kind, what, nil}
					}
				case *ast.TypeSpec:
					if s.Doc != nil {
						owner[s.Doc] = ownerInfo{0, "a type declaration", nil}
					}
				case *ast.ImportSpec:
					if s.Doc != nil {
						owner[s.Doc] = ownerInfo{0, "an import", nil}
					}
				}
			}
		}
	}

	var out []*diag.Diagnostic
	for _, g := range f.Comments {
		for _, c := range g.List {
			if strings.HasPrefix(c.Text, GoPrefix) {
				out = append(out, goDirective(fset.Position(c.Pos()), c.Text))
				continue
			}
			name, arg, ok := Split(c.Text)
			if !ok {
				continue
			}
			pos := fset.Position(c.Pos())
			where, ownerOK := owner[g]
			beforePackage := c.End() <= f.Package
			if d := checkOne(pos, name, arg, where, ownerOK, beforePackage); d != nil {
				out = append(out, d)
			}
		}
	}
	return out
}

// An ownerInfo says what a doc comment belongs to.
type ownerInfo struct {
	kind place
	what string // "a function", for messages
	fn   *ast.FuncDecl
}

// checkOne checks one directive comment.
func checkOne(pos token.Position, name, arg string, owner ownerInfo, hasOwner, beforePackage bool) *diag.Diagnostic {
	if reason, gone := retired[name]; gone {
		if new, ok := replacedBy[name]; ok {
			d := diag.New(pos, diag.RetiredDirective, "//pu:%s was replaced by //pu:%s", name, new)
			d.Text = "//pu:" + name + " was replaced by //pu:" + new + ": " + reason + " Change the name; the function keeps its meaning, with the generation checks off as well."
			d.Fix = &diag.Fix{Intro: "Use the replacement", Code: Prefix + new}
			return d
		}
		d := diag.New(pos, diag.RetiredDirective, "//pu:%s was removed: %s", name, reason)
		d.Text = "//pu:" + name + " is no longer needed: " + reason + ". Delete the line; the program means the same without it."
		d.Fix = &diag.Fix{Intro: "Delete the line", Code: Prefix + name + " (delete this line)"}
		return d
	}
	info, known := directives[name]
	if !known {
		return unknown(pos, name)
	}
	if name == "build" {
		if !beforePackage {
			return misplaced(pos, name, "before the package clause, where it chooses whether the file is part of the build; here it is ignored")
		}
		return argCheck(pos, name, arg, info)
	}
	if beforePackage {
		return misplaced(pos, name, "in the doc comment of "+placeNames(info.where)+"; it is before the package clause here")
	}
	if !hasOwner {
		return misplaced(pos, name, "in the doc comment of "+placeNames(info.where)+", directly above it; it is not above any declaration here")
	}
	if owner.kind&info.where == 0 {
		return misplaced(pos, name, "in the doc comment of "+placeNames(info.where)+", not of "+owner.what)
	}
	if name == "noescape" && owner.fn != nil && owner.fn.Body != nil {
		d := diag.New(pos, diag.RetiredDirective, "//pu:noescape is inferred for a function with a body: delete it")
		d.Text = "The compiler works out which pointer arguments a function keeps by reading its body, and the calls it makes. The directive is only needed on a function without a body, written in assembly or C, which the compiler cannot look into."
		d.Fix = &diag.Fix{Intro: "Delete the directive", Code: "func sum(p *[4]int) int {\n\treturn p[0] + p[1] + p[2] + p[3]\n}"}
		return d
	}
	if name == "noreturn" && owner.fn != nil && owner.fn.Body != nil {
		d := diag.New(pos, diag.RetiredDirective, "//pu:noreturn is inferred for a function with a body: delete it")
		d.Text = "The compiler finds that a function never returns when it has no return statement and its body ends in a terminating statement, such as an endless loop, a panic, or a call of a function that never returns, however many calls away. The directive is only needed on a function without a body, written in assembly or C, which the compiler cannot look into."
		d.Fix = &diag.Fix{Intro: "Delete the directive", Code: "func halt() {\n\tfor {\n\t}\n}"}
		return d
	}
	return argCheck(pos, name, arg, info)
}

func argCheck(pos token.Position, name, arg string, info info) *diag.Diagnostic {
	switch {
	case info.arg == noArg && arg != "":
		d := diag.New(pos, diag.DirectiveArgument, "//pu:%s takes no argument, found %q", name, arg)
		d.Text = "//pu:" + name + " stands alone on its line. Text after its name is not a comment, and the compiler would not see the directive at all. Put an explanation on a line of its own."
		d.Fix = &diag.Fix{Intro: "Write the directive alone, and the explanation above it", Code: "// " + arg + "\n" + Prefix + name}
		return d
	case info.arg == needsArg && arg == "":
		d := diag.New(pos, diag.DirectiveArgument, "//pu:%s needs an argument: %s", name, info.need)
		d.Text = "//pu:" + name + " takes " + info.need + " after its name, and has none, so it would do nothing."
		d.Fix = &diag.Fix{Intro: "Add the argument", Code: Prefix + name + " ..."}
		return d
	}
	return nil
}

// goDirective reports a //go: comment, naming the //pu: directive that does
// the same job, or saying that there is none.
func goDirective(pos token.Position, text string) *diag.Diagnostic {
	rest := strings.TrimPrefix(text, GoPrefix)
	name, arg, _ := strings.Cut(strings.TrimSpace(rest), " ")
	if pu, ok := goEquivalents[name]; ok {
		d := diag.New(pos, diag.GoDirective, "//go:%s is a Go directive; write %s", name, pu)
		code := Prefix + name
		if arg = strings.TrimSpace(arg); arg != "" {
			code += " " + arg
		}
		d.Text = "Plutonium does not read //go: comments, so this one would do nothing. Its directives start with //pu:, and " + GoPrefix + name + " is " + Prefix + name + " there."
		d.Fix = &diag.Fix{Intro: "Use the //pu: directive", Code: code}
		return d
	}
	d := diag.New(pos, diag.GoDirective, "//go:%s is a Go directive, and Plutonium has no equivalent; delete it", name)
	d.Text = "Plutonium does not read //go: comments, so this one would do nothing, and no //pu: directive does what " + GoPrefix + name + " does in Go. The directives are: " + strings.Join(Names(), ", ") + "."
	return d
}

func unknown(pos token.Position, name string) *diag.Diagnostic {
	names := Names()
	list := strings.Join(names, ", ")
	if name == "" {
		d := diag.New(pos, diag.UnknownDirective, "//pu: names no directive")
		d.Text = "A comment that starts with //pu: and no space is a directive, and this one has no name after the colon. The directives are: " + list + ".\nA comment that is only a note should start with // and a space."
		return d
	}
	near := diag.Closest(name, names)
	msg := "unknown directive //pu:" + name
	text := "The compiler has no directive named " + name + ", so this comment would do nothing."
	d := diag.New(pos, diag.UnknownDirective, "%s", msg)
	if near != "" {
		d.Msg = msg + " (did you mean //pu:" + near + "?)"
		text += " It is close to " + near + "."
		d.Fix = &diag.Fix{Intro: "Use the spelling the compiler knows", Code: Prefix + near}
	}
	d.Text = text + "\nThe directives are: " + list + ".\nA comment that is only a note should start with // and a space."
	return d
}

func misplaced(pos token.Position, name, want string) *diag.Diagnostic {
	d := diag.New(pos, diag.MisplacedDirective, "//pu:%s is in the wrong place: it goes %s", name, want)
	d.Text = "A directive does something only where the compiler looks for it. //pu:" + name + " goes " + want + "."
	d.Fix = &diag.Fix{Intro: "Move it", Code: Prefix + name + "\n" + exampleDecl(name)}
	return d
}

// exampleDecl is a declaration for a directive's fix.
func exampleDecl(name string) string {
	switch name {
	case "build":
		return "package main"
	case "percpu", "scheduler":
		return "var v T"
	}
	return "func f() { ... }"
}

// article returns word with "a" or "an" before it.
func article(word string) string {
	if strings.ContainsRune("aeiou", rune(word[0])) {
		return "an " + word
	}
	return "a " + word
}

// placeNames describes where a directive can go.
func placeNames(p place) string {
	var parts []string
	if p&atFunc != 0 {
		parts = append(parts, "a function")
	}
	if p&atVar != 0 {
		parts = append(parts, "a package-level variable")
	}
	if p&atFile != 0 {
		parts = append(parts, "a file")
	}
	return strings.Join(parts, " or ")
}
