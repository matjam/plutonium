// Translated by go2pu from internal/diag/catalog_go_plain.go at the go-cutover tag; edited by hand since.

package diag

// goPlain replaces go/types' description of the most common type errors
// with an explanation in plain words, the section of the Plutonium spec
// that has the rule, and a fix. Codes not listed here use go/types' description.
var goPlain = map[string]Entry{
	"InvalidLen": {
		Text:    "len takes a string, an array or a pointer to one, a slice, a map, a channel, an owned, shared or synced array, or a value whose type has a method Len() int, such as a library container or a ring; len(E) of an enum type E is its number of members. A Len method with a pointer receiver needs an operand that is addressable or a pointer, as a method call would.\nPass a value of one of these types, or store the value in a variable first.",
		Spec:    pspec("13.6", "Length and capacity"),
		Example: "println(len(newRing()))",
		Fixed:   "r := newRing()\nprintln(len(r))",
	},
	"InvalidCap": {
		Text:    "cap takes an array or a pointer to one, a slice or a channel. Unlike len, it does not call methods: a library container or a ring reports its capacity with a method of its own.\nCall the type's Cap method instead.",
		Spec:    pspec("13.6", "Length and capacity"),
		Example: "println(cap(r))",
		Fixed:   "println(r.Cap())",
	},
	"BrokenImport": {
		Text: "The imported package could not be loaded. Check the import path: standard library packages are under pluto/, and the program's own packages are under the module path in pluto.mod.",
		Spec: pspec("14.3", "Import declarations"),
	},
	"UnusedImport": {
		Text:    "The file imports a package but never uses it. Plutonium, like Go, treats an unused import as an error, so that a file's imports always show what it really depends on.\nDelete the import, or use the package.",
		Spec:    pspec("14.3", "Import declarations"),
		Example: "import \"pluto/fmt\"\n\nfunc main() {}",
		Fixed:   "func main() {}",
	},
	"InvalidInitCycle": {
		Text: "Package-level variables are initialized in an order where each comes after the ones its initial value uses. Here the values depend on each other in a circle, so no order works. Break the circle by setting one of them in an init function or at run time.",
		Spec: pspec("15.2", "Package initialization"),
	},
	"DuplicateDecl": {
		Text:    "The same name is declared twice in one scope. Each name in a block, or at package level across all the package's files, must be declared once. The message shows where the other declaration is.\nRename one of them, or remove the duplicate.",
		Spec:    pspec("10", "Declarations and scope"),
		Example: "var x = 1\nvar x = 2",
	},
	"InvalidDeclCycle": {
		Text:    "A type, constant or variable is defined in terms of itself, so it has no well-defined size or value. A struct cannot contain itself directly; it can contain a pointer to itself.",
		Spec:    pspec("10.8", "Type declarations"),
		Example: "type Node struct {\n\tnext Node\n}",
		Fixed:   "type Node struct {\n\tnext *Node\n}",
	},
	"InvalidConstInit": {
		Text:    "A constant's value must be known when compiling: a literal, another constant, or an expression of those. A variable or function call is only known at run time. Declare a variable instead.",
		Spec:    pspec("10.6", "Constant declarations"),
		Example: "var n = 4\nconst size = n * 2",
		Fixed:   "const n = 4\nconst size = n * 2",
	},
	"UntypedNilUse": {
		Text:    "nil has no type of its own: it is the zero value of pointers, slices, interfaces and func values. A variable declared with := or var and no type takes the type of its value, and nil gives it none. Write the type.",
		Spec:    pspec("10.10", "Variable declarations"),
		Example: "var p = nil",
		Fixed:   "var p *Node = nil",
	},
	"WrongAssignCount": {
		Text:    "The number of values on the right does not match the number of variables on the left. A call that returns several results must be assigned to that many variables; use _ for results you do not need.\nThe functions of pluto/os that return a name or value from the operating system (DirEntry.Name, Getenv, LookupEnv, Arg, and so on) return an error as well, because the bytes may not be valid UTF-8, which a string must be. Assign both results, or use the form with Lossy in its name to display the value, or RawName and the Bytes forms to get the exact bytes.",
		Spec:    pspec("12.7", "Assignment statements"),
		Example: "n := strconv.Atoi(s)",
		Fixed:   "n, err := strconv.Atoi(s)",
	},
	"UnassignableOperand": {
		Text:    "The left side of an assignment must be something that can hold a value: a variable, a pointer dereference, a field of an addressable struct, or a slice or array element. Constants, function results and string bytes cannot be assigned to.",
		Spec:    pspec("12.7", "Assignment statements"),
		Example: "const limit = 10\nlimit = 20",
	},
	"NoNewVar": {
		Text:    "x := v declares new variables. Every name on the left already exists in this scope, so nothing would be declared. Use = to assign to the existing variables.",
		Spec:    pspec("10.11", "Short variable declarations"),
		Example: "x := 1\nx := 2",
		Fixed:   "x := 1\nx = 2",
	},
	"InvalidIfaceAssign": {
		Text:    "A value can be used as an interface only if its type has every method the interface lists, with the same parameters and results. Types satisfy interfaces without saying so, so a missing method shows up where the value is used.\nThe message names the missing or mismatched method. A method declared on *T belongs to *T, not to T, so you may need to use a pointer.",
		Spec:    pspec("7.9", "Interface types"),
		Example: "type Shape interface{ Area() float64 }\n\ntype Square struct{ side float64 }\n\nvar s Shape = Square{2}",
		Fixed:   "func (q Square) Area() float64 { return q.side * q.side }\n\nvar s Shape = Square{2}",
	},
	"IncompatibleAssign": {
		Text:    "A value of one type is used where a different type is needed: in an assignment, a variable declaration, an argument or a return. Plutonium, like Go, never converts between types by itself, even between int and int64, or between numbers and text.\nConvert explicitly with T(x) between number types, or use a function such as strconv.Atoi to turn text into a number.",
		Spec:    pspec("8.4", "Assignability"),
		Example: "var count int = \"3\"",
		Fixed:   "var count int = 3",
	},
	"NotAType": {
		Text:    "A name used as a type is not a type: it is a variable, constant, function or package. Check the spelling, and whether a local variable hides a type of the same name.",
		Spec:    pspec("7", "Types"),
		Example: "var size = 4\nvar buf size",
	},
	"InvalidArrayLen": {
		Text:    "The length of an array type must be a constant integer that is not negative, since an array's size is fixed when compiling. For a length known only at run time, use a vec.Vec from pluto/vec.",
		Spec:    pspec("7.4", "Array types"),
		Example: "n := 8\nvar buf [n]byte",
		Fixed:   "n := 8\nbuf := vec.Make[byte](n)\ndefer buf.Free()",
	},
	"DuplicateMethod": {
		Text: "A type has two methods with the same name. Method names must be unique for each type, whatever their parameters; there is no overloading.",
		Spec: pspec("10.14", "Method declarations"),
	},
	"InvalidBlank": {
		Text:    "The blank identifier _ can be assigned to, to discard a value, but cannot be read. It is not a variable.",
		Spec:    pspec("10.2", "Blank identifier"),
		Example: "x := _",
	},
	"InvalidMainDecl": {
		Text: "In package main, the name main must be the function where the program starts. It cannot be a variable or type, and it cannot have type parameters.",
		Spec: pspec("15.4", "Program execution"),
	},
	"TooManyValues": {
		Text:    "A function that returns several results is used where one value is expected. Assign the results to variables first, and use the one you need.\nThis includes the functions of pluto/os that return a name or value from the operating system (DirEntry.Name, Getenv, and so on): they return an error as well, because the bytes may not be valid UTF-8. Use the form with Lossy in its name to display the value, or RawName and the Bytes forms to get the exact bytes.",
		Spec:    pspec("11.14", "Calls"),
		Example: "x := strconv.Atoi(s) + 1",
		Fixed:   "n, err := strconv.Atoi(s)\nx := n + 1",
	},
	"NotAnExpr": {
		Text: "Something that is not a value, such as a type or a package name, is used where a value is needed.",
		Spec: pspec("11", "Expressions"),
	},
	"TruncatedFloat": {
		Text:    "A constant with a fraction is used as an integer. Converting it would lose the fraction, which Plutonium, like Go, does not do silently for constants. Use an integer constant, or convert a variable at run time.",
		Spec:    pspec("8.5", "Representability"),
		Example: "var n int = 2.5",
	},
	"NumericOverflow": {
		Text:    "A constant does not fit in the type it is used as. Each integer type holds a fixed range: uint8 holds 0 to 255, int8 holds -128 to 127, and so on. Use a wider type, or a smaller constant.",
		Spec:    pspec("8.5", "Representability"),
		Example: "var b uint8 = 256",
		Fixed:   "var b uint16 = 256",
	},
	"UndefinedOp": {
		Text:    "The operator is not defined for this type. For example, + works on numbers and constant strings, < works on numbers and strings, and ! works only on bools. Structs, slices and funcs have no arithmetic.",
		Spec:    pspec("11.18", "Operators"),
		Example: "var ok bool\nn := ok + 1",
	},
	"MismatchedTypes": {
		Text:    "The two operands of a binary operator, such as + or ==, must have the same type. Plutonium, like Go, never converts one to match the other, even between int and int64.\nConvert one operand to the other's type with T(x).",
		Spec:    pspec("11.19", "Arithmetic operators"),
		Example: "var a int = 1\nvar b int64 = 2\nc := a + b",
		Fixed:   "c := int64(a) + b",
	},
	"DivByZero": {
		Text: "A constant division by zero. Dividing by a constant 0 is always wrong, so the compiler reports it rather than let it panic at run time.",
		Spec: pspec("11.19", "Arithmetic operators"),
	},
	"NonNumericIncDec": {
		Text: "++ and -- work only on numbers.",
		Spec: pspec("12.6", "IncDec statements"),
	},
	"UnaddressableOperand": {
		Text:    "& takes the address of a variable, a field of an addressable struct, a slice element or a composite literal. Other values, such as constants and function results, have no address. Store the value in a variable first.",
		Spec:    pspec("11.22", "Address operators"),
		Example: "p := &f()",
		Fixed:   "v := f()\np := &v",
	},
	"InvalidIndirection": {
		Text: "* follows a pointer to the value it points at. The operand here is not a pointer.",
		Spec: pspec("11.22", "Address operators"),
	},
	"NonIndexableOperand": {
		Text: "Only arrays, slices, strings, pointers to arrays, and generic types whose type set allows it can be indexed with [i].",
		Spec: pspec("11.10", "Index expressions"),
	},
	"InvalidIndex": {
		Text: "An index must be an integer that is not negative, and a constant index must be inside the bounds of an array or constant string.",
		Spec: pspec("11.10", "Index expressions"),
	},
	"InvalidShiftCount": {
		Text: "The count of a shift, the right operand of << or >>, must be an integer that is not negative.",
		Spec: pspec("11.19", "Arithmetic operators"),
	},
	"DuplicateLitKey": {
		Text: "A composite literal gives the same index or key twice.",
		Spec: pspec("11.3", "Composite literals"),
	},
	"MixedStructLit": {
		Text:    "A struct literal either names every field it sets (Point{X: 1, Y: 2}) or lists a value for every field in order (Point{1, 2}). It cannot mix the two.",
		Spec:    pspec("11.3", "Composite literals"),
		Example: "p := Point{X: 1, 2}",
		Fixed:   "p := Point{X: 1, Y: 2}",
	},
	"MissingLitField": {
		Text: "The struct literal names a field that the struct type does not have. Check the spelling; field names are case-sensitive.",
		Spec: pspec("11.3", "Composite literals"),
	},
	"DuplicateLitField": {
		Text: "The struct literal sets the same field twice.",
		Spec: pspec("11.3", "Composite literals"),
	},
	"UnexportedLitField": {
		Text: "The literal sets a field of another package's struct whose name starts with a lower-case letter. Such fields are private to their package. Use a constructor function from that package, if it has one.",
		Spec: pspec("10.4", "Exported identifiers"),
	},
	"UndeclaredImportedName": {
		Text: "The package has no exported name like this. Only names that start with an upper-case letter can be used from other packages. Check the spelling, and the package's documentation for the name it uses.",
		Spec: pspec("11.2", "Qualified identifiers"),
	},
	"UnexportedName": {
		Text: "The name starts with a lower-case letter, so it is private to the package that declares it and cannot be used from another package.",
		Spec: pspec("10.4", "Exported identifiers"),
	},
	"UndeclaredName": {
		Text:    "The name is not declared in any scope that reaches this point. It may be misspelled (names are case-sensitive), declared in a block that has ended, or belong to a package that is not imported.",
		Spec:    pspec("10", "Declarations and scope"),
		Example: "func main() {\n\tcount := 1\n\tprint(cuont)\n}",
	},
	"MissingFieldOrMethod": {
		Text: "The type has no field or method with this name. Check the spelling: names are case-sensitive, and another package's names must start with an upper-case letter. A method declared on *T can be called on a T variable, but not on a T value that has no address.",
		Spec: pspec("11.7", "Selectors"),
	},
	"UncalledBuiltin": {
		Text: "A builtin function such as len or cap is used as a value. Builtins can only be called; wrap the call in a function literal if you need a func value.",
		Spec: pspec("13", "Built-in functions"),
	},
	"InvalidAppend": {
		Text: "append's first argument must be a slice, and the values after it must fit its element type. Plutonium has no append (a slice cannot grow); use pluto/vec.Vec for a sequence that grows.",
		Spec: pspec("13.1", "Built-ins Plutonium does not have"),
	},
	"InvalidAssert": {
		Text: "A type assertion x.(T) asks which concrete type an interface value holds, so x must be an interface. This value is not an interface; its type is already known.",
		Spec: pspec("11.13", "Type assertions"),
	},
	"ImpossibleAssert": {
		Text: "The type in the assertion does not have every method of the interface, so no value of the interface can hold it, and the assertion would always fail.",
		Spec: pspec("11.13", "Type assertions"),
	},
	"InvalidConversion": {
		Text: "This conversion is not allowed. T(x) converts between number types, between types with the same underlying type, and between pointers and unsafe.Pointer. It does not turn numbers into text: string(65) would make a rune, not \"65\".",
		Spec: pspec("11.24", "Conversions"),
	},
	"UnusedExpr": {
		Text:    "The expression computes a value and throws it away, so it has no effect. A statement must do something: call a function, assign, send, and so on. Remove it, or assign the value to _ if computing it is the point.",
		Spec:    pspec("12.4", "Expression statements"),
		Example: "x + 1",
		Fixed:   "x = x + 1",
	},
	"UnusedVar": {
		Text:    "A local variable is declared but its value is never used. Plutonium, like Go, treats this as an error, because it often means a mistake, such as assigning to the wrong variable.\nRemove the variable, use it, or assign it to _ for now.",
		Spec:    pspec("10.10", "Variable declarations"),
		Example: "func main() {\n\tn := compute()\n}",
		Fixed:   "func main() {\n\t_ = compute()\n}",
	},
	"MissingReturn": {
		Text:    "A function that declares results must end every path through its body with a return statement (or a panic, or an endless loop). The compiler found a path that reaches the closing brace without one.\nAdd a return at the end, or a panic if that point can never be reached.",
		Spec:    pspec("12.1", "Terminating statements"),
		Example: "func sign(x int) int {\n\tif x < 0 {\n\t\treturn -1\n\t}\n}",
		Fixed:   "func sign(x int) int {\n\tif x < 0 {\n\t\treturn -1\n\t}\n\treturn 1\n}",
	},
	"WrongResultCount": {
		Text:    "The return statement gives a different number of values than the function declares as results. Return one value for each result, in order.",
		Spec:    pspec("12.14", "Return statements"),
		Example: "func f() (int, error) {\n\treturn 0\n}",
		Fixed:   "func f() (int, error) {\n\treturn 0, nil\n}",
	},
	"InvalidCond": {
		Text:    "The condition of an if or for statement must be a bool. Plutonium, like Go, does not treat numbers or pointers as true or false; compare explicitly.",
		Spec:    pspec("12.8", "If statements"),
		Example: "if n {\n}",
		Fixed:   "if n != 0 {\n}",
	},
	"InvalidRangeExpr": {
		Text: "for range works over arrays, slices, strings, integers, channels, enum types and iterator functions. This value is none of those.",
		Spec: pspec("12.10", "For statements"),
	},
	"MisplacedBreak": {
		Text: "break must be inside a for, switch or select statement.",
		Spec: pspec("12.15", "Break statements"),
	},
	"MisplacedContinue": {
		Text: "continue must be inside a for statement.",
		Spec: pspec("12.16", "Continue statements"),
	},
	"DuplicateCase": {
		Text: "A switch lists the same constant value in two cases. Only the first could ever match.",
		Spec: pspec("12.9", "Switch statements"),
	},
	"UnusedLabel": {
		Text: "A label is declared but no break, continue or goto uses it. Remove it.",
		Spec: pspec("12.3", "Labeled statements"),
	},
	"WrongArgCount": {
		Text:    "The call passes a different number of arguments than the function has parameters. The message shows the arguments given (have) and the parameters (want). Parameters with default values may be left out from the end.",
		Spec:    pspec("11.14", "Calls"),
		Example: "func add(a, b int) int { return a + b }\n\nx := add(1, 2, 3)",
		Fixed:   "x := add(1, 2)",
	},
	"InvalidCall": {
		Text: "Only functions, methods and func values can be called. The expression before the parentheses is something else, or a type used in a way that is not a conversion.",
		Spec: pspec("11.14", "Calls"),
	},
	"UnusedResults": {
		Text: "The result of this call must be used. Builtins such as len and cap, and conversions, have no effect other than their result.",
		Spec: pspec("12.4", "Expression statements"),
	},
	"UnsupportedFeature": {
		Text: "This feature is not available in the language version being compiled.",
		Spec: pspec("21.8", "Language version"),
	},
	"NotAGenericType": {
		Text: "Type arguments in square brackets are given to a type that has no type parameters.",
		Spec: pspec("11.16", "Instantiations"),
	},
	"WrongTypeArgCount": {
		Text: "A generic function or type gets a different number of type arguments than it has type parameters. Give one for each, or leave out the trailing ones that can be inferred from the arguments.",
		Spec: pspec("11.16", "Instantiations"),
	},
	"CannotInferTypeArgs": {
		Text:    "A generic function was called without type arguments, and the compiler could not work them out from the call's arguments. A type parameter that appears only in the results, or only in parameters that were left out, cannot be inferred. Give the type arguments explicitly in square brackets.",
		Spec:    pspec("11.17", "Type inference"),
		Example: "func Zero[T any]() T { var z T; return z }\n\nx := Zero()",
		Fixed:   "x := Zero[int]()",
	},
	"InvalidTypeArg": {
		Text: "A type argument does not satisfy its type parameter's constraint. The constraint is an interface that lists the methods the type needs, or the set of types allowed (such as ~int | ~float64). The message says which part the type fails.",
		Spec: pspec("10.9", "Type parameter declarations"),
	},
	"InvalidExprSwitch": {
		Text: "A case value cannot be compared with the switch value: their types differ, or the type cannot be compared with ==.",
		Spec: pspec("12.9", "Switch statements"),
	},
}
