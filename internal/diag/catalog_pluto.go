package diag

import typerr "github.com/matjam/plutonium/internal/types2/errors"

// The explanations of the rules Plutonium adds to Go or changes. Each says
// why Plutonium differs, in Why.

// pspec names a section of the Plutonium spec, docs/spec/language.md.
func pspec(section, title string) string {
	return "\"" + title + "\" in the Plutonium spec, section " + section
}

// whyNoGC is the reason shared by the constructs Plutonium removes.
const whyNoGC = "Plutonium has no garbage collector and never allocates memory unless the program asks for it in the code, so that every cost is visible and the same code can run inside a kernel."

func init() {
	register(
		&Entry{
			Code: CheckerCode(typerr.MapNotPermitted), Name: "MapNotPermitted",
			Text: "Plutonium has no built-in map type. A Go map grows by allocating memory behind the program's back, and the old memory is freed by the garbage collector.\nUse a hash map from pluto/hash, which takes an allocator, so you can see and control where its memory comes from.",
			Why:  whyNoGC,
			Fix:     &Fix{Intro: "To use a hash map", Code: "import \"pluto/hash\"\n\nm := hash.NewMap[string, int](0)\ndefer m.Free()\nm.Set(\"a\", 1)"},
			Spec:    pspec("3", "What Plutonium removes from Go"),
			Example: "counts := map[string]int{}\ncounts[\"a\"]++",
			Fixed:   "counts := hash.NewMap[string, int](0)\ndefer counts.Free()\np, _ := counts.Upsert(\"a\")\n*p++",
		},
		&Entry{
			Code: CheckerCode(typerr.ChanNotPermitted), Name: "ChanNotPermitted",
			Text: "This build of the compiler does not accept channel types or channel operations (send, receive and select). Channels need a scheduler to block and wake puroutines.",
			Why:  "Plutonium carries a scheduler only in programs that ask for one, so that a kernel or a small program has no hidden runtime.",
			Spec: pspec("4.8", "Channels"),
		},
		&Entry{
			Code: CheckerCode(typerr.GoNotPermitted), Name: "GoNotPermitted",
			Text: "This build of the compiler does not accept go statements. A go statement starts a puroutine, which needs a scheduler.",
			Why:  "Plutonium carries a scheduler only in programs that ask for one, so that a kernel or a small program has no hidden runtime.",
			Spec: pspec("4.7", "go starts a puroutine"),
		},
		&Entry{
			Code: CheckerCode(typerr.RecoverNotPermitted), Name: "RecoverNotPermitted",
			Text: "In Plutonium, panic stops the program. It does not unwind the stack, and deferred calls do not run, so there is nothing for recover to catch.\nReport failures that the caller can handle by returning an error instead.",
			Why:  "Unwinding needs tables and runtime support that a kernel cannot rely on, and a panic in kernel code means the state is already wrong.",
			Fix:  &Fix{Intro: "Return an error instead", Code: "func parse(s string) (int, error) {\n\tif s == \"\" {\n\t\treturn 0, errors.New(\"empty input\")\n\t}\n\t...\n}"},
			Spec: pspec("4.1", "panic aborts"),
			Example: "defer func() {\n\tif r := recover(); r != nil {\n\t\tlog(r)\n\t}\n}()",
		},
		&Entry{
			Code: CheckerCode(typerr.ReflectNotPermitted), Name: "ReflectNotPermitted",
			Text: "Plutonium has no reflect package. Reflection needs a description of every type in the running program.\nUse a type switch, or generics, to handle values of several types.",
			Why:  "Plutonium keeps type descriptors only for the types a program converts to an interface, so binaries stay small enough for a kernel.",
			Spec: pspec("3", "What Plutonium removes from Go"),
			Example: "import \"reflect\"",
			Fixed:   "switch v := x.(type) {\ncase int:\n\t...\ncase string:\n\t...\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.ComplexNotPermitted), Name: "ComplexNotPermitted",
			Text: "Plutonium has no complex number types (complex64 and complex128) and no complex, real or imag builtins.\nUse a struct of two floats for the real and imaginary parts.",
			Why:  "Complex numbers are rarely useful in systems code, and leaving them out keeps the code generator small.",
			Fix:  &Fix{Intro: "Use a struct", Code: "type Complex struct{ Re, Im float64 }"},
			Spec: pspec("3", "What Plutonium removes from Go"),
			Example: "var z complex128 = 1 + 2i",
		},
		&Entry{
			Code: CheckerCode(typerr.MissingAllocatorType), Name: "MissingAllocatorType",
			Text: "new and make take an optional allocator as their last argument. Its type, Allocator, is declared in package pluto/rt, which this build could not find. This happens only when the standard library is missing or incomplete.\nCheck that PLUTOROOT, if set, points at a directory that contains pluto/rt.",
			Spec: pspec("4.2", "new and make allocate from an allocator"),
		},
		&Entry{
			Code: CheckerCode(typerr.ErrorsNewNotConstant), Name: "ErrorsNewNotConstant",
			Text: "errors.New takes only a constant message, such as a string literal. The compiler makes one error value for each place errors.New is called, and stores it in the program image, so the message must be known when compiling.\nTo build a message from values at run time, use fmt.Errorf, which allocates the error.",
			Why:  "In Go, errors.New allocates a new error on every call. Plutonium never allocates behind the program's back, so errors.New makes a static error instead, which costs nothing and can never fail.",
			Fix:  &Fix{Intro: "To include values in the message", Code: "return fmt.Errorf(\"open %s: not found\", name)"},
			Spec: pspec("13.1", "Sentinels and errors.New"),
			Example: "return errors.New(\"open \" + name)",
			Fixed:   "return fmt.Errorf(\"open %s\", name)",
		},
		&Entry{
			Code: CheckerCode(typerr.BadMainSignature), Name: "BadMainSignature",
			Text: "func main takes no arguments. It may return nothing, as in Go, or one int64, which becomes the program's exit status.\nRead command-line arguments with pluto/os instead of parameters.",
			Fix:  &Fix{Intro: "Either form works", Code: "func main() { ... }\nfunc main() int64 { return 0 }"},
			Spec: pspec("2", "Source files and packages"),
		},

		// Packed types and wrappers.
		&Entry{
			Code: CheckerCode(typerr.BadPackedBacking), Name: "BadPackedBacking",
			Text: "The backing type of a packed type says how its bits are stored. It must be an unsigned or signed integer type (uint32), an array of bytes ([8]byte), or []byte. It can also be left out.",
			Spec: pspec("5.5", "packed types"),
			Example: "type R packed float64 { a bits[uint, 4] }",
			Fixed:   "type R packed uint64 { a bits[uint, 4] }",
		},
		&Entry{
			Code: CheckerCode(typerr.PackedTooLarge), Name: "PackedTooLarge",
			Text: "The fields of a packed type are laid out one after another, from bit 0. Their widths add up to more bits than the backing type holds.\nUse a larger backing type, make fields narrower, or leave the backing type out so the size follows the fields.",
			Spec: pspec("5.5", "packed types"),
			Example: "type R packed uint8 {\n\ta bits[uint, 6]\n\tb bits[uint, 4]\n}",
			Fixed:   "type R packed uint16 {\n\ta bits[uint, 6]\n\tb bits[uint, 4]\n\t_ bits[uint, 6]\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.BadBitField), Name: "BadBitField",
			Text: "A bit field is written bits[T, N]: N bits read as type T. T must be an integer type at least N bits wide, an enum, or bool with N equal to 1.",
			Spec: pspec("5.5", "packed types"),
			Example: "mode bits[uint8, 12]",
			Fixed:   "mode bits[uint16, 12]",
		},
		&Entry{
			Code: CheckerCode(typerr.BadPackedField), Name: "BadPackedField",
			Text: "A field of a packed type must have a fixed bit layout: an integer, bool, float, packed type, or an array of those. Strings, slices, pointers and structs have no fixed layout in bits.",
			Spec: pspec("5.5", "packed types"),
		},
		&Entry{
			Code: CheckerCode(typerr.BadPackedLiteral), Name: "BadPackedLiteral",
			Text: "A packed literal must name each field it sets, as field: value. The fields of a packed type have no order a literal could follow, since reserved bits sit between them.",
			Spec:    pspec("5.5", "packed types"),
			Example: "r := ControlReg{5, true}",
			Fixed:   "r := ControlReg{mode: 5, enable: true}",
		},
		&Entry{
			Code: CheckerCode(typerr.WrongWrapperArgs), Name: "WrongWrapperArgs",
			Text: "volatile takes one type argument: volatile[T]. aligned and ring take a type and a constant: aligned[T, N] and ring[T, N].",
			Spec: pspec("5.2", "volatile[T]"),
		},
		&Entry{
			Code: CheckerCode(typerr.NotPowerOfTwo), Name: "NotPowerOfTwo",
			Text: "The alignment of aligned[T, N] and the size of ring[T, N] must be a power of two: 1, 2, 4, 8, 16 and so on. Hardware alignment is always a power of two, and a ring of that size finds a slot with a mask instead of a division.",
			Spec: pspec("5.3", "aligned[T, N]"),
			Example: "var buf ring[byte, 100]",
			Fixed:   "var buf ring[byte, 128]",
		},
		&Entry{
			Code: CheckerCode(typerr.BadVolatileElem), Name: "BadVolatileElem",
			Text: "volatile[T] makes every read and write of T exactly one load or store. That is only possible for a value the processor moves in one instruction: an integer, a bool, a pointer, or a packed type backed by an integer.",
			Spec: pspec("5.2", "volatile[T]"),
			Example: "var status volatile[[4]uint32]",
			Fixed:   "var status [4]volatile[uint32]",
		},
		&Entry{
			Code: CheckerCode(typerr.BadWrapperSize), Name: "BadWrapperSize",
			Text: "The second argument of aligned[T, N] and ring[T, N] must be a positive integer constant, since the layout is fixed when compiling.",
			Spec: pspec("5.3", "aligned[T, N]"),
		},
		&Entry{
			Code: CheckerCode(typerr.RingLiteralNotEmpty), Name: "RingLiteralNotEmpty",
			Text: "A ring literal cannot list elements. Write an empty literal, or use the zero value, and add elements with Push.",
			Spec: pspec("5.4", "ring[T, N]"),
			Example: "r := ring[int, 4]{1, 2}",
			Fixed:   "var r ring[int, 4]\nr.Push(1)\nr.Push(2)",
		},

		// Default parameter values.
		&Entry{
			Code: CheckerCode(typerr.MisplacedDefault), Name: "MisplacedDefault",
			Text: "Default parameter values are allowed only on the parameters of a declared function or method. Receivers and results cannot have them, and neither can function types, function literals or interface methods, because a call through a function value or an interface does not know which function it calls, so it could not fill in the defaults.",
			Spec: pspec("5.1", "Default parameter values"),
			Example: "type Grower func(n int, factor int = 2) int",
		},
		&Entry{
			Code: CheckerCode(typerr.VariadicDefault), Name: "VariadicDefault",
			Text: "A variadic function (one whose last parameter is ...T) cannot have default values. An argument after the variadic ones could not be told apart from them, and a default before them would have to be given whenever any variadic argument is.\nVariadic functions in the standard library allocate from alloc.Default instead; alloc.Use replaces it for a region of code.",
			Spec: pspec("5.1", "Default parameter values"),
			Example: "func Logf(format string, args ...any, w io.Writer = os.Stderr)",
		},
		&Entry{
			Code: CheckerCode(typerr.MissingDefault), Name: "MissingDefault",
			Text: "A call may leave out only trailing parameters. So once a parameter has a default value, every parameter after it needs one too.",
			Spec:    pspec("5.1", "Default parameter values"),
			Example: "func Grow(n int, factor int = 2, limit int) int",
			Fixed:   "func Grow(n int, limit int, factor int = 2) int",
		},
		&Entry{
			Code: CheckerCode(typerr.SharedDefault), Name: "SharedDefault",
			Text: "A parameter with a default value needs a name and type of its own. In a, b int = 1 it is unclear whether the default belongs to a, to b, or to both.",
			Spec:    pspec("5.1", "Default parameter values"),
			Example: "func f(a, b int = 1)",
			Fixed:   "func f(a int = 1, b int = 1)",
		},
		&Entry{
			Code: CheckerCode(typerr.DefaultRefersToParam), Name: "DefaultRefersToParam",
			Text: "A default value is evaluated by the caller, in the scope where the function is declared, before the function runs. The function's parameters do not exist yet at that point, so a default cannot use them.\nDefault to a sentinel value instead, and compute the real value in the body.",
			Spec:    pspec("5.1", "Default parameter values"),
			Example: "func Fill(buf []byte, n int = len(buf))",
			Fixed:   "func Fill(buf []byte, n int = -1) {\n\tif n < 0 {\n\t\tn = len(buf)\n\t}\n\t...\n}",
		},

		// Enums.
		&Entry{
			Code: CheckerCode(typerr.MisplacedEnum), Name: "MisplacedEnum",
			Text: "An enum type can only be the type of a type definition, type E enum { ... }. It cannot be written inline in a variable or field, or be an alias, because its members are reached through its name (E.Member).",
			Spec:    pspec("5.9", "Enums"),
			Example: "var c enum { Red; Green }",
			Fixed:   "type Color enum { Red; Green }\n\nvar c Color",
		},
		&Entry{
			Code: CheckerCode(typerr.GenericEnum), Name: "GenericEnum",
			Text: "An enum type cannot have type parameters. Its members are constants of one fixed integer type.",
			Spec: pspec("5.9", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.BadEnumBase), Name: "BadEnumBase",
			Text: "The type after the enum keyword is the integer type that holds the enum's values. It must be an integer type such as uint8 or int32. Leave it out to let the compiler pick the smallest one that fits.",
			Spec:    pspec("5.9", "Enums"),
			Example: "type Mode enum string { Read; Write }",
			Fixed:   "type Mode enum { Read; Write }",
		},
		&Entry{
			Code: CheckerCode(typerr.EmptyEnum), Name: "EmptyEnum",
			Text: "An enum needs at least one member, and one member must have the value 0, since that is the zero value of the type.",
			Spec: pspec("5.9", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.BlankEnumMember), Name: "BlankEnumMember",
			Text: "Every member of an enum needs a name. To skip a value, give the next member an explicit value instead.",
			Spec:    pspec("5.9", "Enums"),
			Example: "type E enum { A; _; C }",
			Fixed:   "type E enum { A; C = 2 }",
		},
		&Entry{
			Code: CheckerCode(typerr.DuplicateEnumValue), Name: "DuplicateEnumValue",
			Text: "Two members of an enum have the same value. Each value must belong to one member, so that converting a number to the enum, and printing a value's name, give one answer.\nA member without a value is one more than the member before it, which is often how two members end up equal.",
			Spec:    pspec("5.9", "Enums"),
			Example: "type E enum {\n\tA = 1\n\tB\n\tC = 2\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.EnumWithoutZero), Name: "EnumWithoutZero",
			Text: "A variable of an enum type starts as 0, like every Plutonium variable. So one member must have the value 0, or a fresh variable would hold a value that is not a member.\nAdd a member for the zero value, often named None, Unknown or Invalid.",
			Spec:    pspec("5.9", "Enums"),
			Example: "type Level enum {\n\tLow = 1\n\tHigh\n}",
			Fixed:   "type Level enum {\n\tUnset\n\tLow\n\tHigh\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.EnumOverflow), Name: "EnumOverflow",
			Text: "A member's value does not fit in the enum's integer type. Pick a wider type after the enum keyword, or leave the type out so the compiler picks one that fits.",
			Spec:    pspec("5.9", "Enums"),
			Example: "type E enum uint8 { A; B = 300 }",
		},
		&Entry{
			Code: CheckerCode(typerr.BadEnumValue), Name: "BadEnumValue",
			Text: "The value of an enum member must be a constant integer expression, such as 5, 1 << 3 or another constant, since members are constants fixed when compiling.",
			Spec:    pspec("5.9", "Enums"),
			Example: "type E enum { A = 1.5 }",
		},
		&Entry{
			Code: CheckerCode(typerr.EnumSelfReference), Name: "EnumSelfReference",
			Text: "An enum's member values cannot use the enum being declared, because those values are what decides the enum's integer type.",
			Spec: pspec("5.9", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.UnexportedEnumMember), Name: "UnexportedEnumMember",
			Text: "Enum members follow Go's rule for names: a member whose name starts with a lower-case letter is visible only inside its own package.",
			Spec: pspec("5.9", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.MissingEnumCase), Name: "MissingEnumCase",
			Text: "A switch on an enum value that has no default case must list every member. That way, adding a member to the enum shows every switch that needs to handle it.\nAdd a case for each missing member, or a default case if the remaining members share one action.",
			Why:  "Go does not check this. Plutonium does, because an enum is a closed set of values, so the compiler can tell when one is forgotten.",
			Spec: pspec("5.9", "Enums"),
			Example: "switch c {\ncase Color.Red:\n\t...\n}",
			Fixed:   "switch c {\ncase Color.Red:\n\t...\ndefault:\n\t...\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.BadEnumConversion), Name: "BadEnumConversion",
			Text: "Only integers convert to an enum, and a constant converts only if it is the value of a member. Anything else would make an enum value that is not a member.",
			Spec:    pspec("5.9", "Enums"),
			Example: "c := Color(7) // Color has no member with value 7",
		},
		&Entry{
			Code: CheckerCode(typerr.UncheckedEnumConversion), Name: "UncheckedEnumConversion",
			Text: "A number that is not a constant may not be the value of any member, so converting it to an enum needs a check. Use the comma-ok form: ok reports whether the number is a member's value.",
			Why:  "Go allows any integer in a named integer type. Plutonium enums are closed sets, so a value outside the set is caught where it enters, not later in a switch.",
			Fix:  &Fix{Intro: "Use the comma-ok form", Code: "c, ok := Color(n)\nif !ok {\n\treturn errBadColor\n}"},
			Spec: pspec("5.9", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.NotEnumType), Name: "NotEnumType",
			Text: "This operation needs an enum type, such as len(E) or for v := range E, and the type given is not an enum.",
			Spec: pspec("5.9", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.EnumArithmetic), Name: "EnumArithmetic",
			Text: "Enum values can be compared, but not added, multiplied, shifted or combined with bitwise operators, since the result would usually not be a member. Convert to an integer to do arithmetic.",
			Fix:  &Fix{Intro: "Convert to an integer first", Code: "next := int(c) + 1"},
			Spec: pspec("5.9", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.RangeOverEnumValue), Name: "RangeOverEnumValue",
			Text: "To loop over the members of an enum, range over the enum type itself, not over a value of it.",
			Fix:  &Fix{Intro: "Range over the type", Code: "for c := range Color {\n\t...\n}"},
			Spec: pspec("5.9", "Enums"),
		},

		// Rules checked while compiling.
		&Entry{
			Code: NotYetSupported, Name: "NotYetSupported",
			Text: "The program is valid, but this compiler cannot compile this construct yet. It stops with an error rather than produce wrong code.\nRewrite the code without the construct, or check whether a newer compiler supports it.",
			Spec: "the Plutonium spec, docs/spec/language.md",
		},
		&Entry{
			Code: ExternWithBody, Name: "ExternWithBody",
			Text: "//pu:extern says that the function is written in C or assembly and linked in under the given symbol. A function with that directive must be declared without a body.",
			Spec:    pspec("9", "Assembly and C"),
			Example: "//pu:extern strlen\nfunc strlen(s *byte) uint64 { return 0 }",
			Fixed:   "//pu:extern strlen\nfunc strlen(s *byte) uint64",
		},
		&Entry{
			Code: BadCSignature, Name: "BadCSignature",
			Text: "Functions marked //pu:extern or //pu:export are called with the C calling convention. Their parameters and results must be types C has: integers, floats, bools, pointers, and structs and arrays of those. Strings, slices, interfaces and func values have no C equivalent, and variadic functions are not supported.\nPass a pointer and a length instead of a string or slice.",
			Fix:  &Fix{Intro: "Pass a pointer and a length", Code: "//pu:extern write\nfunc write(fd int32, buf *byte, n uint64) int64\n\nwrite(1, unsafe.SliceData(b), uint64(len(b)))"},
			Spec: pspec("9", "Assembly and C"),
		},
		&Entry{
			Code: DuplicateExport, Name: "DuplicateExport",
			Text: "Two functions are exported under the same symbol name with //pu:export, or the name is already used by another symbol. The linker needs each symbol name to be unique.",
			Spec: pspec("9", "Assembly and C"),
		},
		&Entry{
			Code: GenericExport, Name: "GenericExport",
			Text: "A generic function is compiled once for each set of type arguments it is used with, so there is no single function to export under one C symbol. Export a non-generic wrapper that calls it with the types you need.",
			Fix:  &Fix{Intro: "Export a wrapper", Code: "//pu:export sum_ints\nfunc sumInts(p *int, n int) int { return Sum[int](unsafe.Slice(p, n)) }"},
			Spec: pspec("9", "Assembly and C"),
		},
		&Entry{
			Code: KernelFloat, Name: "KernelFloat",
			Text: "In kernel mode, functions may not use floating point. When an interrupt or system call enters the kernel, the processor does not save the floating-point registers, so kernel code that used them would corrupt the state of the program it interrupted.\nFunctions marked //pu:fpu may use floats. Whoever calls one must save the floating-point state first, with x86.FPUBegin and x86.FPUEnd.\nCopying a struct that contains float fields is allowed, since it uses only integer registers.",
			Why:  "Go has no kernel mode. Plutonium rejects floats here, rather than emulate them in software, so that no hidden cost or library ends up in the kernel.",
			Fix:  &Fix{Intro: "Mark the function, and save the state around calls", Code: "//pu:fpu\nfunc scale(x float64) float64 { return x * 1.5 }\n\nx86.FPUBegin(&area)\ny := scale(x)\nx86.FPUEnd(&area)"},
			Spec: pspec("11.3", "Floating point in kernel mode"),
		},
		&Entry{
			Code: DeferInLoop, Name: "DeferInLoop",
			Text: "A defer statement inside a loop would add a deferred call on every iteration, and each needs room in the function's stack frame until it returns. Plutonium gives every deferred call a fixed slot in the frame, so the number must be known when compiling.\nMove the loop body into a function of its own; its defer then runs at the end of each iteration.",
			Why:  "Go stores such deferred calls on the heap. Plutonium never allocates behind the program's back, and kernel stacks are small and fixed.",
			Fix:  &Fix{Intro: "Move the body into a function", Code: "for _, name := range names {\n\tprocess(name)\n}\n\nfunc process(name string) {\n\tf := open(name)\n\tdefer f.Close()\n\t...\n}"},
			Spec: "\"Defer statements\" in the Go spec; Plutonium adds this limit",
			Example: "for _, name := range names {\n\tf := open(name)\n\tdefer f.Close()\n}",
		},
		&Entry{
			Code: TooManyDefers, Name: "TooManyDefers",
			Text: "A function may contain at most 64 defer statements. Each has a fixed slot in the stack frame. Split the function into smaller ones.",
			Spec: "\"Defer statements\" in the Go spec; Plutonium adds this limit",
		},
		&Entry{
			Code: DeferBuiltin, Name: "DeferBuiltin",
			Text: "This builtin cannot be deferred directly. Wrap the call in a function literal and defer that.",
			Fix:  &Fix{Intro: "Defer a function literal", Code: "defer func() { copy(dst, src) }()"},
			Spec: "\"Defer statements\" in the Go spec",
		},
		&Entry{
			Code: DeferInRangeFunc, Name: "DeferInRangeFunc",
			Text: "The body of a loop over a function (for x := range seq) runs once for each value the iterator yields, so a defer statement in it is a defer inside a loop. Its deferred call belongs to the function the loop is in and runs when that function returns, as in Go. Plutonium gives each defer statement one slot in that function's frame, so the defer may run at most once per call: after it, every path must leave the loop, by return, by panic, or by break, continue or goto to a statement outside the loop.\nIf the deferred call should run at the end of each iteration, move the body into a function of its own.",
			Why:  "Go keeps the deferred calls of such a loop in a list on the heap. Plutonium never allocates behind the program's back, and kernel stacks are small and fixed.",
			Fix:  &Fix{Intro: "Leave the loop after the defer, or give the body a function of its own", Code: "for item := range items.All() {\n\tif item.ID == id {\n\t\titem.mu.Lock()\n\t\tdefer item.mu.Unlock()\n\t\treturn item.Value\n\t}\n}"},
			Spec: pspec("4.10", "Range over functions"),
			Example: "for item := range items.All() {\n\titem.mu.Lock()\n\tdefer item.mu.Unlock()\n\tupdate(item)\n}",
			Fixed:   "for item := range items.All() {\n\tupdateLocked(item)\n}\n\nfunc updateLocked(item *Item) {\n\titem.mu.Lock()\n\tdefer item.mu.Unlock()\n\tupdate(item)\n}",
		},
		&Entry{
			Code: DeferLitCapture, Name: "DeferLitCapture",
			Text: "A function literal deferred in the body of a loop over a function runs when the enclosing function returns, after the loop body has returned. The body's own variables, including the loop variables, live in the body's frame, which is gone by then, so the literal cannot capture them.\nPass the values the deferred call needs as arguments: arguments are evaluated when the defer statement runs.",
			Why:  "Go moves such variables to the heap. Plutonium never moves a variable to the heap; a closure is valid only while the frame it captures from is.",
			Fix:  &Fix{Intro: "Pass the loop variable as an argument", Code: "for x := range seq {\n\tif done(x) {\n\t\tdefer func(x int) { log(x) }(x)\n\t\treturn\n\t}\n}"},
			Spec: pspec("4.10", "Range over functions"),
			Example: "for x := range seq {\n\tif done(x) {\n\t\tdefer func() { log(x) }()\n\t\treturn\n\t}\n}",
		},
		&Entry{
			Code: AllocatingConv, Name: "AllocatingConv",
			Text: "Converting between string and []byte (or []rune), or making a string from a rune that is not a constant, copies the data into newly allocated memory in Go. Plutonium does not allow it.\nTo look at the same bytes without copying, use unsafe.String or unsafe.Slice with unsafe.StringData. To copy, allocate a buffer yourself and copy into it.",
			Why:  "In Go the copy is allocated and later freed by the garbage collector. Plutonium has no garbage collector and never allocates behind the program's back.",
			Fix:  &Fix{Intro: "To view bytes as a string without copying", Code: "s := unsafe.String(unsafe.SliceData(b), len(b))"},
			Spec: pspec("3", "What Plutonium removes from Go"),
			Example: "s := string(buf[:n])",
			Fixed:   "s := unsafe.String(unsafe.SliceData(buf), n)",
		},
		&Entry{
			Code: StringConcat, Name: "StringConcat",
			Text: "Joining strings with + makes a new string, and that needs memory. Plutonium only allows + on strings whose values are constants, which the compiler joins while compiling.\nTo build a string at run time, use pluto/strings.Builder, which takes an allocator, or format into a buffer of your own with fmt.Bprintf.",
			Why:  "In Go the result of + is allocated and later freed by the garbage collector. Plutonium has no garbage collector and never allocates behind the program's back.",
			Fix:  &Fix{Intro: "Use a Builder", Code: "var b strings.Builder\ndefer b.Free()\nb.WriteString(\"hello, \")\nb.WriteString(name)\ngreeting := b.String()"},
			Spec: pspec("3", "What Plutonium removes from Go"),
			Example: "greeting := \"hello, \" + name",
		},
		&Entry{
			Code: ErrorsNewValue, Name: "ErrorsNewValue",
			Text: "errors.New must be called directly. Each place that calls it gets its own static error, made when compiling, so taking errors.New as a value, or deferring it, has no call site to make that error for.",
			Why:  "In Go, errors.New allocates. Plutonium makes a static error per call site so that it never allocates.",
			Spec: pspec("13.1", "Sentinels and errors.New"),
			Example: "newErr := errors.New",
		},
		&Entry{
			Code: ClosureCtorValue, Name: "ClosureCtorValue",
			Text: "This function's body returns a function literal that captures the function's parameters, such as an iterator constructor (func Count(n int) iter.Seq[int] { return func(yield func(int) bool) { ... } }). A closure may not outlive the frame it captures from, so Plutonium builds such a closure in the caller's frame: each call is compiled where it is made, with the parameters as variables of the caller. That needs a caller, so the function can only be called directly. Taking it as a func value or a method value, or calling it through an interface, has no caller's frame to build the closure in.\nCall it directly, or wrap the call in a function that returns something other than the closure.",
			Why:  "In Go the captured parameters move to the heap and the closure can be returned. Plutonium never moves a variable to the heap, and building the closure in the caller's frame costs no allocation.",
			Fix:  &Fix{Intro: "Call it where the iterator is used", Code: "for i := range Count(3) {\n\t...\n}"},
			Spec: pspec("4.5", "Closures do not outlive their frame"),
			Example: "f := Count\nfor i := range f(3) {\n\t...\n}",
			Fixed:   "for i := range Count(3) {\n\t...\n}",
		},
		&Entry{
			Code: PerCPUInit, Name: "PerCPUInit",
			Text: "A per-CPU variable can only be initialized with a constant. Package initialization runs before any processor has its per-CPU block, so there is nowhere to store a value computed at run time. Set the variable at run time on each processor instead.",
			Spec: pspec("11.5", "Per-CPU variables"),
		},
		&Entry{
			Code: PerCPUMode, Name: "PerCPUMode",
			Text: "//pu:percpu variables are reached through the GS segment register, which only a kernel sets up. Build with --mode kernel, or remove the directive.",
			Spec: pspec("11.5", "Per-CPU variables"),
		},
		&Entry{
			Code: PerCPUSection, Name: "PerCPUSection",
			Text: "A per-CPU variable always lives in the .percpu section, so it cannot also be placed in another section with //pu:section.",
			Spec: pspec("11.5", "Per-CPU variables"),
		},
		&Entry{
			Code: InterruptDecl, Name: "InterruptDecl",
			Text: "A //pu:interrupt function is entered by the processor, not called by Plutonium code. So it must be a plain function with a body: not a method, not generic, and not marked //pu:inline, since the processor needs one fixed address to jump to.",
			Spec: pspec("11.4", "pluto/arch/x86"),
		},
		&Entry{
			Code: InterruptSig, Name: "InterruptSig",
			Text: "An interrupt handler receives what the processor pushes on the stack: a pointer to the interrupt frame, and for some exceptions an error code. It returns nothing, since it returns with iretq to the interrupted code.",
			Fix:  &Fix{Intro: "Use one of these signatures", Code: "//pu:interrupt\nfunc timer(frame *x86.InterruptFrame)\n\n//pu:interrupt\nfunc pageFault(frame *x86.InterruptFrame, code uint64)"},
			Spec: pspec("11.4", "pluto/arch/x86"),
		},
		&Entry{
			Code: HandlerAddrArg, Name: "HandlerAddrArg",
			Text: "x86.HandlerAddr gives the address of an interrupt handler for the interrupt table. Its argument must name a function marked //pu:interrupt.",
			Spec: pspec("11.4", "pluto/arch/x86"),
		},
		&Entry{
			Code: InterruptHandlerV, Name: "InterruptHandlerV",
			Text: "An interrupt handler saves every register and returns with iretq, so ordinary code cannot call it, and it is not an ordinary func value. Use x86.HandlerAddr to get its address for the interrupt table.",
			Fix:  &Fix{Intro: "Take its address for the table", Code: "addr := x86.HandlerAddr(timer)"},
			Spec: pspec("11.4", "pluto/arch/x86"),
		},
	)
}
