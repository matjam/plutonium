package diag

// The codes of errors found outside the type checker. The type checker's
// codes are in internal/types2/errors and become Codes with CheckerCode.
// Every code needs a catalog entry; TestCatalogComplete checks.

// Syntax errors, found by the parser and scanner.
const (
	SyntaxError        Code = "E1000" // any syntax error without a code of its own
	UnexpectedToken    Code = "E1001" // "expected X, found Y"
	UnclosedBlock      Code = "E1002" // the file ends inside a { ... } block
	StmtOutsideFunc    Code = "E1003" // a statement where a declaration must be
	MissingSemicolon   Code = "E1004" // two statements run together on one line
	MissingOperand     Code = "E1005" // an operator or keyword where a value must be
	BraceOnNextLine    Code = "E1006" // the { of a block on the line after if, for, func or switch
	BadImportDecl      Code = "E1007" // a malformed or misplaced import
	BadLiteral         Code = "E1008" // a malformed string, rune or number
	MissingPackage     Code = "E1009" // a file that does not start with a package clause
	BadDefaultSyntax   Code = "E1010" // "a, b T = x": a default after several names
	IfaceMethodTParams Code = "E1011" // an interface method with type parameters
)

// Plutonium rules checked while compiling, after type checking.
const (
	NotYetSupported   Code = "E2500" // a construct the compiler cannot compile yet
	ExternWithBody    Code = "E2510" // a //pu:extern function with a body
	BadCSignature     Code = "E2511" // a //pu:extern or //pu:export signature C cannot express
	DuplicateExport   Code = "E2512" // two functions exported under one symbol
	GenericExport     Code = "E2513" // a generic function with //pu:export
	KernelFloat       Code = "E2520" // floating point in kernel mode outside //pu:fpu
	DeferInLoop       Code = "E2530" // defer inside a loop
	TooManyDefers     Code = "E2531" // more than 64 defers in one function
	DeferBuiltin      Code = "E2532" // defer of a builtin such as copy
	AllocatingConv    Code = "E2540" // string to []byte, []byte to string, and similar
	StringConcat      Code = "E2541" // + on strings that are not constants
	ErrorsNewValue    Code = "E2542" // errors.New used other than by calling it
	PerCPUInit        Code = "E2550" // a per-CPU variable with a non-constant initialiser
	PerCPUMode        Code = "E2551" // //pu:percpu outside kernel mode
	PerCPUSection     Code = "E2552" // //pu:percpu with //pu:section
	StackDirective    Code = "E2560" // a malformed //pu:stack, or one not on main in a hosted build
	InterruptDecl     Code = "E2570" // a //pu:interrupt function that cannot be a handler
	InterruptSig      Code = "E2571" // a handler with the wrong parameters or results
	HandlerAddrArg    Code = "E2572" // x86.HandlerAddr of something other than a handler
	InterruptHandlerV Code = "E2573" // a handler called, or used as a value
	NoPanicHandler    Code = "E2580" // a freestanding or kernel build without a //pu:panic function
	DupPanicHandler   Code = "E2581" // more than one //pu:panic function
	PanicHandlerDecl  Code = "E2582" // a //pu:panic function that cannot be the panic handler
	PanicHandlerRet   Code = "E2583" // a panic handler that can return
	ContextWaits      Code = "E2584" // an interrupt handler or the panic handler can reach code that waits
	ContextSpawns     Code = "E2585" // an interrupt handler or the panic handler can reach a go statement

	// Puroutines and the scheduler.
	GoCapturingLiteral Code = "E2590" // go of a function literal that captures variables
	GoBuiltin          Code = "E2591" // go of a builtin function
	BadScheduler       Code = "E2592" // a //pu:scheduler variable that cannot be the scheduler, or two of them
	NoScheduler        Code = "E2593" // a program that needs a scheduler and binds none
	GoFrameClosure     Code = "E2594" // go of, or with an argument that is, a closure whose record is in the frame
)

// Finding and loading packages.
const (
	NotMainPackage    Code = "E4001" // building a package that is not package main
	NotPackageDir     Code = "E4002" // pluto test of a path that is not a directory
	TestDirName       Code = "E4003" // testing a package outside a module from a reserved directory name
	TestMainPackage   Code = "E4004" // pluto test of package main
	StdPackageMissing Code = "E4005" // a pluto/ import that is not in the standard library
	PackageNotFound   Code = "E4006" // an import inside the module with no directory
	ImportNoModule    Code = "E4007" // a non-standard import without a pluto.mod
	ImportOutside     Code = "E4008" // an import outside the module
	ImportCycle       Code = "E4009" // packages that import each other
	NoSourceFiles     Code = "E4010" // a package with no .p files for this build
	ExternalTestPkg   Code = "E4011" // a _test.p file in package x_test
	MixedPackages     Code = "E4012" // files of different packages in one directory
	BadConstraint     Code = "E4013" // a //pu:build line that does not parse
	BadModFile        Code = "E4014" // a pluto.mod whose first line is not "module path"
	ReservedModule    Code = "E4015" // a module path under pluto/
	NoMainFunc        Code = "E4016" // package main without func main
	ReadPackage       Code = "E4017" // a package directory that cannot be read
	BadTestSignature  Code = "E4018" // a TestXxx or BenchmarkXxx function with the wrong signature
)

// Assembling, compiling C, and linking.
const (
	UndefinedSymbol Code = "E5001" // the linker found no definition of a symbol
	RelocOverflow   Code = "E5002" // an address does not fit the code model
	MultipleDefs    Code = "E5003" // two definitions of one symbol
	AssemblerError  Code = "E5004" // the assembler rejected a .s file
	CCompilerError  Code = "E5005" // the C compiler rejected a .c file
	LinkFailed      Code = "E5006" // any other linker failure
	PerCPUNotAtZero Code = "E5007" // a kernel image whose .percpu section is not at address 0
	ToolNotFound    Code = "E5008" // as, ld or cc is not installed
)

// Ownership and lifetimes: E3000-E3999, reserved for the ownership and
// escape checks.

// Errors in the compiler itself.
const (
	InternalError Code = "E9001" // a bug in the compiler
)
