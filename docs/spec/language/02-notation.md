## 2. Notation

The syntax is specified using a variant of Extended Backus-Naur Form (EBNF):

```
Syntax      = { Production } .
Production  = production_name "=" [ Expression ] "." .
Expression  = Term { "|" Term } .
Term        = Factor { Factor } .
Factor      = production_name | token [ "…" token ] | Group | Option | Repetition .
Group       = "(" Expression ")" .
Option      = "[" Expression "]" .
Repetition  = "{" Expression "}" .
```

Productions are expressions constructed from terms and the following operators, in increasing precedence:

```
|   alternation
()  grouping
[]  option (0 or 1 times)
{}  repetition (0 to n times)
```

Lowercase production names identify lexical (terminal) tokens. Non-terminals are in CamelCase. Lexical
tokens are enclosed in double quotes (`"like this"`) or back quotes.

The form `a … b` represents the set of characters from `a` through `b` as alternatives. The horizontal
ellipsis `…` is also used elsewhere in this manual to denote enumerations or code snippets that are not
further specified. The character `…` (as opposed to the three characters `...`) is not a token of the
language.

**Where productions are given.** Each chapter that defines syntax gives its productions, and each
production is defined in one place: the lexical tokens in chapters 3 and 4, the types in chapter 7
(Types), declarations in chapter 10 (Declarations and scope), expressions in chapter 11 (Expressions),
statements in chapter 12 (Statements) and packages in chapter 14 (Packages). A word that is reserved or
contextual appears in a production as a quoted token like any other, for example `"enum"` or `"owner"`.
Section 4.6 (Contextual keywords) says where a contextual word is a keyword and where it is an ordinary
identifier. Appendix E (Grammar) collects all productions.

**Other notation in this manual.**

- Code and tokens are set in `monospace`.
- In examples, a comment that begins `error`, `illegal` or `invalid` marks a line that is a compile
  error, and a comment that begins `panics` or `causes a run-time panic` marks a line that panics when it
  runs. Section 1.3 (Relation to Go, and how to read this specification) says how the other lines of an
  example are read.
- A reference such as "section 11.12" names a section of this manual by number.

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
