package diag

import typerr "github.com/matjam/plutonium/internal/types2/errors"

// The explanations of the rules Plutonium adds to Go or changes. Each says
// why Plutonium differs, in Why.

// pspec names a section of the Plutonium spec, whose chapters are the files
// of docs/spec/language/.
func pspec(section, title string) string {
	return "\"" + title + "\" in the Plutonium spec, section " + section
}

// whyNoGC is the reason shared by the constructs Plutonium removes.
const whyNoGC = "Plutonium has no garbage collector and never allocates memory unless the program asks for it in the code, so that every cost is visible and the same code can run inside a kernel."

func init() {
	register(
		&Entry{
			Code: CheckerCode(typerr.NotSendable), Name: "NotSendable",
			Text:    "A value that crosses tasks, as the argument or receiver of a task statement or as the element of a channel, must be sendable. This type holds a shared[T], whose reference count is changed with ordinary arithmetic, so two tasks copying or dropping references at once would corrupt it. That holds through pointers, slices, maps and interfaces too: two tasks that reach one struct through a pointer could both copy the shared[T] out of it. Starting a method of a shared box with a task statement (task z.Serve()) hands the box itself to the new task, so it needs a synced box as well.\nUse synced[T], which counts atomically.",
			Why:     "Go's garbage collector makes every pointer safe to share. Plutonium counts shared references without atomic instructions, which is only correct on one task.",
			Fix:     &Fix{Intro: "Count the references atomically", Code: "type Job struct {\n\tdata synced[Buffer]\n}\n\njobs := make(chan Job, 16)"},
			Spec:    pspec("8.8", "Sendable types"),
			Example: "type Job struct {\n\tdata shared[Buffer]\n}\n\njobs := make(chan Job, 16)",
		},
		&Entry{
			Code: CheckerCode(typerr.NotPortable), Name: "NotPortable",
			Text:    "A type parameter declared [T portable] stands for a value that can be sent between address spaces, such as two operating system processes that share no memory. Its value must mean the same to the receiver as to the sender, so it may refer to nothing in the sender's memory. A portable type is made only of numbers, booleans, enums and packed types, and of arrays, structs, unions, ring, volatile and aligned types of those. A pointer, slice, string, function value, channel, map, interface value, box (owned, shared, synced, weak) or owner anywhere inside the type makes it not portable. An owner is not portable even when it is built from numbers, because sending it would also transfer the duty to drop it.\nThe message names the first field that stops the type and says why. Replace it with data that holds its own contents, such as a fixed-size array of bytes and a length in place of a string.",
			Why:     "Go has one address space, so any value can be passed anywhere and a string or pointer always means the same thing. A Plutonium kernel and its processes have separate address spaces, and the compiler checks what may cross instead of leaving the receiver to read a stale address.",
			Fix:     &Fix{Intro: "Send the bytes in a fixed-size array, with their length as a number", Code: "type Header struct {\n\tname  [16]byte\n\tnameN uint8\n}"},
			Spec:    pspec("8.9", "Portable types"),
			Example: "type Header struct {\n\tname string\n}\n\nfunc Send[T portable](port int, v T) {}\n\nSend(1, Header{})",
			Fixed:   "type Header struct {\n\tname  [16]byte\n\tnameN uint8\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.FrameViewSent), Name: "FrameViewSent",
			Text:    "This value points into the current function's stack frame: the address of a local variable or of part of one, a slice of a local array, or the address of a composite literal. A task statement or a channel send hands it to another task, which may still use it after this function has returned and its frame is gone.\nStart the work with a sync.Scope instead: it waits for the tasks it starts before its block ends, so they may use the function's variables. Or allocate the variable with new, so that it outlives the frame, or make it a package-level variable.",
			Why:     "Go moves such variables to the heap without saying so. Plutonium never allocates behind the program's back, so a local stays in its frame.",
			Fix:     &Fix{Intro: "Start the work with a sync.Scope, which waits for it before its block ends", Code: "var s sync.Scope\ns.Task(func() { worker(&state) })"},
			Spec:    pspec("18.3", "Views"),
			Example: "var wg sync.WaitGroup\nwg.Add(1)\ntask worker(&wg)",
		},
		&Entry{
			Code: CheckerCode(typerr.RecoverNotPermitted), Name: "RecoverNotPermitted",
			Text:    "In Plutonium, panic stops the program. A hosted program first runs the pending deferred calls, as Go does, but control never returns to the function that panicked, so there is nothing for recover to catch.\nReport failures that the caller can handle by returning an error instead.",
			Why:     "Resuming a function after a panic needs an unwinder, tables and runtime support that a kernel cannot rely on, and a panic in kernel code means the state is already wrong.",
			Fix:     &Fix{Intro: "Return an error instead", Code: "func parse(s string) (int, error) {\n\tif s == \"\" {\n\t\treturn 0, errors.New(\"empty input\")\n\t}\n\t...\n}"},
			Spec:    pspec("13.1", "Built-ins Plutonium does not have"),
			Example: "defer func() {\n\tif r := recover(); r != nil {\n\t\tlog(r)\n\t}\n}()",
		},
		&Entry{
			Code: CheckerCode(typerr.ReflectNotPermitted), Name: "ReflectNotPermitted",
			Text:    "Plutonium has no reflect package. Reflection needs a description of every type in the running program.\nUse a type switch, or generics, to handle values of several types.",
			Why:     "Plutonium keeps type descriptors only for the types a program converts to an interface, so binaries stay small enough for a kernel.",
			Spec:    pspec("14.3", "Import declarations"),
			Example: "import \"reflect\"",
			Fixed:   "switch v := x.(type) {\ncase int:\n\t...\ncase string:\n\t...\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.ComplexNotPermitted), Name: "ComplexNotPermitted",
			Text:    "Plutonium has no complex number types (complex64 and complex128) and no complex, real or imag builtins.\nUse a struct of two floats for the real and imaginary parts.",
			Why:     "Complex numbers are rarely useful in systems code, and leaving them out keeps the code generator small.",
			Fix:     &Fix{Intro: "Use a struct", Code: "type Complex struct{ Re, Im float64 }"},
			Spec:    pspec("7.2", "Numeric types"),
			Example: "var z complex128 = 1 + 2i",
		},
		&Entry{
			Code: CheckerCode(typerr.MissingAllocatorType), Name: "MissingAllocatorType",
			Text: "new and make take an optional allocator as their last argument. Its type, Allocator, is declared in package pluto/rt, which this build could not find. This happens only when the standard library is missing or incomplete.\nCheck that PLUTOROOT, if set, points at a directory that contains pluto/rt.",
			Spec: pspec("13.9", "Allocation"),
		},
		&Entry{
			Code: CheckerCode(typerr.ErrorsNewNotConstant), Name: "ErrorsNewNotConstant",
			Text:    "errors.New takes only a constant message, such as a string literal. The compiler makes one error value for each place errors.New is called, and stores it in the program image, so the message must be known when compiling.\nTo build a message from values at run time, use fmt.Errorf, which allocates the error.",
			Why:     "In Go, errors.New allocates a new error on every call. Plutonium never allocates behind the program's back, so errors.New makes a static error instead, which costs nothing and can never fail.",
			Fix:     &Fix{Intro: "To include values in the message", Code: "return fmt.Errorf(\"open %s: not found\", name)"},
			Spec:    pspec("16.3", "Sentinels and errors.New"),
			Example: "return errors.New(\"open \" + name)",
			Fixed:   "return fmt.Errorf(\"open %s\", name)",
		},
		&Entry{
			Code: CheckerCode(typerr.BadMainSignature), Name: "BadMainSignature",
			Text: "func main takes no arguments. It may return nothing, as in Go, or one int64, which becomes the program's exit status.\nRead command-line arguments with pluto/os instead of parameters.",
			Fix:  &Fix{Intro: "Either form works", Code: "func main() { ... }\nfunc main() int64 { return 0 }"},
			Spec: pspec("10.12", "Function declarations"),
		},
		&Entry{
			Code: CheckerCode(typerr.InvalidUTF8String), Name: "InvalidUTF8String",
			Text:    "Every string in Plutonium holds valid UTF-8 text, so a string literal whose escapes spell out bytes that are not valid UTF-8 is an error. Here the escapes make a byte or a sequence of bytes that does not encode a character.\nBinary data belongs in a []byte or a byte array, which may hold any bytes. To write the character itself, use its \\u escape or type it.",
			Why:     "Go strings may hold any bytes. Plutonium guarantees that a string is text, so that ranging over it, slicing it between runes and the library's rune functions never meet broken encodings, and so that ASCII strings can be recognized once and taken fast paths for.",
			Fix:     &Fix{Intro: "Keep binary data in bytes", Code: "magic := []byte{0x7f, 'E', 'L', 'F'}"},
			Spec:    pspec("4.11", "String literals"),
			Example: "const magic = \"\\x7fELF\\xff\"",
			Fixed:   "var magic = [4]byte{0x7f, 'E', 'L', 0xff}",
		},
		&Entry{
			Code: CheckerCode(typerr.AppendNotPermitted), Name: "AppendNotPermitted",
			Text: "Plutonium has no append. In Go, append grows a slice by allocating a larger array behind " +
				"the program's back, and the garbage collector frees the old one. A Plutonium slice is a view " +
				"of an array that something else owns, so it cannot grow.\n" +
				"Use a vec.Vec from pluto/vec, which owns its array and grows it from an allocator. " +
				"To fill a slice's spare capacity without growing it, slice it longer and assign, or use copy.",
			Why:     whyNoGC,
			Fix:     &Fix{Intro: "To build a sequence that grows", Code: "import \"pluto/vec\"\n\nv := vec.New[int]()\nv.Push(1)\nv.Append(2, 3)"},
			Spec:    pspec("13.1", "Built-ins Plutonium does not have"),
			Example: "var s []int\ns = append(s, 1)\ns = append(s, 2, 3)",
			Fixed:   "s := vec.New[int]()\ns.Push(1)\ns.Append(2, 3)",
		},
		&Entry{
			Code: CheckerCode(typerr.MakeSliceNotPermitted), Name: "MakeSliceNotPermitted",
			Text: "Plutonium has no make for slices. In Go, make([]T, n) allocates an array that the garbage " +
				"collector frees when no slice refers to it. A Plutonium slice is a view of an array that " +
				"something else owns, so a new array needs an owner to free it.\n" +
				"vec.Make[T](n) from pluto/vec returns a vec.Vec[T] of n zero elements, which owns its array and " +
				"frees it in Free; vec.WithCapacity[T](c) is make([]T, 0, c). v.Slice() lends the elements as a " +
				"slice, and v[i] and len(v) work on the Vec itself. For a size known when the program is " +
				"compiled, an array needs no allocation at all.",
			Why:     whyNoGC,
			Fix:     &Fix{Intro: "Make a vec.Vec, which owns the array", Code: "import \"pluto/vec\"\n\nbuf := vec.Make[byte](n)\ndefer buf.Free()"},
			Spec:    pspec("13.7", "Making channels and maps"),
			Example: "buf := make([]byte, n)\nread(buf)",
			Fixed:   "buf := vec.Make[byte](n)\ndefer buf.Free()\nread(buf.Slice())",
		},
		&Entry{
			Code: CheckerCode(typerr.BytesToStringNotPermitted), Name: "BytesToStringNotPermitted",
			Text: "Plutonium has no string(b) for a byte slice b. Every string holds valid UTF-8 text, but a " +
				"slice of bytes can hold anything. In Go the conversion keeps the bytes. Here it would have to " +
				"change each byte that is not valid UTF-8 into the replacement character U+FFFD without telling you, " +
				"so two different slices, such as {0xfe} and {0xff}, would give equal strings.\n" +
				"Say what you mean. strings.FromUTF8(b) returns the string and an error, and fails if the bytes are not " +
				"valid UTF-8. strings.FromUTF8Lossy(b) returns a string in which each invalid byte is replaced by U+FFFD. " +
				"If the bytes are binary data, such as a digest used as a map key, keep them in a byte array or a " +
				"slice and not in a string.",
			Why:     "Go strings may hold any bytes. Plutonium strings are text, so a conversion from bytes must either check them or replace some of them, and it must not do the second without the program saying so. The data in a string must not change silently.",
			Fix:     &Fix{Intro: "To check the bytes and handle bad input", Code: "import \"pluto/strings\"\n\ns, err := strings.FromUTF8(b)\nif err != nil {\n\treturn err\n}"},
			Spec:    pspec("11.24", "Conversions"),
			Example: "s := string(b)",
			Fixed:   "s, err := strings.FromUTF8(b)",
		},
		&Entry{
			Code: CheckerCode(typerr.StringToSliceNotPermitted), Name: "StringToSliceNotPermitted",
			Text: "Plutonium has no []byte(s) or []rune(s) for a string s. In Go the conversion allocates a new " +
				"slice and the garbage collector frees it later. A Plutonium slice is a view of an array that " +
				"something else owns, so a copy of a string needs an owner to free it.\n" +
				"strings.Bytes(s) and strings.Runes(s) from pluto/strings return a vec.Vec that owns the copy and frees it " +
				"when it is dropped; v.Slice() lends the elements as a slice. To fill a buffer of your own without " +
				"allocating, use copy(buf[:], s), which works from a string into a []byte. To visit the runes of " +
				"a string, range over it, and to count them, use utf8.RuneCountInString(s).",
			Why:     "Go allocates the copy behind the program's back and leaves the garbage collector to free it. Plutonium never allocates unless the code says so, and every allocation has an owner, so that every cost is visible and the same code can run inside a kernel.",
			Fix:     &Fix{Intro: "To keep a copy that you own", Code: "import \"pluto/strings\"\n\nb := strings.Bytes(s)\nuse(b.Slice())"},
			Spec:    pspec("11.24", "Conversions"),
			Example: "b := []byte(s)\nr := []rune(s)",
			Fixed:   "b := strings.Bytes(s)\nr := strings.Runes(s)",
		},
		&Entry{
			Code: CheckerCode(typerr.IntToStringNotPermitted), Name: "IntToStringNotPermitted",
			Text: "string(i) of an integer does not give the digits of i. In Go it gives the character whose code is i, " +
				"so string(65) is \"A\" and string(n) for n := 65 is \"A\" too, not \"65\". Go's own vet tool reports it " +
				"because programmers expect the digits.\n" +
				"Plutonium accepts a conversion to string from a rune and from an untyped constant such as string('A'), " +
				"and nothing else, so the choice is visible in the source. To make the character, convert to a rune first: " +
				"string(rune(i)). To make the digits, use strconv.Itoa(i), or strconv.FormatInt and strconv.FormatUint for " +
				"other integer types.",
			Why:     "A conversion that reads like formatting but means something else is a trap that Go itself warns about. Plutonium has no false friends: the program says which of the two it means.",
			Fix:     &Fix{Intro: "To make the character with code i", Code: "s := string(rune(i))"},
			Spec:    pspec("11.24", "Conversions"),
			Example: "n := 65\ns := string(n)",
			Fixed:   "n := 65\ns := strconv.Itoa(n)",
		},

		// Packed types and wrappers.
		&Entry{
			Code: CheckerCode(typerr.BadPackedBacking), Name: "BadPackedBacking",
			Text:    "The backing type of a packed type says how its bits are stored. It must be an unsigned or signed integer type (uint32), an array of bytes ([8]byte), or []byte. It can also be left out.",
			Spec:    pspec("7.14", "packed types"),
			Example: "type R packed float64 { a bits[uint, 4] }",
			Fixed:   "type R packed uint64 { a bits[uint, 4] }",
		},
		&Entry{
			Code: CheckerCode(typerr.PackedTooLarge), Name: "PackedTooLarge",
			Text:    "The fields of a packed type are laid out one after another, from bit 0. Their widths add up to more bits than the backing type holds.\nUse a larger backing type, make fields narrower, or leave the backing type out so the size follows the fields.",
			Spec:    pspec("7.14", "packed types"),
			Example: "type R packed uint8 {\n\ta bits[uint, 6]\n\tb bits[uint, 4]\n}",
			Fixed:   "type R packed uint16 {\n\ta bits[uint, 6]\n\tb bits[uint, 4]\n\t_ bits[uint, 6]\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.BadBitField), Name: "BadBitField",
			Text:    "A bit field is written bits[T, N]: N bits read as type T. T must be an integer type at least N bits wide, an enum, or bool with N equal to 1.",
			Spec:    pspec("7.14", "packed types"),
			Example: "mode bits[uint8, 12]",
			Fixed:   "mode bits[uint16, 12]",
		},
		&Entry{
			Code: CheckerCode(typerr.BadPackedField), Name: "BadPackedField",
			Text: "A field of a packed type must have a fixed bit layout: an integer, bool, float, packed type, or an array of those. Strings, slices, pointers and structs have no fixed layout in bits.",
			Spec: pspec("7.14", "packed types"),
		},
		&Entry{
			Code: CheckerCode(typerr.BadPackedLiteral), Name: "BadPackedLiteral",
			Text:    "A packed literal must name each field it sets, as field: value. The fields of a packed type have no order a literal could follow, since reserved bits sit between them.",
			Spec:    pspec("7.14", "packed types"),
			Example: "r := ControlReg{5, true}",
			Fixed:   "r := ControlReg{mode: 5, enable: true}",
		},
		&Entry{
			Code: CheckerCode(typerr.WrongWrapperArgs), Name: "WrongWrapperArgs",
			Text: "volatile takes one type argument: volatile[T]. aligned and ring take a type and a constant: aligned[T, N] and ring[T, N].",
			Spec: pspec("7.15", "volatile[T]"),
		},
		&Entry{
			Code: CheckerCode(typerr.NotPowerOfTwo), Name: "NotPowerOfTwo",
			Text:    "The alignment of aligned[T, N] and the size of ring[T, N] must be a power of two: 1, 2, 4, 8, 16 and so on. Hardware alignment is always a power of two, and a ring of that size finds a slot with a mask instead of a division.",
			Spec:    pspec("7.16", "aligned[T, N]"),
			Example: "var buf ring[byte, 100]",
			Fixed:   "var buf ring[byte, 128]",
		},
		&Entry{
			Code: CheckerCode(typerr.BadVolatileElem), Name: "BadVolatileElem",
			Text:    "volatile[T] makes every read and write of T exactly one load or store. That is only possible for a value the processor moves in one instruction: an integer, a bool, a pointer, or a packed type backed by an integer.",
			Spec:    pspec("7.15", "volatile[T]"),
			Example: "var status volatile[[4]uint32]",
			Fixed:   "var status [4]volatile[uint32]",
		},
		&Entry{
			Code: CheckerCode(typerr.BadWrapperSize), Name: "BadWrapperSize",
			Text: "The second argument of aligned[T, N] and ring[T, N] must be a positive integer constant, since the layout is fixed when compiling.",
			Spec: pspec("7.16", "aligned[T, N]"),
		},
		&Entry{
			Code: CheckerCode(typerr.RingLiteralNotEmpty), Name: "RingLiteralNotEmpty",
			Text:    "A ring literal cannot list elements. Write an empty literal, or use the zero value, and add elements with Push.",
			Spec:    pspec("7.17", "ring[T, N]"),
			Example: "r := ring[int, 4]{1, 2}",
			Fixed:   "var r ring[int, 4]\nr.Push(1)\nr.Push(2)",
		},

		// Default parameter values.
		&Entry{
			Code: CheckerCode(typerr.MisplacedDefault), Name: "MisplacedDefault",
			Text:    "Default parameter values are allowed only on the parameters of a declared function or method. Receivers and results cannot have them, and neither can function types, function literals or interface methods, because a call through a function value or an interface does not know which function it calls, so it could not fill in the defaults.",
			Spec:    pspec("10.13", "Default parameter values"),
			Example: "type Grower func(n int, factor int = 2) int",
		},
		&Entry{
			Code: CheckerCode(typerr.VariadicDefault), Name: "VariadicDefault",
			Text:    "A variadic function (one whose last parameter is ...T) cannot have default values. An argument after the variadic ones could not be told apart from them, and a default before them would have to be given whenever any variadic argument is.\nVariadic functions in the standard library allocate from alloc.Default instead; alloc.Use replaces it for a region of code.",
			Spec:    pspec("10.13", "Default parameter values"),
			Example: "func Logf(format string, args ...any, w io.Writer = os.Stderr)",
		},
		&Entry{
			Code: CheckerCode(typerr.MissingDefault), Name: "MissingDefault",
			Text:    "A call may leave out only trailing parameters. So once a parameter has a default value, every parameter after it needs one too.",
			Spec:    pspec("10.13", "Default parameter values"),
			Example: "func Grow(n int, factor int = 2, limit int) int",
			Fixed:   "func Grow(n int, limit int, factor int = 2) int",
		},
		&Entry{
			Code: CheckerCode(typerr.SharedDefault), Name: "SharedDefault",
			Text:    "A parameter with a default value needs a name and type of its own. In a, b int = 1 it is unclear whether the default belongs to a, to b, or to both.",
			Spec:    pspec("10.13", "Default parameter values"),
			Example: "func f(a, b int = 1)",
			Fixed:   "func f(a int = 1, b int = 1)",
		},
		&Entry{
			Code: CheckerCode(typerr.DefaultRefersToParam), Name: "DefaultRefersToParam",
			Text:    "A default value is evaluated by the caller, in the scope where the function is declared, before the function runs. The function's parameters do not exist yet at that point, so a default cannot use them.\nDefault to a sentinel value instead, and compute the real value in the body.",
			Spec:    pspec("10.13", "Default parameter values"),
			Example: "func Fill(buf []byte, n int = len(buf))",
			Fixed:   "func Fill(buf []byte, n int = -1) {\n\tif n < 0 {\n\t\tn = len(buf)\n\t}\n\t...\n}",
		},

		// Enums.
		&Entry{
			Code: CheckerCode(typerr.MisplacedEnum), Name: "MisplacedEnum",
			Text:    "An enum type can only be the type of a type definition, type E enum { ... }. It cannot be written inline in a variable or field, or be an alias, because its members are reached through its name (E.Member).",
			Spec:    pspec("7.12", "Enums"),
			Example: "var c enum { Red; Green }",
			Fixed:   "type Color enum { Red; Green }\n\nvar c Color",
		},
		&Entry{
			Code: CheckerCode(typerr.GenericEnum), Name: "GenericEnum",
			Text: "An enum type cannot have type parameters. Its members are constants of one fixed integer type.",
			Spec: pspec("7.12", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.BadEnumBase), Name: "BadEnumBase",
			Text:    "The type after the enum keyword is the integer type that holds the enum's values. It must be an integer type such as uint8 or int32. Leave it out to let the compiler pick the smallest one that fits.",
			Spec:    pspec("7.12", "Enums"),
			Example: "type Mode enum string { Read; Write }",
			Fixed:   "type Mode enum { Read; Write }",
		},
		&Entry{
			Code: CheckerCode(typerr.EmptyEnum), Name: "EmptyEnum",
			Text: "An enum needs at least one member, and one member must have the value 0, since that is the zero value of the type.",
			Spec: pspec("7.12", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.BlankEnumMember), Name: "BlankEnumMember",
			Text:    "Every member of an enum needs a name. To skip a value, give the next member an explicit value instead.",
			Spec:    pspec("7.12", "Enums"),
			Example: "type E enum { A; _; C }",
			Fixed:   "type E enum { A; C = 2 }",
		},
		&Entry{
			Code: CheckerCode(typerr.DuplicateEnumValue), Name: "DuplicateEnumValue",
			Text:    "Two members of an enum have the same value. Each value must belong to one member, so that converting a number to the enum, and printing a value's name, give one answer.\nA member without a value is one more than the member before it, which is often how two members end up equal.",
			Spec:    pspec("7.12", "Enums"),
			Example: "type E enum {\n\tA = 1\n\tB\n\tC = 2\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.EnumWithoutZero), Name: "EnumWithoutZero",
			Text:    "A variable of an enum type starts as 0, like every Plutonium variable. So one member must have the value 0, or a fresh variable would hold a value that is not a member.\nAdd a member for the zero value, often named None, Unknown or Invalid.",
			Spec:    pspec("7.12", "Enums"),
			Example: "type Level enum {\n\tLow = 1\n\tHigh\n}",
			Fixed:   "type Level enum {\n\tUnset\n\tLow\n\tHigh\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.EnumOverflow), Name: "EnumOverflow",
			Text:    "A member's value does not fit in the enum's integer type. Pick a wider type after the enum keyword, or leave the type out so the compiler picks one that fits.",
			Spec:    pspec("7.12", "Enums"),
			Example: "type E enum uint8 { A; B = 300 }",
		},
		&Entry{
			Code: CheckerCode(typerr.BadEnumValue), Name: "BadEnumValue",
			Text:    "The value of an enum member must be a constant integer expression, such as 5, 1 << 3 or another constant, since members are constants fixed when compiling.",
			Spec:    pspec("7.12", "Enums"),
			Example: "type E enum { A = 1.5 }",
		},
		&Entry{
			Code: CheckerCode(typerr.EnumSelfReference), Name: "EnumSelfReference",
			Text: "An enum's member values cannot use the enum being declared, because those values are what decides the enum's integer type.",
			Spec: pspec("7.12", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.UnexportedEnumMember), Name: "UnexportedEnumMember",
			Text: "Enum members follow Go's rule for names: a member whose name starts with a lower-case letter is visible only inside its own package.",
			Spec: pspec("7.12", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.MissingEnumCase), Name: "MissingEnumCase",
			Text:    "A switch on an enum value that has no default case must list every member. That way, adding a member to the enum shows every switch that needs to handle it.\nAdd a case for each missing member, or a default case if the remaining members share one action.",
			Why:     "Go does not check this. Plutonium does, because an enum is a closed set of values, so the compiler can tell when one is forgotten.",
			Spec:    pspec("12.9", "Switch statements"),
			Example: "switch c {\ncase Color.Red:\n\t...\n}",
			Fixed:   "switch c {\ncase Color.Red:\n\t...\ndefault:\n\t...\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.BadEnumConversion), Name: "BadEnumConversion",
			Text:    "Only integers convert to an enum, and a constant converts only if it is the value of a member. Anything else would make an enum value that is not a member.",
			Spec:    pspec("7.12", "Enums"),
			Example: "c := Color(7) // Color has no member with value 7",
		},
		&Entry{
			Code: CheckerCode(typerr.UncheckedEnumConversion), Name: "UncheckedEnumConversion",
			Text: "A number that is not a constant may not be the value of any member, so converting it to an enum needs a check. Use the comma-ok form: ok reports whether the number is a member's value.",
			Why:  "Go allows any integer in a named integer type. Plutonium enums are closed sets, so a value outside the set is caught where it enters, not later in a switch.",
			Fix:  &Fix{Intro: "Use the comma-ok form", Code: "c, ok := Color(n)\nif !ok {\n\treturn errBadColor\n}"},
			Spec: pspec("7.12", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.NotEnumType), Name: "NotEnumType",
			Text: "This operation needs an enum type, such as len(E) or for v := range E, and the type given is not an enum.",
			Spec: pspec("7.12", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.EnumArithmetic), Name: "EnumArithmetic",
			Text: "Enum values can be compared, but not added, multiplied, shifted or combined with bitwise operators, since the result would usually not be a member. Convert to an integer to do arithmetic.",
			Fix:  &Fix{Intro: "Convert to an integer first", Code: "next := int(c) + 1"},
			Spec: pspec("7.12", "Enums"),
		},
		&Entry{
			Code: CheckerCode(typerr.RangeOverEnumValue), Name: "RangeOverEnumValue",
			Text: "To loop over the members of an enum, range over the enum type itself, not over a value of it.",
			Fix:  &Fix{Intro: "Range over the type", Code: "for c := range Color {\n\t...\n}"},
			Spec: pspec("12.10", "For statements"),
		},

		// The indexing interface.
		&Entry{
			Code: CheckerCode(typerr.MissingIndexMethod), Name: "MissingIndexMethod",
			Text: "Library containers such as vec.Vec and hash.Map are indexed through methods. x[k] calls x.Index(k), x[k] = v calls x.SetIndex(k, v), and v, ok := x[k] calls x.IndexOk(k). The container's type does not have the method this use of x[k] needs.\nAdd the method to the type, or call another of its methods directly.",
			Why:  "Go builds maps and slices into the language. In Plutonium they are library types that own their memory, so the index syntax is lent to them through methods.",
			Fix:  &Fix{Intro: "To support v, ok := x[k], add", Code: "func (m *Table) IndexOk(k string) (int, bool) {\n\t...\n}"},
			Spec: pspec("11.11", "Indexing library containers"),
		},
		&Entry{
			Code: CheckerCode(typerr.BadIndexMethod), Name: "BadIndexMethod",
			Text:    "The indexing methods have fixed forms, so that x[k] can be turned into a call: Index(k K) V, SetIndex(k K, v V) and IndexOk(k K) (V, bool). They may not have type parameters of their own or be variadic.",
			Spec:    pspec("11.11", "Indexing library containers"),
			Example: "func (v *Vec) Index(i int) (int, error)",
			Fixed:   "func (v *Vec) Index(i int) int",
		},
		&Entry{
			Code: CheckerCode(typerr.IndexMethodMismatch), Name: "IndexMethodMismatch",
			Text:    "The indexing methods of one type must agree: Index, SetIndex and IndexOk take the same key type K, and use the same element type V. Otherwise x[k] would have a different type depending on whether it is read or assigned, and x[k] += 1 could not read and write the same element.",
			Spec:    pspec("11.11", "Indexing library containers"),
			Example: "func (m *M) Index(k string) int\nfunc (m *M) SetIndex(k string, v int64)",
			Fixed:   "func (m *M) Index(k string) int\nfunc (m *M) SetIndex(k string, v int)",
		},
		&Entry{
			Code: CheckerCode(typerr.ContainerIndexAddress), Name: "ContainerIndexAddress",
			Text:    "x[k] on a container calls its Index method, which returns a copy of the element. A copy has no address that would stay valid, and a pointer to the element itself would be invalidated as soon as the container grows or rehashes and moves its elements.\nCall a method that returns the pointer explicitly, such as vec.Vec.Ptr or hash.Map.Ptr. Its documentation says how long the pointer is valid.",
			Why:     "&s[i] on a slice is safe because a slice never moves its elements. A container that grows does, so Plutonium makes the pointer an explicit method call rather than give it the same syntax as the safe case.",
			Fix:     &Fix{Intro: "Ask for the pointer by name", Code: "p := v.Ptr(i)\np.count++"},
			Spec:    pspec("11.11", "Indexing library containers"),
			Example: "p := &v[i]",
			Fixed:   "p := v.Ptr(i)",
		},
		&Entry{
			Code: CheckerCode(typerr.ContainerElemAssign), Name: "ContainerElemAssign",
			Text:    "x[k] on a container is a copy that its Index method returns, so assigning to a field or element of it would change only the copy. This is the same rule as Go's for map elements.\nRead the element, change it, and store it back with x[k] = v, or change it in place through a pointer from a method such as Ptr.",
			Fix:     &Fix{Intro: "Store the changed element back", Code: "p := v[i]\np.x = 1\nv[i] = p"},
			Spec:    pspec("11.11", "Indexing library containers"),
			Example: "v[i].x = 1",
			Fixed:   "v.Ptr(i).x = 1",
		},
		&Entry{
			Code: CheckerCode(typerr.RangeOverContainer), Name: "RangeOverContainer",
			Text: "A for range loop over a value that is not a string, integer, array, slice, map, channel or function calls the value's All method and ranges over the iterator it returns, an iter.Seq or iter.Seq2. This type has no All method.\nGive the type an All method that returns an iterator, or range over one of its other iterators.",
			Why:  "Library containers such as vec.Vec and hash.Map are structs, not built-in types, so the loop asks them for an iterator by name. vec.Vec and hash.Map have All, so they range like Go's slices and maps.",
			Fix:  &Fix{Intro: "Add an All method", Code: "func (t *T) All() iter.Seq2[string, int] {\n\treturn func(yield func(string, int) bool) {\n\t\t// call yield for each key and value\n\t}\n}"},
			Spec: pspec("12.10", "For statements"),
		},
		&Entry{
			Code: CheckerCode(typerr.BadRangeMethod), Name: "BadRangeMethod",
			Text:    "A for range loop over a container calls its All method, which must take no arguments and return an iterator: a function of the form func(yield func(K, V) bool), such as iter.Seq[V] or iter.Seq2[K, V]. This type's All method has another form.",
			Spec:    pspec("12.10", "For statements"),
			Example: "func (l *List) All() []int\n\nfor x := range l { ... }",
			Fixed:   "func (l *List) All() iter.Seq[int]\n\nfor x := range l { ... }",
		},
		&Entry{
			Code: CheckerCode(typerr.UnaddressableRange), Name: "UnaddressableRange",
			Text:    "A for range loop over a container calls its All method. That method has a pointer receiver, so the loop needs the address of the container. Here the container is a value that has no address, such as the result of a function call.\nStore the container in a variable first, or range over a pointer to it.",
			Fix:     &Fix{Intro: "Store it in a variable first", Code: "m := load()\nfor k, v := range m {\n\tuse(k, v)\n}"},
			Spec:    pspec("12.10", "For statements"),
			Example: "for k, v := range load() { ... }",
		},
		&Entry{
			Code: CheckerCode(typerr.UnaddressableContainer), Name: "UnaddressableContainer",
			Text:    "The container's indexing method has a pointer receiver, so x[k] needs the address of x. Here x is a value that has no address, such as the result of a function call.\nStore the container in a variable first, or index through a pointer to it.",
			Fix:     &Fix{Intro: "Store it in a variable first", Code: "m := load()\nv := m[\"key\"]"},
			Spec:    pspec("11.11", "Indexing library containers"),
			Example: "v := load()[\"key\"]",
		},

		// Or statements.
		&Entry{
			Code: CheckerCode(typerr.OrNotFunctionCall), Name: "OrNotFunctionCall",
			Text:    "An or clause tests the error that a function returns as its last result. A conversion, such as int64(x), and a call of a built-in function, such as len(s), are written like calls, but they call no function and return no error, so there is nothing for or to test.\nRemove the or clause, or call a function that returns an error.",
			Spec:    pspec("11.27", "Or clauses"),
			Example: "n := int64(count()) or return",
			Fixed:   "n := int64(count())",
		},
		&Entry{
			Code: CheckerCode(typerr.OrNoError), Name: "OrNoError",
			Text:    "The call before or must return a result of type error last, and the type must be error itself. A function that returns no error, or no result at all, cannot fail in a way that or can see. A type that implements error, a defined type based on error, or a type parameter is not accepted either.\nRemove the or clause, or change the function so that its last result has type error.",
			Why:     "With the type exactly error, the test is always the nil test of one interface type, and err in the clause is the result itself, not a conversion of it.",
			Fix:     &Fix{Intro: "Return the error as an error", Code: "func parse(s string) (int, error)"},
			Spec:    pspec("11.27", "Or clauses"),
			Example: "s := strconv.Itoa(5) or return",
			Fixed:   "s := strconv.Itoa(5)",
		},
		&Entry{
			Code: CheckerCode(typerr.OrClauseCompletes), Name: "OrClauseCompletes",
			Text:    "The clause of an or statement runs when the call failed, and the code after the statement uses the values the call did not produce. So the clause must leave: it must be a return, break, continue or goto statement, a call of panic, or a block that ends in one of these. An if statement in the block leaves only when it has an else branch and both branches leave. A call of a function that never returns, such as os.Exit or log.Fatal, does not count, since whether a function compiles must not depend on the body of another.\nEnd the clause with a return, break, continue, goto or panic.",
			Why:     "The or clause is the short form of an if statement that tests an error and leaves. Requiring the exit on the line keeps the code after the statement from running with values that were never set.",
			Fix:     &Fix{Intro: "End the clause with a statement that leaves", Code: "cfg := load(path) or {\n\tlog.Printf(\"loading: %v\", err)\n\treturn err\n}"},
			Spec:    pspec("11.27", "Or clauses"),
			Example: "cfg := load(path) or log.Printf(\"loading: %v\", err)",
			Fixed:   "cfg := load(path) or {\n\tlog.Printf(\"loading: %v\", err)\n\treturn err\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.OrNoErrorResult), Name: "OrNoErrorResult",
			Text:    "An unbraced return with no operands in an or clause returns err as the function's last result. This function has no result of type error last, so there is nowhere to put err, and returning without it would drop the error.\nHandle the error in a block clause, or return values that this function has.",
			Why:     "The short form of return exists to pass an error up. In a function that cannot return one, the program must say what to do with the error.",
			Fix:     &Fix{Intro: "Handle the error in a block clause", Code: "load(path) or {\n\tlog.Printf(\"loading: %v\", err)\n\treturn\n}"},
			Spec:    pspec("11.27", "Or clauses"),
			Example: "func main() {\n\tload(path) or return\n}",
			Fixed:   "func main() {\n\tload(path) or panic(err)\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.OrPostStmt), Name: "OrPostStmt",
			Text:    "The post statement of a for statement runs at the end of each iteration. An or statement cannot be the post statement, since a break or continue in its clause would end or repeat the iteration that is ending.\nTest the error at the end of the loop body instead.",
			Fix:     &Fix{Intro: "Test the error in the body", Code: "for i := 0; i < n; {\n\t...\n\ti = next(i) or break\n}"},
			Spec:    pspec("11.27", "Or clauses"),
			Example: "for i := 0; i < n; i = next(i) or break {\n\t...\n}",
			Fixed:   "for i := 0; i < n; {\n\t...\n\ti = next(i) or break\n}",
		},

		// Unions.
		&Entry{
			Code: CheckerCode(typerr.MisplacedUnion), Name: "MisplacedUnion",
			Text:    "A union type can only be the type of a type definition, type U union { ... }. It cannot be written inline in a variable or field, or be an alias, because its variants are reached through its name (U.V).",
			Spec:    pspec("7.13", "Unions"),
			Example: "type Alias = union { A int; B }",
			Fixed:   "type Shape union {\n\tA int\n\tB\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.GenericPackedUnion), Name: "GenericPackedUnion",
			Text: "A packed union cannot have type parameters. Its layout is fixed bit by bit by its backing type, its tag and its fields, so it cannot depend on a type argument. An ordinary union can be generic.",
			Spec: pspec("7.13", "Unions"),
		},
		&Entry{
			Code: CheckerCode(typerr.EmptyUnion), Name: "EmptyUnion",
			Text:    "A union value holds one of its variants, so a union needs at least one. Variants are written like struct fields: a name, and a payload type if the variant carries data.",
			Spec:    pspec("7.13", "Unions"),
			Example: "type Option union {}",
			Fixed:   "type Option union {\n\tNone\n\tSome int\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.BadVariantDecl), Name: "BadVariantDecl",
			Text:    "Each variant of a union has a name of its own, which is not _, followed by its payload type if it has one. Variants have no tags, and in a packed union each variant has its own case clause.",
			Spec:    pspec("7.13", "Unions"),
			Example: "type Value union {\n\t*Node\n\t_ int\n}",
			Fixed:   "type Value union {\n\tRef  *Node\n\tSize int\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.TooManyVariants), Name: "TooManyVariants",
			Text: "A union numbers its variants with a tag of at most 16 bits, so it can have at most 65536 variants. Group related variants into a variant whose payload is another union or an enum.",
			Spec: pspec("7.13", "Unions"),
		},
		&Entry{
			Code: CheckerCode(typerr.UnionOwnerPayload), Name: "UnionOwnerPayload",
			Text:    "This payload is an owner: an owned[T] box, or a type declared owner, such as a file or a vec.Vec. A union that holds one owns the payload of the variant it holds. It drops that payload when the union is dropped or given another variant, and it moves instead of being copied, so that the payload has exactly one owner. Plutonium never makes a type an owner silently, so the union must be declared owner union.\nIf the union should only look at a value that something else owns, hold a pointer in the payload instead.",
			Why:     whyOwners,
			Fix:     &Fix{Intro: "Declare the union as an owner", Code: "type Slot owner union {\n\tEmpty\n\tFull owned[Buffer]\n}"},
			Spec:    pspec("7.13", "Unions"),
			Example: "type Slot union {\n\tEmpty\n\tFull owned[Buffer]\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.BadUnionTag), Name: "BadUnionTag",
			Text:    "A packed union has a backing type, which fixes its size, and exactly one named field before its variants: the tag. The tag is a bool, integer or enum field that occupies the same bits in every variant, and each variant lists the tag values that select it.",
			Spec:    pspec("7.13", "Unions"),
			Example: "type PTE packed union uint64 {\n\tSwapped case false { slot bits[uint64, 63] }\n}",
			Fixed:   "type PTE packed union uint64 {\n\tpresent bits[bool, 1]\n\n\tSwapped case false { slot bits[uint64, 63] }\n\tMapped case true { frame bits[uint64, 40] }\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.BadVariantCase), Name: "BadVariantCase",
			Text:    "The values after case in a packed union variant are the tag values that select it. Each must be a constant of the tag's type that fits in the tag's bits, and no value may select two variants, so that every bit pattern means at most one variant.",
			Spec:    pspec("7.13", "Unions"),
			Example: "type Desc packed union uint8 {\n\tkind bits[uint8, 2]\n\tA case 1 {}\n\tB case 1, 4 {}\n}",
			Fixed:   "type Desc packed union uint8 {\n\tkind bits[uint8, 2]\n\tA case 1 {}\n\tB case 2, 3 {}\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.VariantNotType), Name: "VariantNotType",
			Text:    "A variant of a union is not a type. U.V(x) and U.V{...} make a value of the union type U holding the variant, and a type switch on a union gives you the payload of the variant it holds, which has the payload's own type.\nTo keep a payload in a variable, give the variable the payload's type.",
			Why:     "Go has no unions. Keeping variants out of the type system means a payload always has its own type and methods, and generic unions need no special rules.",
			Spec:    pspec("7.13", "Unions"),
			Example: "var r DeviceState.Ready",
			Fixed:   "var st DeviceState = DeviceState.Ready{irq: 11}",
		},
		&Entry{
			Code: CheckerCode(typerr.UnknownVariant), Name: "UnknownVariant",
			Text: "The union has no variant with this name, or the type before the dot is not a union, so it has no variants at all. The message lists the variants the union has.",
			Spec: pspec("7.13", "Unions"),
		},
		&Entry{
			Code: CheckerCode(typerr.UnexportedVariant), Name: "UnexportedVariant",
			Text: "Union variants follow Go's rule for names: a variant whose name starts with a lower-case letter is visible only inside its own package. A switch in another package that cannot name it needs a default case.",
			Spec: pspec("7.13", "Unions"),
		},
		&Entry{
			Code: CheckerCode(typerr.UnionLiteral), Name: "UnionLiteral",
			Text:    "A union value holds one of its variants, so a literal must name the variant: U.V{...} for a variant whose payload is a struct, array or packed type, or U.V(payload) for any variant with a payload. A variant without a payload is the value U.V itself. In a packed union, the variant sets the tag, so its literal cannot.",
			Spec:    pspec("7.13", "Unions"),
			Example: "st := DeviceState{}",
			Fixed:   "st := DeviceState.Off\nst = DeviceState.Probing{attempt: 1}",
		},
		&Entry{
			Code: CheckerCode(typerr.NotAVariant), Name: "NotAVariant",
			Text:    "A type switch on a union value, and a type assertion on one, name the union's own variants: U.V, or in a case of the switch the bare name V. A union value is never nil, and a variant of another union, or a type, can never match.\nA bare variant name is accepted only in the cases of a type switch, where the switch says which union is meant.",
			Spec:    pspec("7.13", "Unions"),
			Example: "switch v := st.(type) {\ncase int:\n\t...\n}",
			Fixed:   "switch v := st.(type) {\ncase Probing:\n\t...\ndefault:\n\t...\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.MissingVariantCase), Name: "MissingVariantCase",
			Text:    "A type switch on a union value that has no default case must name every variant. That way, adding a variant to the union shows every switch that needs to handle it.\nAdd a case for each missing variant, or a default case if the remaining variants share one action. A switch that names every variant needs no default to end a function.",
			Why:     "Go cannot check this for interfaces, whose set of types is open. A union's variants are a closed set, so the compiler can tell when one is forgotten.",
			Spec:    pspec("12.9", "Switch statements"),
			Example: "switch s := st.(type) {\ncase Off:\n\tstart()\ncase Probing:\n\tretry(s.attempt)\n}",
			Fixed:   "switch s := st.(type) {\ncase Off:\n\tstart()\ncase Probing:\n\tretry(s.attempt)\ncase Ready, Failed:\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.PackedUnionNoDefault), Name: "PackedUnionNoDefault",
			Text:    "A packed union's bits usually come from hardware or from converting a raw value, so its tag may hold any value its bits allow, including values no variant names. A type switch without a default case must therefore cover every value of the tag; this one does not.\nAdd a default case that handles the other values.",
			Spec:    pspec("12.9", "Switch statements"),
			Example: "switch d.(type) {\ncase Seg:\n\t...\ncase Int:\n\t...\n}",
			Fixed:   "switch d.(type) {\ncase Seg:\n\t...\ncase Int:\n\t...\ndefault:\n\treturn errBadDescriptor\n}",
		},

		// Rules checked while compiling.
		&Entry{
			Code: NotYetSupported, Name: "NotYetSupported",
			Text: "The program is valid, but this compiler cannot compile this construct yet. It stops with an error rather than produce wrong code.\nRewrite the code without the construct, or check whether a newer compiler supports it.",
			Spec: pspec("21.2", "No silent failures"),
		},
		&Entry{
			Code: ExternWithBody, Name: "ExternWithBody",
			Text:    "//pu:extern says that the function is written in C or assembly and linked in under the given symbol. A function with that directive must be declared without a body.",
			Spec:    pspec("20.4", "Assembly and C"),
			Example: "//pu:extern strlen\nfunc strlen(s *byte) uint64 { return 0 }",
			Fixed:   "//pu:extern strlen\nfunc strlen(s *byte) uint64",
		},
		&Entry{
			Code: BadCSignature, Name: "BadCSignature",
			Text: "Functions marked //pu:extern or //pu:export are called with the C calling convention. Their parameters and results must be types C has: integers, floats, bools, pointers, and structs and arrays of those. Strings, slices, interfaces and func values have no C equivalent, and variadic functions are not supported.\nPass a pointer and a length instead of a string or slice.",
			Fix:  &Fix{Intro: "Pass a pointer and a length", Code: "//pu:extern write\nfunc write(fd int32, buf *byte, n uint64) int64\n\nwrite(1, unsafe.SliceData(b), uint64(len(b)))"},
			Spec: pspec("20.4", "Assembly and C"),
		},
		&Entry{
			Code: DuplicateExport, Name: "DuplicateExport",
			Text: "Two functions are exported under the same symbol name with //pu:export, or the name is already used by another symbol. The linker needs each symbol name to be unique.",
			Spec: pspec("20.4", "Assembly and C"),
		},
		&Entry{
			Code: GenericExport, Name: "GenericExport",
			Text: "A generic function is compiled once for each set of type arguments it is used with, so there is no single function to export under one C symbol. Export a non-generic wrapper that calls it with the types you need.",
			Fix:  &Fix{Intro: "Export a wrapper", Code: "//pu:export sum_ints\nfunc sumInts(p *int, n int) int { return Sum[int](unsafe.Slice(p, n)) }"},
			Spec: pspec("20.4", "Assembly and C"),
		},
		&Entry{
			Code: KernelFloat, Name: "KernelFloat",
			Text: "In kernel mode, functions may not use floating point. When an interrupt or system call enters the kernel, the processor does not save the floating-point registers, so kernel code that used them would corrupt the state of the program it interrupted.\nFunctions marked //pu:fpu may use floats. Whoever calls one must save the floating-point state first, with x86.FPUBegin and x86.FPUEnd.\nCopying a struct that contains float fields is allowed, since it uses only integer registers.",
			Why:  "Go has no kernel mode. Plutonium rejects floats here, rather than emulate them in software, so that no hidden cost or library ends up in the kernel.",
			Fix:  &Fix{Intro: "Mark the function, and save the state around calls", Code: "//pu:fpu\nfunc scale(x float64) float64 { return x * 1.5 }\n\nx86.FPUBegin(&area)\ny := scale(x)\nx86.FPUEnd(&area)"},
			Spec: pspec("20.7", "Floating point in kernel mode"),
		},
		&Entry{
			Code: DeferInLoop, Name: "DeferInLoop",
			Text:    "A defer statement inside a loop would add a deferred call on every iteration, and each needs room in the function's stack frame until it returns. Plutonium gives every deferred call a fixed slot in the frame, so the number must be known when compiling.\nMove the loop body into a function of its own; its defer then runs at the end of each iteration.",
			Why:     "Go stores such deferred calls on the heap. Plutonium never allocates behind the program's back, and kernel stacks are small and fixed.",
			Fix:     &Fix{Intro: "Move the body into a function", Code: "for _, name := range names {\n\tprocess(name)\n}\n\nfunc process(name string) {\n\tf := open(name)\n\tdefer f.Close()\n\t...\n}"},
			Spec:    pspec("12.19", "Defer statements"),
			Example: "for _, name := range names {\n\tf := open(name)\n\tdefer f.Close()\n}",
		},
		&Entry{
			Code: TooManyDefers, Name: "TooManyDefers",
			Text: "A function may contain at most 64 defer statements. Each has a fixed slot in the stack frame. Split the function into smaller ones.",
			Spec: pspec("12.19", "Defer statements"),
		},
		&Entry{
			Code: DeferBuiltin, Name: "DeferBuiltin",
			Text: "This builtin cannot be deferred directly. Wrap the call in a function literal and defer that.",
			Fix:  &Fix{Intro: "Defer a function literal", Code: "defer func() { n = len(s) }()"},
			Spec: pspec("12.19", "Defer statements"),
		},
		&Entry{
			Code: DeferInRangeFunc, Name: "DeferInRangeFunc",
			Text:    "The body of a loop over a function (for x := range seq) runs once for each value the iterator yields, so a defer statement in it is a defer inside a loop. Its deferred call belongs to the function the loop is in and runs when that function returns, as in Go. Plutonium gives each defer statement one slot in that function's frame, so the defer may run at most once per call: after it, every path must leave the loop, by return, by panic, or by break, continue or goto to a statement outside the loop.\nIf the deferred call should run at the end of each iteration, move the body into a function of its own.",
			Why:     "Go keeps the deferred calls of such a loop in a list on the heap. Plutonium never allocates behind the program's back, and kernel stacks are small and fixed.",
			Fix:     &Fix{Intro: "Leave the loop after the defer, or give the body a function of its own", Code: "for item := range items.All() {\n\tif item.ID == id {\n\t\titem.mu.Lock()\n\t\tdefer item.mu.Unlock()\n\t\treturn item.Value\n\t}\n}"},
			Spec:    pspec("12.11", "Range over functions"),
			Example: "for item := range items.All() {\n\titem.mu.Lock()\n\tdefer item.mu.Unlock()\n\tupdate(item)\n}",
			Fixed:   "for item := range items.All() {\n\tupdateLocked(item)\n}\n\nfunc updateLocked(item *Item) {\n\titem.mu.Lock()\n\tdefer item.mu.Unlock()\n\tupdate(item)\n}",
		},
		&Entry{
			Code: DeferLitCapture, Name: "DeferLitCapture",
			Text:    "A function literal deferred in the body of a loop over a function runs when the enclosing function returns, after the loop body has returned. The body's own variables, including the loop variables, live in the body's frame, which is gone by then, so the literal cannot capture them.\nPass the values the deferred call needs as arguments: arguments are evaluated when the defer statement runs.",
			Why:     "Go moves such variables to the heap. Plutonium never moves a variable to the heap; a closure is valid only while the frame it captures from is.",
			Fix:     &Fix{Intro: "Pass the loop variable as an argument", Code: "for x := range seq {\n\tif done(x) {\n\t\tdefer func(x int) { log(x) }(x)\n\t\treturn\n\t}\n}"},
			Spec:    pspec("12.11", "Range over functions"),
			Example: "for x := range seq {\n\tif done(x) {\n\t\tdefer func() { log(x) }()\n\t\treturn\n\t}\n}",
		},
		&Entry{
			Code: ErrorsNewValue, Name: "ErrorsNewValue",
			Text:    "errors.New must be called directly. Each place that calls it gets its own static error, made when compiling, so taking errors.New as a value, or deferring it, has no call site to make that error for.",
			Why:     "In Go, errors.New allocates. Plutonium makes a static error per call site so that it never allocates.",
			Spec:    pspec("16.3", "Sentinels and errors.New"),
			Example: "newErr := errors.New",
		},
		&Entry{
			Code: PerCPUInit, Name: "PerCPUInit",
			Text: "A per-CPU variable can only be initialized with a constant. Package initialization runs before any processor has its per-CPU block, so there is nowhere to store a value computed at run time. Set the variable at run time on each processor instead.",
			Spec: pspec("20.9", "Per-CPU variables"),
		},
		&Entry{
			Code: PerCPUMode, Name: "PerCPUMode",
			Text: "//pu:percpu variables are reached through the GS segment register, which only a kernel sets up. Build with --mode kernel, or remove the directive.",
			Spec: pspec("20.9", "Per-CPU variables"),
		},
		&Entry{
			Code: PerCPUSection, Name: "PerCPUSection",
			Text: "A per-CPU variable always lives in the .percpu section, so it cannot also be placed in another section with //pu:section.",
			Spec: pspec("20.9", "Per-CPU variables"),
		},
		&Entry{
			Code: StackDirective, Name: "StackDirective",
			Text: "//pu:stack sets the size of the stack a hosted program runs on: the runtime reserves that much address space before main starts. It belongs on func main in package main, and takes a size in bytes, with an optional K, M or G suffix, from 64K to 64G. A freestanding program sets up its own stack, so the directive has nothing to act on there.",
			Fix:  &Fix{Intro: "Put it on main", Code: "//pu:stack 256M\nfunc main() { ... }"},
			Spec: pspec("20.3", "Directives"),
		},
		&Entry{
			Code: InterruptDecl, Name: "InterruptDecl",
			Text: "A //pu:interrupt function is entered by the processor, not called by Plutonium code. So it must be a plain function with a body: not a method, not generic, and not marked //pu:inline, since the processor needs one fixed address to jump to.",
			Spec: pspec("20.10", "Interrupts"),
		},
		&Entry{
			Code: InterruptSig, Name: "InterruptSig",
			Text: "An interrupt handler receives what the processor pushes on the stack: a pointer to the interrupt frame, and for some exceptions an error code. It returns nothing, since it returns with iretq to the interrupted code.",
			Fix:  &Fix{Intro: "Use one of these signatures", Code: "//pu:interrupt\nfunc timer(frame *x86.InterruptFrame)\n\n//pu:interrupt\nfunc pageFault(frame *x86.InterruptFrame, code uint64)"},
			Spec: pspec("20.10", "Interrupts"),
		},
		&Entry{
			Code: HandlerAddrArg, Name: "HandlerAddrArg",
			Text: "x86.HandlerAddr gives the address of an interrupt handler for the interrupt table. Its argument must name a function marked //pu:interrupt.",
			Spec: pspec("20.8", "pluto/arch/x86"),
		},
		&Entry{
			Code: GoCapturingLiteral, Name: "GoCapturingLiteral",
			Text:    "A function literal that uses variables of the function around it reaches them in that function's stack frame. A task statement may start it after that function has returned, when the frame is gone.\nStart the literal with a sync.Scope instead: s.Task(func() {...}) waits for it before s's block ends, so it may use the function's variables, a Mutex or a WaitGroup among them. Or pass the values the literal needs as arguments: they are copied when the task statement runs. Do not pass a Mutex, a WaitGroup or another sync type this way, since the copy is a different one.",
			Why:     "Go moves captured variables to the heap. Plutonium never allocates behind the program's back, so captured variables stay in their frame (spec section 11.5).",
			Fix:     &Fix{Intro: "Pass the values as arguments", Code: "for i := 0; i < n; i++ {\n\ttask func(i int, out chan int) {\n\t\tout <- i * i\n\t}(i, out)\n}"},
			Spec:    pspec("12.12", "Task statements"),
			Example: "for i := 0; i < n; i++ {\n\ttask func() {\n\t\tout <- i * i\n\t}()\n}",
		},
		&Entry{
			Code: GoFrameClosure, Name: "GoFrameClosure",
			Text: "A function literal that captures variables, and a method value such as c.handle, keep their closure record (the code to call, with the captured variables or a copy of the receiver) in the frame of the function that makes them. A func value points to that record. A task statement copies its function and arguments, but copying a func value copies only the pointer, so the task may call through it after the frame is gone.\n" +
				"The compiler reports this wherever it can tell: a capturing literal or a method value passed to a task statement, or a local variable assigned only once, from one of them. Elsewhere the task statement checks when it runs, and panics.\n" +
				"Library functions that start a task with a func argument, such as sync.WaitGroup.Task and time.AfterFunc, follow the same rule and the compiler reports them the same way.\n" +
				"To let the task use the function's variables, start a function literal with a sync.Scope declared in the function instead: s.Task(func() {...}) waits for it before s's block ends.",
			Why:     "Go moves captured variables and the receivers of method values to the heap. Plutonium never allocates behind the program's back, so the record stays in its frame (spec section 11.5).",
			Fix:     &Fix{Intro: "Pass the values the function needs, and start a function that takes them", Code: "task func(n int, out chan int) {\n\tout <- n * n\n}(n, out)"},
			Spec:    pspec("12.12", "Task statements"),
			Example: "square := func() { out <- n * n }\ntask square()",
		},
		&Entry{
			Code: GoBuiltin, Name: "GoBuiltin",
			Text: "A task statement cannot start a built-in function such as println or close directly. Wrap it in a function and start that.",
			Spec: pspec("12.12", "Task statements"),
		},
		&Entry{
			Code: BadScheduler, Name: "BadScheduler",
			Text: "A program binds its scheduler with one package-level variable marked //pu:scheduler, whose type, not an interface, implements rt.Scheduler: NewTask, FreeTask, Spawn, Park, Wake and Yield. The runtime calls its methods directly. This variable does not qualify, or the program marks more than one.",
			Why:  "The scheduler is chosen when the program is linked, like the panic handler, so the runtime calls it without an indirection and a program cannot run without one.",
			Spec: pspec("19.5", "The scheduler contract"),
		},
		&Entry{
			Code: NoScheduler, Name: "NoScheduler",
			Text: "This program starts tasks, uses channels or waits in pluto/sync, so it needs a scheduler, and none is bound. Hosted builds bind one by default; a freestanding program, such as a kernel, provides its own: a package-level variable marked //pu:scheduler whose type implements rt.Scheduler.",
			Why:  "Plutonium carries a scheduler only in programs that ask for one, and a kernel supplies its own, so a freestanding program must say which.",
			Fix:  &Fix{Intro: "Bind the kernel's scheduler", Code: "//pu:scheduler\nvar scheduler kernelScheduler"},
			Spec: pspec("19.5", "The scheduler contract"),
		},
		&Entry{
			Code: SchedulerTimers, Name: "SchedulerTimers",
			Text: "Timers need two things from the scheduler: a clock, its Now method, which returns monotonic nanoseconds, and a way to be called back, its ArmTimer method, which asks for rt.RunTimers to be called once a deadline has passed. This program uses timers (time.Sleep, a time.Timer or Ticker, rt.ParkUntil or rt.Now), and the bound scheduler lacks one of them.",
			Why:  "The scheduler contract keeps timers optional, so a kernel that never sleeps needs no clock. A program that does sleep must get its clock from the kernel, because only the kernel knows which timer hardware it drives.",
			Fix:  &Fix{Intro: "Give the scheduler a clock, and run the timers from the timer interrupt", Code: "func (kernelSched) Now() int64 { return int64(ticks) * 1000000 }\n\nfunc (kernelSched) ArmTimer(deadline int64) { next = deadline }\n\n// in the timer interrupt handler:\nif kernelSched{}.Now() >= next {\n\trt.RunTimers()\n}"},
			Spec: pspec("19.5", "The scheduler contract"),
		},
		&Entry{
			Code: InterruptHandlerV, Name: "InterruptHandlerV",
			Text: "An interrupt handler saves every register and returns with iretq, so ordinary code cannot call it, and it is not an ordinary func value. Use x86.HandlerAddr to get its address for the interrupt table.",
			Fix:  &Fix{Intro: "Take its address for the table", Code: "addr := x86.HandlerAddr(timer)"},
			Spec: pspec("20.10", "Interrupts"),
		},
		&Entry{
			Code: ClosureReturned, Name: "ClosureReturned",
			Text: "A function literal that uses variables of the function around it, and a method value such as c.handle, are closure records: the code to run, with where each captured variable is or a copy of the receiver. A function that returns one builds it, and the variables it captures, in a region of memory its caller provides, so the closure lives as long as the caller's frame. That works when the function itself makes the closure and returns it as a func, in an interface, or in a struct or array result.\n" +
				"This function returns a closure record that cannot be built there: one made by a function literal nested in it or by the body of a range-over-func loop, which are functions of their own whose frames are gone when they return, or one its results hold only through a pointer, for which its callers provide no region.",
			Why:     "Go moves the captured variables and the closure to the heap. Plutonium never allocates behind the program's back, so the record lives in a frame: the maker's, or the region its caller provides.",
			Fix:     &Fix{Intro: "Make the closure in the function that returns it, and return it by value", Code: "func counter() func() int {\n\tn := 0\n\treturn func() int {\n\t\tn++\n\t\treturn n\n\t}\n}"},
			Spec:    pspec("11.5", "Closures and their regions"),
			Example: "func newHandler() *Handler {\n\th := new(Handler)\n\tcount := 0\n\th.serve = func() { count++ }\n\treturn h\n}",
		},
		&Entry{
			Code: ClosureRegionLoop, Name: "ClosureRegionLoop",
			Text:    "A function that returns a closure builds it in a region its caller provides. For a call in a loop that region is part of the caller's frame, and every iteration's call uses the same region, so the closure one iteration gets is overwritten by the next iteration's call. Keeping it past the iteration, in a variable declared outside the loop, in a variable of the for statement (which passes its value on to the next iteration) or in memory reached through a pointer, would leave it changing underneath. A goto back to a label before the call makes a loop too.\nPassing the closure back to the same call, as in it = Filter(it, f), would make each closure hold the one before it: a chain as long as the loop runs, which no region of fixed size can hold.\nUse the closure within the iteration, or declare the variable that holds it inside the loop. To compose closures, compose a fixed number of them, or keep what each step needs in a slice and return one closure that applies every step.",
			Why:     "Go allocates each closure on the heap. Plutonium builds it in the caller's frame, which has room for one closure per call site.",
			Fix:     &Fix{Intro: "Use the closure within the iteration", Code: "for _, n := range sizes {\n\tnext := counterFrom(n)\n\tprintln(next(), next())\n}"},
			Spec:    pspec("11.5", "Closures and their regions"),
			Example: "all := vec.New[func() int]()\nfor _, n := range sizes {\n\tall.Push(counterFrom(n))\n}",
		},
		&Entry{
			Code: ClosureRegionCycle, Name: "ClosureRegionCycle",
			Text:    "A function that returns a closure builds it in a region its caller provides, and that region holds the regions of the calls whose closures the function returns in turn. When the function returns the closures of a call that can reach the same function again, directly or through other functions, or through a func value or an interface whose implementations do, its region would have to hold itself. No fixed size fits, so the caller could not reserve it.\nBuild the closures in a loop instead of by recursion, or keep their state in a value the caller owns, such as a struct with a method.",
			Why:     "Go allocates each closure on the heap, however deep the recursion. Plutonium builds closures in frames, whose sizes are fixed when compiling.",
			Spec:    pspec("11.5", "Closures and their regions"),
			Example: "func chain(n int) func() int {\n\tif n == 0 {\n\t\treturn func() int { return 0 }\n\t}\n\tnext := chain(n - 1)\n\treturn func() int { return n + next() }\n}",
		},
		&Entry{
			Code: NoPanicHandler, Name: "NoPanicHandler",
			Text:    "A freestanding or kernel build has no operating system to report a crash to, so the program itself must say what happens when it panics (print to a serial port or screen, then halt). It does so with one function marked //pu:panic.",
			Why:     whyPanicHandler,
			Fix:     &Fix{Intro: "Mark one function with //pu:panic", Code: "//pu:panic\nfunc onPanic(p *rt.Panic) { ... }"},
			Spec:    pspec("17.4", "Panics in freestanding programs"),
			Example: "package main\n\nfunc main() {}",
			Fixed:   "package main\n\nimport (\n\t\"pluto/arch/x86\"\n\t\"pluto/rt\"\n)\n\n//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tp.Print(rt.Console{})\n\tfor {\n\t\tx86.Cli()\n\t\tx86.Hlt()\n\t}\n}\n\nfunc main() {}",
		},
		&Entry{
			Code: DupPanicHandler, Name: "DupPanicHandler",
			Text: "A program has exactly one panic handler, and another function is already marked //pu:panic. Remove //pu:panic from all but one of them.",
			Why:  whyPanicHandler,
			Spec: pspec("17.4", "Panics in freestanding programs"),
		},
		&Entry{
			Code: PanicHandlerDecl, Name: "PanicHandlerDecl",
			Text: "The runtime calls the panic handler, the function marked //pu:panic, with a description of the panic, and never expects it to return. It is a plain function with a body, not a method, not generic and not also an interrupt handler, that takes a *rt.Panic and returns nothing.",
			Why:  whyPanicHandler,
			Fix:  &Fix{Intro: "Declare it", Code: "//pu:panic\nfunc onPanic(p *rt.Panic) { ... }"},
			Spec: pspec("17.4", "Panics in freestanding programs"),
		},
		&Entry{
			Code: PanicHandlerRet, Name: "PanicHandlerRet",
			Text:    "A panic stops the program: it does not unwind, so there is nothing for the panic handler to return to. Every path through it must end in an endless loop or in a call of a function that never returns, such as os.Exit, or a function whose every path ends the same way. A call through a func value or an interface, and recursion, count as returning.",
			Why:     whyPanicHandler,
			Fix:     &Fix{Intro: "End the handler by halting", Code: "for {\n\tx86.Cli()\n\tx86.Hlt()\n}"},
			Spec:    pspec("17.4", "Panics in freestanding programs"),
			Example: "//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tp.Print(rt.Console{})\n}",
			Fixed:   "//pu:panic\nfunc onPanic(p *rt.Panic) {\n\tp.Print(rt.Console{})\n\tfor {\n\t\tx86.Cli()\n\t\tx86.Hlt()\n\t}\n}",
		},
		&Entry{
			Code: ContextWaits, Name: "ContextWaits",
			Text: "Interrupt handlers run on top of whatever code the CPU was executing, and the panic handler on top of whatever panicked. If either waits, for a lock or for room in a channel, the code it interrupted can never resume to release it, and the CPU stops. So nothing they can reach may call a function that may park (such as sync.Mutex.Lock or time.Sleep), send or receive on a channel outside a select with a default case, run a select without one, or allocate from an allocator that may wait.\nCalls through interfaces and func values count as calling every method or function they could reach in the program.\nWriting to an os.File, such as os.Stderr, waits for the poller when the file is a full pipe or socket, and the check cannot tell which file a call writes to. In a panic or interrupt handler, write to rt.Console{} instead of os.Stderr: it writes to standard error with a system call that never waits.",
			Why:  whyContext,
			Fix:  &Fix{Intro: "To hand work to normal code, use a non-blocking send", Code: "select { case work <- item: default: }"},
			Spec: pspec("20.10", "Interrupts"),
		},
		&Entry{
			Code: ContextSpawns, Name: "ContextSpawns",
			Text: "Interrupt handlers run on top of whatever code the CPU was executing, and the panic handler on top of whatever panicked. Starting a task allocates its stack and takes the scheduler's locks, which the interrupted code may hold, so nothing they can reach may use a task statement.",
			Why:  whyContext,
			Fix:  &Fix{Intro: "To hand work to a task, wake one that waits for it, or send without waiting", Code: "select { case work <- item: default: }"},
			Spec: pspec("20.10", "Interrupts"),
		},
		&Entry{
			Code: InitAllocates, Name: "InitAllocates",
			Text:    "In a freestanding build, which every kernel is, a package-level initializer or an init function can reach an allocation: new, make, a map literal, an operation that makes a string at run time, or a call that reaches one. A freestanding program has no allocator while its packages are initialized; its entry function installs one with alloc.SetDefault, after initialization has run. Initializers that need no allocation, such as composite literals, &T{...}, slice literals, conversions to an interface and errors.New, are placed in static storage and are allowed.\nDeclare the variable without an initializer, and set it in the entry function after alloc.SetDefault.",
			Why:     "Go's runtime sets up its heap before any package is initialized. A freestanding Plutonium program brings its own allocator, so there is none until its own code installs it, and no console to report the failure on if an initializer tried.",
			Spec:    pspec("15.2", "Package initialization"),
			Example: "var cache = make(map[string]int)",
			Fixed:   "var cache map[string]int\n\nfunc kernelMain(info *BootInfo) {\n\talloc.SetDefault(newSlab(info))\n\tcache = make(map[string]int)\n}",
		},
		&Entry{
			Code: InitSpawns, Name: "InitSpawns",
			Text: "Package initialization runs sequentially before main. Starting another task there could let it read package variables before initialization finishes. A package variable's initializer or an init function must not reach a task statement or a library call that starts a task. The check follows calls, including deferred calls, drops, interfaces and func values, through the complete program.",
			Why:  "Tasks may read package variables written only during initialization without a lock. Finishing initialization before starting another task preserves that data race rule.",
			Fix:  &Fix{Intro: "Start the task from main or a function main calls", Code: "func main() {\n\ttask worker()\n}"},
			Spec: pspec("15.2", "Package initialization"),
		},
		&Entry{
			Code: UnknownDirective, Name: "UnknownDirective",
			Text:    "A comment that starts with //pu: and no space is a directive: an instruction to the compiler. This one has a name the compiler does not know, so it would do nothing. A misspelt directive that was ignored could silently leave out an interrupt handler, a per-CPU variable or an inlining request, so the compiler reports it.\nThe directives are: build, inline, noinline, nochecks, extern, export, section, interrupt, noreturn, noescape, resultholds, panic, blocking, scheduler, nosimd, fpu, percpu and stack.\nA comment that is only a note should start with // and a space.",
			Why:     "Go ignores unknown //go: comments, which makes a typo easy to miss. Plutonium directives change what the program means, so a typo must not go unnoticed.",
			Fix:     &Fix{Intro: "Use one of the directives, spelt exactly", Code: "//pu:interrupt\nfunc timer(frame *x86.InterruptFrame) { ... }"},
			Spec:    pspec("20.3", "Directives"),
			Example: "//pu:interupt\nfunc timer(frame *x86.InterruptFrame) { ... }",
			Fixed:   "//pu:interrupt\nfunc timer(frame *x86.InterruptFrame) { ... }",
		},
		&Entry{
			Code: RetiredDirective, Name: "RetiredDirective",
			Text:    "This directive was removed, so the line is a compile error instead of something the compiler reads. Where the compiler now works the fact out, delete the line; the program means the same without it. Where another directive replaced it, change the name to the replacement.\nRemoved directives and what replaced them:\n//pu:alloc: kernel code may allocate strings anywhere the allocator may be called.\n//pu:bytewise: the compiler proves that string slice bounds fall on rune boundaries.\n//pu:gofunc: the compiler finds which functions start tasks with their func arguments.\n//pu:schedhook and //pu:schedlate: the compiler knows the runtime's scheduler hooks by name.\n//pu:nobounds: replaced by //pu:nochecks, which removes the bounds checks and the generation checks from the function's body.\n//pu:noreturn on a function with a body: the compiler finds that a function never returns when its body ends in a terminating statement. The directive is still needed on a function without a body, written in assembly or C.\n//pu:noescape on a function with a body: the compiler finds which pointer arguments a function keeps by reading its body. The directive is still needed on a function without a body, written in assembly or C.",
			Why:     "Plutonium asks for a directive only for a fact the compiler cannot derive from the program, so that using the language does not need knowledge of how the compiler works.",
			Fix:     &Fix{Intro: "Delete the directive", Code: "func halt() {\n\tfor {\n\t}\n}"},
			Spec:    pspec("20.3", "Directives"),
			Example: "//pu:noreturn\nfunc halt() {\n\tfor {\n\t}\n}",
			Fixed:   "func halt() {\n\tfor {\n\t}\n}",
		},
		&Entry{
			Code: MisplacedDirective, Name: "MisplacedDirective",
			Text:    "A directive belongs in the doc comment of the declaration it applies to: the lines of comment directly above it, with no blank line between. Each directive applies to one kind of declaration, such as functions or package-level variables, and //pu:build applies to a whole file and goes before the package clause. In any other place the compiler would not see it and it would do nothing.",
			Why:     "Go's //go: directives are ignored when misplaced. Plutonium directives change what the program means, so a directive that does nothing is an error.",
			Fix:     &Fix{Intro: "Put it directly above the declaration it applies to", Code: "//pu:noinline\nfunc slow() { ... }"},
			Spec:    pspec("20.3", "Directives"),
			Example: "func f() {\n\t//pu:noinline\n\tg()\n}",
			Fixed:   "//pu:noinline\nfunc g() { ... }\n\nfunc f() {\n\tg()\n}",
		},
		&Entry{
			Code: GoDirective, Name: "GoDirective",
			Text:    "A line comment that starts with //go: and no space is a Go directive. Plutonium does not read //go: comments, so this one would sit in the file looking as if it had an effect. Plutonium's directives start with //pu:, and the ones Go also has are spelt the same after the prefix: //pu:build, //pu:noinline, and //pu:noescape on a function without a body. Go's others, such as //go:generate, //go:embed, //go:linkname and //go:nosplit, have no equivalent.\nWrite the //pu: directive, or delete the line.",
			Why:     "Go ignores //go: comments it does not know, which makes a typo easy to miss. Plutonium directives change what the program means, so a comment that looks like one is never ignored.",
			Spec:    pspec("20.3", "Directives"),
			Example: "//go:noinline\nfunc slow() { ... }",
			Fixed:   "//pu:noinline\nfunc slow() { ... }",
		},
		&Entry{
			Code: DirectiveArgument, Name: "DirectiveArgument",
			Text:    "Some directives take an argument, such as the symbol name in //pu:extern, and the rest take none. This directive was given text it does not take, or lacks the argument it needs, so the compiler would not understand it. To explain why a directive is there, write an ordinary comment on a line of its own.",
			Why:     "Go's //go: directives ignore text they do not expect. Plutonium directives change what the program means, so they are read exactly.",
			Fix:     &Fix{Intro: "Write the directive alone on its line", Code: "// slow path, kept out of line.\n//pu:noinline\nfunc slow() { ... }"},
			Spec:    pspec("20.3", "Directives"),
			Example: "//pu:noinline keeps the profile readable\nfunc slow() { ... }",
			Fixed:   "// Out of line to keep the profile readable.\n//pu:noinline\nfunc slow() { ... }",
		},
	)
}

// whyPanicHandler is why the panic handler is chosen at compile time.
const whyPanicHandler = "A kernel has no operating system to fall back on when it panics, so Plutonium makes the program choose what happens at compile time, and checks the choice: the runtime calls the handler directly, with nothing to register, so it works from the first instruction."

// whyContext is why interrupt context is checked at compile time.
const whyContext = "A kernel that waits in an interrupt handler hangs, often only under load and far from the cause, so Plutonium checks the whole program at compile time instead."
