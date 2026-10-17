## G. Attribution

Portions of this specification are adapted from *The Go Programming Language Specification* (language
version go1.27), <https://go.dev/ref/spec>, copyright 2009 The Go Authors, used under the Creative
Commons Attribution 4.0 International License (<https://creativecommons.org/licenses/by/4.0/>). The text
has been modified: it is reordered, rewritten and extended to define Plutonium, and many of its rules
differ from Go's. Code examples adapted from it are used under the BSD license in
[LICENSE-go](../LICENSE-go). The adapted material is provided as is, without warranties, as those
licenses state. The Go Authors do not endorse Plutonium.

**What was adapted.** The specification follows the order of the Go specification for everything that Go
also has, so that a reader finds each rule where they expect it. Text that adapts the Go specification
appears in the chapters on notation, source code representation, lexical elements, constants, variables,
types, properties of types and values, blocks, declarations and scope, expressions, statements, built-in
functions, packages, program initialization and execution, errors, run-time panics, concurrency and system considerations, in the introduction, and in appendices B to E. Each such chapter or appendix ends with a line that refers to this appendix.
Chapters and sections for which Go has no counterpart (ownership and memory, most of concurrency, kernel mode, and the other additions listed in appendix A, section A.4) are not
adapted from it.

**What the licenses require.**

- The creator is The Go Authors, and the copyright notice is "Copyright 2009 The Go Authors." The
  adapted prose is licensed under the Creative Commons Attribution 4.0 International License; the license
  text and its disclaimer of warranties are at <https://creativecommons.org/licenses/by/4.0/legalcode>.
  The original is at <https://go.dev/ref/spec>.
- The adapted text has been modified, as stated above. No endorsement by The Go Authors is implied.
- The code examples adapted from the Go specification are used under the 3-clause BSD license that
  covers the Go repository. Its notice, its conditions and its disclaimer are kept in
  [LICENSE-go](../LICENSE-go), which travels with this specification wherever it is redistributed.
- The terms that apply to Plutonium's own text are not affected by the license of the adapted material.
  No term of this specification limits what others may do with the adapted material under the licenses
  above.
