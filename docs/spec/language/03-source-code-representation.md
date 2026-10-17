## 3. Source code representation

Source code is Unicode text encoded in UTF-8. The text is not canonicalized, so a single accented code
point is distinct from the same character constructed from a combining accent and a letter. Those are
treated as two code points. For simplicity, this manual uses the unqualified term *character* to refer to
a Unicode code point in the source text.

Each code point is distinct. For instance, uppercase and lowercase letters are different characters.

Source text that is not valid UTF-8 is an error. So is the NUL character (U+0000) anywhere in the text.

A byte order mark (U+FEFF) is ignored if it is the first code point of the source text. It is an error
anywhere else.

### 3.1 Characters

The following terms denote specific Unicode character categories:

```
newline        = /* the Unicode code point U+000A */ .
unicode_char   = /* an arbitrary Unicode code point except newline */ .
unicode_letter = /* a Unicode code point categorized as "Letter" */ .
unicode_digit  = /* a Unicode code point categorized as "Number, decimal digit" */ .
```

The Unicode Standard defines a set of general categories. Plutonium treats all characters in any of the
Letter categories Lu, Ll, Lt, Lm and Lo as Unicode letters, and those in the Number category Nd as Unicode
digits.

### 3.2 Letters and digits

The underscore character `_` (U+005F) is considered a lowercase letter.

```
letter        = unicode_letter | "_" .
decimal_digit = "0" … "9" .
binary_digit  = "0" | "1" .
octal_digit   = "0" … "7" .
hex_digit     = "0" … "9" | "A" … "F" | "a" … "f" .
```

### 3.3 Source files

A Plutonium source file is a file whose name ends in `.p` and whose content is source text as described in
this chapter. A file holds one package clause, then imports, then declarations (section 14.1, Source file
organization). A package is made of the source files in one directory (chapter 14, Packages).

A file name can select the files of a build. A suffix that names a target or an operating system, such as
`_amd64.p` or `_linux.p`, includes the file only in builds for that target. A file whose name ends in
`_test.p` belongs to the tests of its package and is left out of every other build. A `//pu:build` line
at the start of a file, before the package clause, includes it only when a boolean expression of build tags holds. Section 14.4
(Build constraints) defines these rules and the tags each mode sets.

A source file may contain the directives of section 20.3 (Directives) in line comments that begin
`//pu:`. The directive syntax is in section 4.1 (Comments).

```go
package main

import "pluto/fmt"

func main() int64 {
	fmt.Printf("hello, %s\n", "world")
	return 0
}
```

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
