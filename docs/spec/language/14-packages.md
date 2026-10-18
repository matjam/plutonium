## 14. Packages

Programs are constructed by linking together packages. A package is constructed from one or more source
files that together declare constants, types, variables and functions belonging to the package and which
are accessible in all files of the same package. Those elements may be exported and used in another
package (section 10.4, Exported identifiers).

### 14.1 Source file organization

Each source file consists of a package clause defining the package to which it belongs, followed by a
possibly empty set of import declarations that declare packages whose contents it wishes to use,
followed by a possibly empty set of declarations of functions, types, variables and constants.

```
SourceFile = PackageClause ";" { ImportDecl ";" } { TopLevelDecl ";" } .
```

Source files have the extension `.p` and are UTF-8 text (section 3.3, Source files). A `//pu:build` line
before the package clause applies to the whole file (section 14.4). Every other directive is a comment
on the declaration it applies to (section 4.1, Comments, and section 20.3, Directives).

**Packages and directories.** A package is the set of `.p` files in one directory that the build selects
(section 14.4). All of them must start with the same package clause. Files with different package names
in one directory are an error (E4012), and so is a directory for which the build selects no file
(E4010). Assembly, C and object files in the directory belong to the package too (section 20.4, Assembly
and C). A subdirectory is a different package, not a part of this one. Files named `_test.p` are left out
of every build except a test of their own package; a test file does not form a package of its own, so a
package clause such as `package x_test` in one is an error (E4011).

**Modules.** The programs and packages of a project form a module. The module is the directory that
holds a file named `pluto.mod`, together with everything below it. The first line of that file is
`module` followed by the module path, such as `module example.com/project`. A program that is a single
package needs no module file. The module path may not start with `pluto/` or be `pluto` (E4015), since
those paths name the standard library. A first line that does not have this form is an error (E4014).

**Language version.** Plutonium has one language version, the one this specification describes; its
number is in the header of the specification. A module file records no language version, and no feature of
the language is switched on or off by one.

### 14.2 Package clause

A package clause begins each source file (a file without one is E1009) and defines the package to which the file belongs.

```
PackageClause = "package" PackageName .
PackageName   = identifier .
```

The PackageName must not be the blank identifier.

```go
package math
```

A set of files sharing the same PackageName form the implementation of a package. All files of a package
are in one directory (section 14.1).

A program is built from a package named `main` (section 15.4, Program execution). Building a package
that has another name as a program is an error (E4001). A package named `main` declares the function
`main` (section 10.12, Function declarations), and the program's entry point follows from it.

### 14.3 Import declarations

An import declaration states that the source file containing the declaration depends on functionality
of the imported package (section 15.3, Program initialization) and enables access to exported
identifiers of that package. The import names an identifier (PackageName) to be used for access and an
ImportPath that specifies the package to be imported.

```
ImportDecl = "import" ( ImportSpec | "(" { ImportSpec ";" } ")" ) .
ImportSpec = [ "." | PackageName ] ImportPath .
ImportPath = string_lit .
```

An import path is a string literal, and imports come right after the package clause, before any other declaration; otherwise the import declaration is a syntax error (E1007).

The PackageName is used in qualified identifiers to access exported identifiers of the package within the
importing source file (section 11.2, Qualified identifiers). It is declared in the file block. If the
PackageName is omitted, it defaults to the identifier specified in the package clause of the imported
package. If an explicit period (`.`) appears instead of a name, all the package's exported identifiers
declared in that package's package block are declared in the importing source file's file block and
must be accessed without a qualifier.

**Import paths.** An import path names a package in one of three places.

- A path that is `pluto` or starts with `pluto/` names a package of the standard library, which is part of
  the toolchain (`pluto/fmt`, `pluto/alloc`). A path under `pluto/` that the library does not have is an
  error (E4005). The packages and what they provide are described in the library documentation, not in
  this specification (appendix F, Library names the language relies on, lists the few names the language
  itself depends on).
- A path that starts with the module path of the importing program's module names a package of the module:
  the module path followed by the package's directory, relative to the directory that holds `pluto.mod`.
  If no such directory exists the import is an error (E4006).
- The path `unsafe` names the package of section 20.1 (Package `unsafe`), which the compiler provides.

Any other path is an error. Without a `pluto.mod`, a program is a single package that can import only the
standard library (E4007). A path outside the module and the standard library is E4008: Plutonium has no
package downloads. The import `reflect` is E2005, since the language has no run-time type metadata to
reflect on.

Consider a package with the package clause `package math` that exports the function `Sin`, imported as
`"lib/math"`. This table shows how `Sin` is accessed in files that import the package with the various
kinds of import declaration.

| Import declaration | Local name of `Sin` |
|---|---|
| `import "lib/math"` | `math.Sin` |
| `import m "lib/math"` | `m.Sin` |
| `import . "lib/math"` | `Sin` |

**Dependencies.** An import declaration declares a dependency relation between the importing and the
imported package. It is illegal for a package to import itself, directly or indirectly (E4009). It is
also illegal to import a package without referring to any of its exported identifiers (E0008). To import
a package solely for its side effects (initialization), use the blank identifier as explicit package name:

```go
import _ "pluto/arch/x86"
```

The packages in a program form a directed acyclic graph. Section 15.3 (Program initialization) defines
the order in which their initialization runs.

### 14.4 Build constraints

A build chooses which files of a package belong to it. The choice depends on the **tags** that are set. A
tag is a name. The tags that a build sets are:

| Build mode | Tags |
|---|---|
| `hosted` | `amd64`, `linux`, `hosted` |
| `freestanding` | `amd64`, `none`, `freestanding` |
| `kernel` | `amd64`, `none`, `freestanding`, `kernel` |

The modes and targets are defined in section 20.5 (Build modes and targets). A build can set further tags
by name; they are visible to constraints like the tags above.

**`//pu:build` lines.** A file starts with a `//pu:build` line comment, before the package clause, to
say when it is part of the package. The line holds a boolean expression over tags, written with `&&`,
`||`, `!` and parentheses. The file is part of the package only when the expression is true. A file with
no such line is always part of the package.

```go
//pu:build amd64 && !freestanding

package arch
```

A `//pu:build` line after the package clause is an error (E2602). A `//pu:build` line without an expression is E2603, and an expression that does not parse is E4013. A line comment that begins with `//go:` is E2604 (section 20.3,
Directives), and for `//go:build` the error names `//pu:build`.

**File names.** A file name selects too. Take the name without its extension and without a final `_test`.
If the last, or the second to last, of the parts separated by underscores is one of `amd64`, `arm64`,
`linux` or `none`, the file is part of the package only when that tag is set. `sched_linux.p` belongs to
hosted builds on Linux, `sched_none.p` to builds without an operating system, and `ctx_amd64.s` to builds
for the x86-64 processor. Other words in a file name have no meaning to the build.

Both rules apply: a file is part of the package when its name and its `//pu:build` line both allow it.
Assembly files (`.s`) follow the same rules, with `#pu:build` for the line.

### 14.5 An example package

Here is a complete package that implements a concurrent prime sieve. It prints the first ten primes.

```go
package main

import "pluto/fmt"

// Send the sequence until the caller requests stopping.
func generate(ch chan<- int, done <-chan struct{}) {
	defer close(ch)
	for i := 2; ; i++ {
		select {
		case ch <- i:
		case <-done:
			return
		}
	}
}

// Each task closes only its own output channel.
func filter(src <-chan int, dst chan<- int, prime int, done <-chan struct{}) {
	defer close(dst)
	for {
		select {
		case <-done:
			return
		case i, ok := <-src:
			if !ok {
				return
			}
			if i%prime != 0 {
				select {
				case dst <- i:
				case <-done:
					return
				}
			}
		}
	}
}

func sieve() {
	done := make(chan struct{})
	defer close(done) // explicitly request that all workers stop
	ch := make(chan int)
	task generate(ch, done)
	for n := 0; n < 10; n++ {
		prime := <-ch
		fmt.Println(prime)
		ch1 := make(chan int)
		task filter(ch, ch1, prime, done)
		ch = ch1
	}
}

func main() {
	sieve()
}
```

Returning from `sieve` closes `done`. Each worker can then stop even if its next send would block,
and closes its own output. When `main` returns, the program ends; workers still blocked are not waited for (section 15.4).

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
