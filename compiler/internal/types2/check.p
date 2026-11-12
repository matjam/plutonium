// Translated by go2pu from internal/types2/check.go at the go-cutover tag; edited by hand since.

// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements the Check function, which drives type-checking.

package types2

import (
	"compiler/compat"
	"compiler/internal/constant"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	. "compiler/internal/types2/errors"
	"compiler/pu"
	"pluto/fmt"
)

// nopos, noposn indicate an unknown position
var nopos token.Pos
var noposn = atPos(nopos)

// debugging/development support
const debug = false // leave on during development

// exprInfo stores information about an untyped expression.
type exprInfo struct {
	isLhs bool // expression is lhs operand of a shift with delayed type-check
	mode  operandMode
	typ   *Basic
	val   constant.Value // constant value; or nil (if not a constant)
}

// An environment represents the environment within which an object is
// type-checked.
type environment struct {
	decl          *declInfo              // package-level declaration whose init expression/function body is checked
	scope         *Scope                 // top-most scope for lookups
	version       goVersion              // current accepted language version; changes across files
	iota          constant.Value         // value of iota in a constant declaration; nil otherwise
	errpos        positioner             // if set, identifier position of a constant with inherited initializer
	inTParamList  bool                   // set if inside a type parameter list
	sig           *Signature             // function signature if inside a function; nil otherwise
	isPanic       map[*ast.CallExpr]bool // set of panic call expressions (used for termination check)
	hasLabel      bool                   // set if a function makes use of labels (only ~1% of functions); unused outside functions
	hasCallOrRecv bool                   // set if an expression contains a function call or channel receive operation

	// go/types only
	exprPos token.Pos // if valid, identifiers are looked up as if at position pos (used by CheckExpr, Eval)
}

// lookupScope looks up name in the current environment and if an object
// is found it returns the scope containing the object and the object.
// Otherwise it returns (nil, nil).
//
// Note that obj.Parent() may be different from the returned scope if the
// object was inserted into the scope and already had a parent at that
// time (see Scope.Insert). This can only happen for dot-imported objects
// whose parent is the scope of the package that exported them.
func (env *environment) lookupScope(name string) (*Scope, Object) {
	for s := env.scope; s != nil; s = s.parent {
		if obj := s.Lookup(name); obj != nil && (!env.exprPos.IsValid() || cmpPos(obj.scopePos(), env.exprPos) <= 0) {
			return s, obj
		}
	}
	return nil, nil
}

// lookup is like lookupScope but it only returns the object (or nil).
func (env *environment) lookup(name string) Object {
	_, obj := env.lookupScope(name)
	return obj
}

// An importKey identifies an imported package by import path and source directory
// (directory containing the file containing the import). In practice, the directory
// may always be the same, or may not matter. Given an (import path, directory), an
// importer must always return the same package (but given two different import paths,
// an importer may still return the same package by mapping them to the same package
// paths).
type importKey struct {
	path, dir string
}

// A dotImportKey describes a dot-imported object in the given scope.
type dotImportKey struct {
	scope *Scope
	name  string
}

// An action describes a (delayed) action.
type action struct {
	version goVersion   // applicable language version
	f       goFn1       // action to be executed
	desc    *actionDesc // action description; may be nil, requires debug to be set
}

// If debug is set, describef sets a printf-formatted description for action a.
// Otherwise, it is a no-op.
func (a *action) describef(pos positioner, format string, args ...any) {
	_ = a
	_ = pos
	_ = format
	_ = args
}

// An actionDesc provides information on an action.
// For debugging only.
type actionDesc struct {
	pos    positioner
	format string
	args   []any
}

// A Checker maintains the state of the type checker.
// It must be created with [NewChecker].
type Checker struct {
	// package information
	// (initialized by NewChecker, valid for the life-time of checker)
	conf *Config
	ctxt *Context // context for de-duplicating instances
	fset *token.FileSet
	pkg  *Package
	*Info
	nextID   uint64                 // unique Id for type parameters (first valid Id is 1)
	ownerRHS map[*Struct]bool       // struct types that define a named type (plutonium_owner.go)
	objMap   map[Object]*declInfo   // maps package-level objects and (non-interface) methods to declaration info
	objList  []Object               // source-ordered keys of objMap
	impMap   map[importKey]*Package // maps (import path, source directory) to (complete or fake) package
	// see TODO in validtype.go
	// valids instanceLookup // valid *Named (incl. instantiated) types per the validType check

	// pkgPathMap maps package names to the set of distinct import paths we've
	// seen for that name, anywhere in the import graph. It is used for
	// disambiguating package names in error messages.
	//
	// pkgPathMap is allocated lazily, so that we don't pay the price of building
	// it on the happy path. seenPkgMap tracks the packages that we've already
	// walked.
	pkgPathMap map[string]map[string]bool
	seenPkgMap map[*Package]bool

	// information collected during type-checking of a set of package files
	// (initialized by Files, valid only for the duration of check.Files;
	// maps and lists are allocated on demand)
	files         []*ast.File               // package files
	versions      map[*ast.File]string      // maps files to goVersion strings (each file has an entry); shared with Info.FileVersions if present; may be unaltered Config.GoVersion
	imports       []*PkgName                // list of imported packages
	dotImportMap  map[dotImportKey]*PkgName // maps dot-imported objects to the package they were dot-imported through
	brokenAliases map[*TypeName]bool        // set of aliases with broken (not yet determined) types
	unionTypeSets map[*Union]*_TypeSet      // computed type sets for union types
	usedVars      map[*Var]bool             // set of used variables
	usedPkgNames  map[*PkgName]bool         // set of used package names
	mono          monoGraph                 // graph for detecting non-monomorphizable instantiation loops

	firstErr   error                 // first error encountered
	methods    map[*TypeName][]*Func // maps package scope type names to associated non-blank (non-interface) methods
	untyped    map[ast.Expr]exprInfo // map of expressions without final type
	delayed    []action              // stack of delayed action segments; segments are processed in FIFO order
	objPath    []Object              // path of object dependencies during type-checking (for cycle reporting)
	objPathIdx map[Object]int        // map of object to object path index during type-checking (for cycle reporting)
	cleaners   []cleaner             // list of types that may need a final cleanup at the end of type-checking

	// environment within which the current object is type-checked (valid only
	// for the duration of type-checking a specific object)
	environment

	// Plutonium: set by unary & so that the next rawExpr keeps a volatile or
	// aligned wrapper on its operand instead of unwrapping it.
	keepWrapper bool

	// Plutonium: positions of values of complex type, reported together
	// once checking is done (see reportComplex).
	complexAt []token.Pos

	// Plutonium: current nesting of expressions and type expressions, and
	// whether nesting too deep has been reported (see enter).
	nesting   int
	nestingAt bool

	// Plutonium: the expression multiExpr is evaluating in a two-value
	// (comma-ok) context, or nil. A non-constant conversion to an enum
	// type is valid only as that expression.
	commaOkExpr ast.Expr

	// Plutonium: the expression lhsVar is evaluating as the target of an
	// assignment, or nil. An index expression on a container is a call of
	// SetIndex there and of Index elsewhere (see containerIndex).
	lhsExpr ast.Expr

	// Plutonium: the selector callExpr is evaluating as the function of a
	// call, or nil. A pointer-receiver method may be called on a temporary
	// of owner type (spec section 11.14), not otherwise selected on one.
	calleeSel *ast.SelectorExpr

	// Plutonium: the call of the defer or task statement being checked,
	// and its keyword. Its receiver is evaluated when the statement runs,
	// and the call made later, so it cannot be an owner temporary.
	suspended    *ast.CallExpr
	suspendedKey string

	// Plutonium: index expressions that call a container's indexing
	// methods, so that taking their address or assigning to a part of one
	// can be explained (see indexing.go).
	containerIndexes map[ast.Expr]Type // to the container's type

	// Plutonium: selectors of packed fields, so that taking the address of
	// one that is not plain memory is an error (see packed_address.go).
	packedParts map[ast.Expr]packedPart
	// Expressions that denote a variable of a volatile type, before the
	// wrapper is taken off (see packed_address.go).
	volatileExprs map[ast.Expr]bool

	// Plutonium: type switches on union values that name every variant
	// (or cover every tag value of a packed union) without a default
	// case. Such a switch is a terminating statement if every case is.
	exhaustive map[ast.Stmt]bool

	// Plutonium: the declared function or method each identifier names
	// when it is used as a value or callee, so that a call can find the
	// default values of its callee (see calleeFunc).
	funcRefs map[*ast.Ident]*Func

	// Plutonium: stand-ins for parameter names while default values are
	// checked; using one is an error (see defaultValues).
	paramPlaceholders map[*Var]bool

	// debugging
	indent int // indentation for tracing
}

// addDeclDep adds the dependency edge (check.decl -> to) if check.decl exists
func (check *Checker) addDeclDep(to Object) {
	from := check.decl
	if from == nil {
		return // not in a package-level init expression
	}
	if _, found := check.objMap[to]; !found {
		return // to is not a package-level object
	}
	from.addDep(to)
}

func (check *Checker) rememberUntyped(e ast.Expr, lhs bool, mode operandMode, typ *Basic, val constant.Value) {
	m := check.untyped
	if m == nil {
		m = make(map[ast.Expr]exprInfo)
		check.untyped = m
	}
	m[e] = exprInfo{lhs, mode, typ, val}
}

// later pushes f on to the stack of actions that will be processed later;
// either at the end of the current statement, or in case of a local constant
// or variable declaration, before the constant or variable is in scope
// (so that f still sees the scope before any new declarations).
// later returns the pushed action so one can provide a description
// via action.describef for debugging, if desired.
func (check *Checker) later(f goFn1) *action {
	i := len(check.delayed)
	check.delayed = compat.Append(check.delayed, action{version: check.version, f: f})
	return &check.delayed[i]
}

// push pushes obj onto the object path and records its index in the path index map.
func (check *Checker) push(obj Object) {
	if check.objPathIdx == nil {
		check.objPathIdx = make(map[Object]int)
	}
	check.objPathIdx[obj] = len(check.objPath)
	check.objPath = compat.Append(check.objPath, obj)
}

// pop pops an object from the object path and removes it from the path index map.
func (check *Checker) pop() {
	i := len(check.objPath) - 1
	obj := check.objPath[i]
	check.objPath[i] = nil // help the garbage collector
	check.objPath = check.objPath[:i]
	delete(check.objPathIdx, obj)
}

type cleaner interface {
	cleanup()
}

// needsCleanup records objects/types that implement the cleanup method
// which will be called at the end of type-checking.
func (check *Checker) needsCleanup(c cleaner) {
	check.cleaners = compat.Append(check.cleaners, c)
}

// NewChecker returns a new [Checker] instance for a given package.
// [Package] files may be added incrementally via checker.Files.
func NewChecker(conf *Config, fset *token.FileSet, pkg *Package, info *Info) *Checker {
	// make sure we have a configuration
	if conf == nil {
		conf = pu.Alloc[Config]()
	}

	// make sure we have an info struct
	if info == nil {
		info = pu.Alloc[Info]()
	}

	// Note: clients may call NewChecker with the Unsafe package, which is
	// globally shared and must not be mutated. Therefore NewChecker must not
	// mutate *pkg.
	//
	// (previously, pkg.goVersion was mutated here: go.dev/issue/61212)

	return pu.New(Checker{
		conf:         conf,
		ctxt:         conf.Context,
		fset:         fset,
		pkg:          pkg,
		Info:         info,
		objMap:       make(map[Object]*declInfo),
		impMap:       make(map[importKey]*Package),
		usedVars:     make(map[*Var]bool),
		usedPkgNames: make(map[*PkgName]bool),
	})
}

// initFiles initializes the files-specific portion of checker.
// The provided files must all belong to the same package.
func (check *Checker) initFiles(files []*ast.File) {
	// start with a clean slate (check.Files may be called multiple times)
	// TODO(gri): what determines which fields are zeroed out here, vs at the end
	// of checkFiles?
	check.files = nil
	check.imports = nil
	check.dotImportMap = nil

	check.firstErr = nil
	check.methods = nil
	check.untyped = nil
	check.delayed = nil
	check.objPath = nil
	check.objPathIdx = nil
	check.cleaners = nil

	// We must initialize usedVars and usedPkgNames both here and in NewChecker,
	// because initFiles is not called in the CheckExpr or Eval codepaths, yet we
	// want to free this memory at the end of Files ('used' predicates are
	// only needed in the context of a given file).
	check.usedVars = make(map[*Var]bool)
	check.usedPkgNames = make(map[*PkgName]bool)

	// determine package name and collect valid files
	pkg := check.pkg
	for _, file := range files {
		switch name := file.Name.Name; pkg.name {
		case "":
			if name != "_" {
				pkg.name = name
			} else {
				check.error(file.Name, BlankPkgName, "invalid package name _")
			}
			fallthrough

		case name:
			check.files = compat.Append(check.files, file)

		default:
			// The loader groups files by directory and reports a mismatched
			// package clause (E4xxx) before type checking starts.
			check.errorf(atPos(file.Package), InvalidSyntaxTree, "package %s; expected package %s", pu.Box(name), pu.Box(pkg.name))
			// ignore this file
		}
	}

	// reuse Info.FileVersions if provided
	versions := check.FileVersions
	if versions == nil {
		versions = make(map[*ast.File]string)
	}
	check.versions = versions

	// Plutonium has one language version, so a file's //go:build line (itself
	// an error, E2604) and Config.GoVersion never limit the features a file may
	// use. Every file gets the unaltered Config.GoVersion, which is empty, and
	// allowVersion then allows every feature.
	for _, file := range check.files {
		versions[file] = check.conf.GoVersion
	}
}

// Files checks the provided files as part of the checker's package.
func (check *Checker) Files(files []*ast.File) (err error) {
	if check.pkg == Unsafe {
		// Defensive handling for Unsafe, which cannot be type checked, and must
		// not be mutated. See https://go.dev/issue/61212 for an example of where
		// Unsafe is passed to NewChecker.
		return nil
	}

	// Avoid early returns here! Nearly all errors can be
	// localized to a piece of syntax and needn't prevent
	// type-checking of the rest of the package.

	check.checkFiles(files)
	return check.firstErr
}

// checkFiles type-checks the specified files. Errors are reported as
// a side effect, not by returning early, to ensure that well-formed
// syntax is properly type annotated even in a package containing
// errors.
func (check *Checker) checkFiles(files []*ast.File) {
	print := func(msg string) {
		if check.conf._Trace {
			fmt.Println()
			compat.Printfb("%v\n", compat.ArgS(msg))
		}
	}

	print("== initFiles ==")
	check.initFiles(files)

	print("== collectObjects ==")
	check.collectObjects()

	print("== sortObjects ==")
	check.sortObjects()

	print("== directCycles ==")
	check.directCycles()

	print("== packageObjects ==")
	check.packageObjects()

	print("== processDelayed ==")
	check.processDelayed(0) // incl. all functions

	check.ownerMethods()

	print("== cleanup ==")
	check.cleanup()

	print("== initOrder ==")
	check.initOrder()

	if !check.conf.DisableUnusedImportCheck {
		print("== unusedImports ==")
		check.unusedImports()
	}

	print("== recordUntyped ==")
	check.recordUntyped()

	print("== reportComplex ==")
	check.reportComplex()

	if check.firstErr == nil {
		// TODO(mdempsky): Ensure monomorph is safe when errors exist.
		check.monomorph()
	}

	check.pkg.goVersion = check.conf.GoVersion
	check.pkg.complete = true

	// no longer needed - release memory
	check.imports = nil
	check.dotImportMap = nil
	check.pkgPathMap = nil
	check.seenPkgMap = nil
	check.brokenAliases = nil
	check.unionTypeSets = nil
	check.usedVars = nil
	check.usedPkgNames = nil
	check.ctxt = nil

	// TODO(gri): shouldn't the cleanup above occur after the bailout?
	// TODO(gri) There's more memory we should release at this point.
}

// processDelayed processes all delayed actions pushed after top.
func (check *Checker) processDelayed(top int) {
	// If each delayed action pushes a new action, the
	// stack will continue to grow during this loop.
	// However, it is only processing functions (which
	// are processed in a delayed fashion) that may
	// add more actions (such as nested functions), so
	// this is a sufficiently bounded process.
	savedVersion := check.version
	for i := top; i < len(check.delayed); i++ {
		a := &check.delayed[i]
		if check.conf._Trace {
			if a.desc != nil {
				check.trace(a.desc.pos.Pos(), compat.Concat("-- ", a.desc.format), a.desc.args...)
			} else {
				check.trace(nopos, "-- delayed")
			}
		}
		check.version = a.version // reestablish the effective Go version captured earlier
		a.f.Call()                // may append to check.delayed

		if check.conf._Trace {
			fmt.Println()
		}
	}
	assert(top <= len(check.delayed)) // stack must not have shrunk
	check.delayed = check.delayed[:top]
	check.version = savedVersion
}

// cleanup runs cleanup for all collected cleaners.
func (check *Checker) cleanup() {
	// Don't use a range clause since Named.cleanup may add more cleaners.
	for i := 0; i < len(check.cleaners); i++ {
		check.cleaners[i].cleanup()
	}
	check.cleaners = nil
}

// go/types doesn't support recording of types directly in the AST.
// dummy function to match types2 code.
func (check *Checker) recordTypeAndValueInSyntax(x ast.Expr, mode operandMode, typ Type, val constant.Value) {
	// nothing to do
}

// go/types doesn't support recording of types directly in the AST.
// dummy function to match types2 code.
func (check *Checker) recordCommaOkTypesInSyntax(x ast.Expr, t0, t1 Type) {
	// nothing to do
}

// instantiatedIdent determines the identifier of the type instantiated in expr.
// Helper function for recordInstance in recording.go.
func instantiatedIdent(expr ast.Expr) *ast.Ident {
	var selOrIdent ast.Expr
	switch e := expr.(type) {
	case *ast.IndexExpr:
		selOrIdent = e.X
	case *ast.IndexListExpr: // only exists in go/ast, not syntax
		selOrIdent = e.X
	case *ast.SelectorExpr, *ast.Ident:
		selOrIdent = e
	}
	switch x := selOrIdent.(type) {
	case *ast.Ident:
		return x
	case *ast.SelectorExpr:
		return x.Sel
	}

	// extra debugging of go.dev/issue/63933
	panic(pu.Box(sprintf(nil, nil, true, "instantiated ident not found; please report: %s", expr)))
}

// goFn1 stands for func(), which a closure that outlives its frame cannot be
// (go2pu closure conversion): the closure is a struct with this method.
type goFn1 interface {
	Call()
}
