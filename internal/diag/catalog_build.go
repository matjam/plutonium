package diag

// The explanations of errors in finding and loading packages, assembling
// and linking, and in the compiler itself.

func init() {
	register(
		&Entry{
			Code: NotMainPackage, Name: "NotMainPackage",
			Text: "pluto build and pluto run build a program, and a program starts in package main, at func main. The directory given holds a different package, which can only be imported by a program or tested with pluto test.",
			Spec: pspec("14.2", "Package clause"),
		},
		&Entry{
			Code: NotPackageDir, Name: "NotPackageDir",
			Text: "pluto test takes a directory holding a package, or the import path of a standard library package such as pluto/strings. This path is neither.",
		},
		&Entry{
			Code: TestDirName, Name: "TestDirName",
			Text: "A package outside any module is imported by its tests under the name of its directory. That name is reserved here, since main and pluto/... already mean something else. Rename the directory, or add a pluto.mod file to make the package part of a module.",
			Fix:  &Fix{Intro: "Declare a module in the directory above", Code: "module example.com/project"},
		},
		&Entry{
			Code: TestMainPackage, Name: "TestMainPackage",
			Text: "pluto test builds a program of its own that calls the tests, so it cannot test package main, which already is a program. Move the code under test into a package of its own, and import it from main.",
		},
		&Entry{
			Code: StdPackageMissing, Name: "StdPackageMissing",
			Text: "Import paths that start with pluto/ name packages of the standard library, and this one does not exist. Check the spelling. Go's packages have Plutonium versions under pluto/, such as pluto/fmt for fmt, but not every Go package has one.",
			Spec: pspec("14.3", "Import declarations"),
		},
		&Entry{
			Code: PackageNotFound, Name: "PackageNotFound",
			Text: "The import path is inside this program's module, as declared by its pluto.mod file, but no directory exists at the matching place. An import path is the module path followed by the package's directory, relative to the directory holding pluto.mod.",
		},
		&Entry{
			Code: ImportNoModule, Name: "ImportNoModule",
			Text: "Without a pluto.mod file, a program is a single package that can import only the standard library (paths under pluto/). To split a program into packages, add a pluto.mod file in its top directory that names the module, and import packages by the module path followed by their directory.",
			Fix:  &Fix{Intro: "Create pluto.mod in the top directory", Code: "module example.com/project\n\n// and import a package in ./util as:\nimport \"example.com/project/util\""},
		},
		&Entry{
			Code: ImportOutside, Name: "ImportOutside",
			Text: "A program can import the standard library (paths under pluto/) and packages of its own module (paths under the module path in pluto.mod). This import path is neither, and Plutonium has no package downloads.",
		},
		&Entry{
			Code: ImportCycle, Name: "ImportCycle",
			Text: "Packages import each other in a circle. A package must be compiled after every package it imports, so a circle has no order that works. Move the code both packages need into a third package that both import.",
			Spec: pspec("14.3", "Import declarations"),
		},
		&Entry{
			Code: NoSourceFiles, Name: "NoSourceFiles",
			Text: "The package's directory has no .p files for this build. Either it has none at all, or every file is left out by a //pu:build line or a file name suffix such as _linux.p for the target and mode being built.",
			Spec: pspec("14.1", "Source file organization"),
		},
		&Entry{
			Code: ExternalTestPkg, Name: "ExternalTestPkg",
			Text: "Test files in Plutonium belong to the package they test. A _test.p file in a package named x_test, which Go allows, is not supported. Declare the test in the package itself.",
		},
		&Entry{
			Code: MixedPackages, Name: "MixedPackages",
			Text: "All the .p files in one directory must belong to the same package, so they must all start with the same package clause.",
			Spec: pspec("14.2", "Package clause"),
		},
		&Entry{
			Code: BadConstraint, Name: "BadConstraint",
			Text: "A //pu:build line is a boolean expression over build tags, using && (and), || (or), ! (not) and parentheses, such as //pu:build amd64 && !kernel. This one does not parse.",
			Spec: pspec("14.4", "Build constraints"),
		},
		&Entry{
			Code: BadModFile, Name: "BadModFile",
			Text: "The first line of a pluto.mod file must be the word module followed by the module path.",
			Fix:  &Fix{Intro: "Write the first line as", Code: "module example.com/project"},
		},
		&Entry{
			Code: ReservedModule, Name: "ReservedModule",
			Text: "Import paths under pluto/ belong to the standard library, so a module cannot use one as its path. Choose another, such as a domain you control followed by the project name.",
		},
		&Entry{
			Code: NoMainFunc, Name: "NoMainFunc",
			Text: "A program starts by calling func main in package main, and this package does not declare one.",
			Fix:  &Fix{Intro: "Add a main function", Code: "func main() {\n}"},
			Spec: pspec("15.4", "Program execution"),
		},
		&Entry{
			Code: ReadPackage, Name: "ReadPackage",
			Text: "The compiler could not read the package's directory. Check that the path exists and that you have permission to read it.",
		},
		&Entry{
			Code: BadTestSignature, Name: "BadTestSignature",
			Text: "pluto test runs the functions in _test.p files whose names start with Test or Benchmark. Such a function must take exactly one parameter, a *testing.T for a test or a *testing.B for a benchmark, and return nothing.",
			Fix:  &Fix{Intro: "Declare it like this", Code: "func TestParse(t *testing.T) {\n\t...\n}"},
		},

		// Assembling and linking.
		&Entry{
			Code: UndefinedSymbol, Name: "UndefinedSymbol",
			Text: "The linker joins the compiled program with its assembly, C and object files, and found a symbol that is used but defined nowhere. This happens when a function is declared without a body but no .s, .c or .o file in its package defines it, or when //pu:extern names a C function that is not linked in.",
			Spec: pspec("20.4", "Assembly and C"),
		},
		&Entry{
			Code: RelocOverflow, Name: "RelocOverflow",
			Text: "The compiled code reaches every symbol with a 32-bit offset, so code and data must all lie within 2 GiB of each other. In hosted builds they are near address 0; in kernel mode they are in the top 2 GiB of the address space. The linker placed a symbol outside that range, usually because a linker script puts a section somewhere else.\nIn kernel mode, link the image at or above 0xFFFFFFFF80000000, as the default kernel linker script does, and keep the .percpu section at address 0.",
			Spec: pspec("20.6", "Kernel mode"),
		},
		&Entry{
			Code: MultipleDefs, Name: "MultipleDefs",
			Text: "Two objects define the same symbol. This happens when a .s, .c or .o file defines a function that Plutonium code also defines, when a //pu:export name is also defined in C, or when two C files define the same global.",
			Spec: pspec("20.4", "Assembly and C"),
		},
		&Entry{
			Code: AssemblerError, Name: "AssemblerError",
			Text: "The GNU assembler rejected a .s file of the program. Its messages follow, with the file's own name and line. Assembly files use GNU assembler syntax; Plutonium functions are referred to by their import path and name, in quotes, such as \"pluto/syscall.Syscall\".",
			Spec: pspec("20.4", "Assembly and C"),
		},
		&Entry{
			Code: CCompilerError, Name: "CCompilerError",
			Text: "The C compiler rejected a .c file of the program. Its messages follow. C files are compiled freestanding: there is no C library, so headers such as stdio.h are not available.",
			Spec: pspec("20.4", "Assembly and C"),
		},
		&Entry{
			Code: LinkFailed, Name: "LinkFailed",
			Text: "The linker failed. Its messages follow. If a linker script was given with --ldscript, check that it exists and places every section the program has.",
			Spec: pspec("20.5", "Build modes and targets"),
		},
		&Entry{
			Code: PerCPUNotAtZero, Name: "PerCPUNotAtZero",
			Text: "Per-CPU variables are reached at their offset from the GS base, which only works if the linker places the .percpu section at address 0, with the Plutonium object first. The linker script given puts it elsewhere.",
			Spec: pspec("20.9", "Per-CPU variables"),
		},
		&Entry{
			Code: ToolNotFound, Name: "ToolNotFound",
			Text: "Plutonium uses the system's GNU assembler (as), linker (ld) and, for .c files, C compiler (cc, or $CC). This one is not installed or not on the PATH. On most Linux systems the binutils package provides as and ld.",
		},

		// The compiler itself.
		&Entry{
			Code: InternalError, Name: "InternalError",
			Text: "The compiler failed on a program that passed type checking. This is a bug in the compiler, not in your program. Please report it with the smallest program that shows it. Changing the code near the position shown may work around it.",
		},
	)
}
