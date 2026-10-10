## 4. Lexical elements

### 4.1 Comments

Comments serve as program documentation. There are two forms:

1. *Line comments* start with the character sequence `//` and stop at the end of the line.
2. *General comments* start with the character sequence `/*` and stop with the first subsequent character
   sequence `*/`.

A comment cannot start inside a rune or string literal, or inside a comment. A general comment containing
no newlines acts like a space. Any other comment acts like a newline.

**Directives.** A line comment whose first characters are `//pu:`, with no space after the slashes, is a
*directive*. A directive states a fact about the program that the compiler cannot work out for itself:
linkage to assembly and C, hardware state, a binding to the runtime, or an explicit request. The directives
that exist, where each may appear and what each means are defined in section 20.3 (Directives). A
`//pu:` line comment that is not a valid directive in the place where it appears is a compile error, so
that a misspelled directive never silently changes nothing (E2600 to E2603).

```
directive_comment = "//pu:" directive_name [ directive_args ] .
directive_name    = letter { letter | decimal_digit } .
directive_args    = ( " " | "\t" ) { unicode_char } .
```

A comment that is a note, not a directive, starts with `// ` (a space after the slashes) or is a general
comment. `// pu:inline` is a note and `//pu:inline` is a directive.

**Go directives.** A line comment whose first characters are `//go:` is a compile error. Plutonium does
not use Go's directives, and a comment that looks like one but does nothing would be a false friend (section
1.2, Design rules). The error names the `//pu:` directive that does the same job, such as `//pu:build` for
`//go:build` and `//pu:noinline` for `//go:noinline`. It belongs to the family of E2600 (unknown
directive).

```
go_comment = "//go:" { unicode_char } .   /* always an error */
```

```go
//go:noinline // error: use //pu:noinline
func f() {}

//pu:noinline
func g() {}
```

### 4.2 Tokens

Tokens form the vocabulary of the language. There are four classes: *identifiers*, *keywords*, *operators
and punctuation*, and *literals*. White space, formed from spaces (U+0020), horizontal tabs (U+0009),
carriage returns (U+000D) and newlines (U+000A), is ignored except as it separates tokens that would
otherwise combine into a single token. Also, a newline or end of file may trigger the insertion of a
semicolon. While breaking the input into tokens, the next token is the longest sequence of characters that
form a valid token.

A contextual keyword (section 4.6) is an identifier token. The parser gives it the meaning of a keyword in
the positions that section lists.

### 4.3 Semicolons

The formal syntax uses semicolons `;` as terminators in a number of productions. Programs may omit most of
these semicolons using the following two rules:

1. When the input is broken into tokens, a semicolon is automatically inserted into the token stream
   immediately after a line's final token if that token is
   - an identifier (a contextual keyword is an identifier);
   - an integer, floating-point, rune or string literal;
   - one of the keywords `break`, `continue`, `fallthrough` or `return`;
   - one of the operators and punctuation `++`, `--`, `)`, `]` or `}`.
2. To allow complex statements to occupy a single line, a semicolon may be omitted before a closing `)`
   or `}`.

To reflect idiomatic use, code examples in this manual elide semicolons using these rules. The members of
an enum, the variants of a union and the fields of a packed type are separated by inserted semicolons in the
same way:

```go
type Color enum {
	Red // a semicolon is inserted after Red
	Green
	Blue
}
```

### 4.4 Identifiers

Identifiers name program entities such as variables and types. An identifier is a sequence of one or more
letters and digits. The first character in an identifier must be a letter.

```
identifier = letter { letter | unicode_digit } .
```

```
a
_x9
ThisVariableIsExported
αβ
```

Some identifiers are predeclared (section 10.3, Predeclared identifiers). A keyword (section 4.5) is not an
identifier. A contextual keyword (section 4.6) is an identifier that has a second meaning in some
positions.

### 4.5 Keywords

The following keywords are reserved and may not be used as identifiers.

```
break        default      func         interface    select
case         defer        go           map          struct
chan         else         goto         package      switch
const        fallthrough  if           range        type
continue     for          import       return       var
enum         packed
```

The first 25 are the keywords of Go. Plutonium adds `enum` (section 7.12, Enums) and `packed` (section
7.14, `packed` types). Both begin type literals, and reserving them keeps the grammar simple.

The keyword `go` begins no construct. Plutonium starts a task with the `task` statement (section 12.12,
Task statements). `go` stays reserved so that a Go program's `go f()` is a compile error whose message
points to `task`, and so that no variable named `go` can be misread by a Go reader.

```go
go worker(jobs)   // error: go starts no task here; write: task worker(jobs)
```

The keyword `map` begins the map type (section 7.10, Map types). The keyword `case` also appears in packed
unions (section 7.13, Unions), with a meaning described there.

### 4.6 Contextual keywords

A *contextual keyword* is spelled like an identifier. The scanner produces an identifier token for it, and
the parser gives it the meaning of a keyword only in the positions listed here. Everywhere else it is an
ordinary identifier: it may name a variable, a field, a type, a function or a package.

| Word | Is a keyword | Defined in |
|---|---|---|
| `owner` | before a type in a type definition, and before the constraint of a type parameter | section 7.18 (Owner types) |
| `union` | as the type of a type definition when `{` follows it, and after `packed` when something other than `{` follows it | section 7.13 (Unions) |
| `task` | at the start of a statement when the next token is an identifier or `func` | section 12.12 (Task statements) |
| `bits` | as the type of a field of a packed type, when `[` or `{` follows it | section 7.14 (`packed` types) |
| `le`, `be` | directly after `packed`, or after `packed union` | section 7.14, section 7.13 |

`enum` and `packed` are reserved, not contextual (section 4.5). The words `union`, `owner` and `task` are
common identifiers in existing code (set types, ownership fields, job queues), and reserving them would
break that code.

```
EnumType        = "enum" [ Type ] "{" { EnumMember ";" } "}" .
PackedType      = "packed" [ "le" | "be" ] [ Type ] "{" { PackedFieldDecl ";" } "}" .
PackedUnionType = "packed" "union" [ "le" | "be" ] Type "{" TagField ";" { PackedVariant ";" } "}" .
UnionType       = "union" "{" { VariantDecl ";" } "}" .
BitsType        = "bits" "[" Type "," Expression "]" | "bits" "{" { identifier } "}" .
TypeDef         = identifier [ TypeParameters ] [ "owner" ] Type .
TypeParamDecl   = IdentifierList [ "owner" ] TypeConstraint .
TaskStmt        = "task" Expression .
```

The productions above show the syntax that the reserved and contextual words begin. Sections 7.12 (Enums),
7.13 (Unions), 7.14 (`packed` types), 7.18 (Owner types), 10.8 (Type declarations), 10.9 (Type parameter
declarations) and 12.12 (Task statements) define the complete forms and the productions they use.

**`owner`.** In a type definition or a type parameter declaration, `owner` is the keyword when the token
after it is an identifier, `[`, `*`, `(`, `<-`, or one of the keywords `struct`, `interface`, `func`,
`map`, `chan`, `enum` and `packed`. For any other following token, including `.`, `;`, `,`, `]`, `)`
and `=`, it is an identifier that names a type. In particular, `owner [` always begins an owner array
type.

```go
type owner int      // a type named owner
type A owner        // A is defined from the type owner: a semicolon follows
type B owner int    // owner is the keyword: B is an owner type over int
type C owner [4]B   // owner is the keyword: C is an array of owners
```

**`union`.** In a type definition, `union` followed by `{` begins a union type. Followed by anything
else it is an identifier that names a type. After `packed`, `union` begins a packed union unless `{`
follows it, in which case it names the backing type.

**`task`.** At the start of a statement, `task` followed by an identifier or by `func` begins a task
statement. In every other position `task` is an identifier. An expression cannot be followed by an
identifier or by `func`, so no valid statement that starts with the identifier `task` begins this way, and
the rule never changes the meaning of code that uses `task` as a name. The one form the rule gives up is a
parenthesized function: `task (f)(x)` is written `task f(x)`.

```
task worker(jobs)      // a task statement
task func() { }()      // a task statement
task.Run()             // a method call on a variable named task
task = next            // an assignment
task := next           // a short variable declaration
task[i] = v            // an assignment to an element
```

**`bits`, `le` and `be`.** Inside a packed type, `le` and `be` directly after `packed` (or after
`packed union`) set the byte order, and `bits` as the type of a field, followed by `[` or `{`, is the
bit-field type or the flag group. Outside those positions they are identifiers. In particular the package
`pluto/bits` is imported and used as `bits.Set(&x, 3)` as usual.

```go
type ControlReg packed uint32 {
	mode   bits[uint, 4]
	enable bits[bool, 1]
	_      bits[uint, 27]
}
```

### 4.7 Operators and punctuation

The following character sequences represent operators (including assignment operators) and punctuation:

```
+    &     +=    &=     &&    ==    !=    (    )
-    |     -=    |=     ||    <     <=    [    ]
*    ^     *=    ^=     <-    >     >=    {    }
/    <<    /=    <<=    ++    =     :=    ,    ;
%    >>    %=    >>=    --    !     ...   .    :
     &^          &^=          ~
```

Plutonium adds no operators and no punctuation. The tokens `=` and `...` and the brackets `[` and `]` also
appear in the syntax Plutonium adds: `=` introduces a default parameter value (section 10.13) and an enum
member's value (section 7.12), and `[` and `]` enclose the arguments of `owned[T]`, `aligned[T, N]` and the
other generic type forms of chapter 7.

### 4.8 Integer literals

An *integer literal* is a sequence of digits representing an integer constant. An optional prefix sets a
non-decimal base: `0b` or `0B` for binary, `0`, `0o` or `0O` for octal, and `0x` or `0X` for hexadecimal. A
single `0` is considered a decimal zero. In hexadecimal literals, letters `a` through `f` and `A` through
`F` represent values 10 through 15.

For readability, an underscore character `_` may appear after a base prefix or between successive digits;
such underscores do not change the literal's value.

```
int_lit        = decimal_lit | binary_lit | octal_lit | hex_lit .
decimal_lit    = "0" | ( "1" … "9" ) [ [ "_" ] decimal_digits ] .
binary_lit     = "0" ( "b" | "B" ) [ "_" ] binary_digits .
octal_lit      = "0" [ "o" | "O" ] [ "_" ] octal_digits .
hex_lit        = "0" ( "x" | "X" ) [ "_" ] hex_digits .

decimal_digits = decimal_digit { [ "_" ] decimal_digit } .
binary_digits  = binary_digit { [ "_" ] binary_digit } .
octal_digits   = octal_digit { [ "_" ] octal_digit } .
hex_digits     = hex_digit { [ "_" ] hex_digit } .
```

```
42
4_2
0600
0_600
0o600
0O600       // second character is capital letter 'O'
0xBadFace
0xBad_Face
0x_67_7a_2f_cc_40_c6
170141183460469231731687303715884105727
170_141183_460469_231731_687303_715884_105727

_42         // an identifier, not an integer literal
42_         // invalid: _ must separate successive digits
4__2        // invalid: only one _ at a time
0_xBadFace  // invalid: _ must separate successive digits
```

The value of an integer literal is an untyped integer constant (chapter 5, Constants).

### 4.9 Floating-point literals

A *floating-point literal* is a decimal or hexadecimal representation of a floating-point constant.

A decimal floating-point literal consists of an integer part (decimal digits), a decimal point, a
fractional part (decimal digits) and an exponent part (`e` or `E` followed by an optional sign and decimal
digits). One of the integer part or the fractional part may be elided; one of the decimal point or the
exponent part may be elided. An exponent value *exp* scales the mantissa (integer and fractional part) by
10 raised to *exp*.

A hexadecimal floating-point literal consists of a `0x` or `0X` prefix, an integer part (hexadecimal
digits), a radix point, a fractional part (hexadecimal digits) and an exponent part (`p` or `P` followed by
an optional sign and decimal digits). One of the integer part or the fractional part may be elided; the
radix point may be elided as well, but the exponent part is required. This syntax matches the one given in
IEEE 754-2008 section 5.12.3. An exponent value *exp* scales the mantissa (integer and fractional part) by
2 raised to *exp*.

For readability, an underscore character `_` may appear after a base prefix or between successive digits;
such underscores do not change the literal value.

```
float_lit         = decimal_float_lit | hex_float_lit .

decimal_float_lit = decimal_digits "." [ decimal_digits ] [ decimal_exponent ] |
                    decimal_digits decimal_exponent |
                    "." decimal_digits [ decimal_exponent ] .
decimal_exponent  = ( "e" | "E" ) [ "+" | "-" ] decimal_digits .

hex_float_lit     = "0" ( "x" | "X" ) hex_mantissa hex_exponent .
hex_mantissa      = [ "_" ] hex_digits "." [ hex_digits ] |
                    [ "_" ] hex_digits |
                    "." hex_digits .
hex_exponent      = ( "p" | "P" ) [ "+" | "-" ] decimal_digits .
```

```
0.
72.40
072.40       // == 72.40
2.71828
1.e+0
6.67428e-11
1E6
.25
.12345E+5
1_5.         // == 15.0
0.15e+0_2    // == 15.0

0x1p-2       // == 0.25
0x2.p10      // == 2048.0
0x1.Fp+0     // == 1.9375
0X.8p-0      // == 0.5
0X_1FFFP-16  // == 0.1249847412109375
0x15e-2      // == 0x15e - 2 (integer subtraction)

0x.p1        // invalid: mantissa has no digits
1p-2         // invalid: p exponent requires hexadecimal mantissa
0x1.5e-2     // invalid: hexadecimal mantissa requires p exponent
1_.5         // invalid: _ must separate successive digits
1._5         // invalid: _ must separate successive digits
1.5_e1       // invalid: _ must separate successive digits
1.5e_1       // invalid: _ must separate successive digits
1.5e1_       // invalid: _ must separate successive digits
```

The value of a floating-point literal is an untyped floating-point constant (chapter 5, Constants).

**No imaginary literals.** Plutonium has no complex numbers (section 7.2, Numeric types). A decimal,
binary, octal, hexadecimal or floating-point literal immediately followed by the letter `i`, such as `2i`
or `1.5e3i`, is a compile error. The fix is a struct of two floats.

```go
x := 2i // error: Plutonium has no complex numbers
```

### 4.10 Rune literals

A *rune literal* represents a rune constant, an integer value identifying a Unicode code point. A rune
literal is expressed as one or more characters enclosed in single quotes, as in `'x'` or `'\n'`. Within the
quotes, any character may appear except newline and unescaped single quote. A single quoted character
represents the Unicode value of the character itself, while multi-character sequences beginning with a
backslash encode values in various formats.

The simplest form represents the single character within the quotes. Since source text is Unicode
characters encoded in UTF-8, multiple UTF-8-encoded bytes may represent a single integer value. For
instance, the literal `'a'` holds a single byte representing a literal *a*, Unicode U+0061, value `0x61`,
while `'ä'` holds two bytes (`0xc3 0xa4`) representing a literal *a-dieresis*, U+00E4, value `0xe4`.

Several backslash escapes allow arbitrary values to be encoded as ASCII text. There are four ways to
represent the integer value as a numeric constant: `\x` followed by exactly two hexadecimal digits; `\u`
followed by exactly four hexadecimal digits; `\U` followed by exactly eight hexadecimal digits, and a plain
backslash `\` followed by exactly three octal digits. In each case the value of the literal is the value
represented by the digits in the corresponding base.

Although these representations all result in an integer, they have different valid ranges. Octal escapes
must represent a value between 0 and 255 inclusive. Hexadecimal escapes satisfy this condition by
construction. The escapes `\u` and `\U` represent Unicode code points, so within them some values are
illegal, in particular those above `0x10FFFF` and surrogate halves.

After a backslash, certain single-character escapes represent special values:

```
\a   U+0007 alert or bell
\b   U+0008 backspace
\f   U+000C form feed
\n   U+000A line feed or newline
\r   U+000D carriage return
\t   U+0009 horizontal tab
\v   U+000B vertical tab
\\   U+005C backslash
\'   U+0027 single quote  (valid escape only within rune literals)
\"   U+0022 double quote  (valid escape only within string literals)
```

An unrecognized character following a backslash in a rune literal is illegal.

```
rune_lit         = "'" ( unicode_value | byte_value ) "'" .
unicode_value    = unicode_char | little_u_value | big_u_value | escaped_char .
byte_value       = octal_byte_value | hex_byte_value .
octal_byte_value = `\` octal_digit octal_digit octal_digit .
hex_byte_value   = `\` "x" hex_digit hex_digit .
little_u_value   = `\` "u" hex_digit hex_digit hex_digit hex_digit .
big_u_value      = `\` "U" hex_digit hex_digit hex_digit hex_digit
                           hex_digit hex_digit hex_digit hex_digit .
escaped_char     = `\` ( "a" | "b" | "f" | "n" | "r" | "t" | "v" | `\` | "'" | `"` ) .
```

```
'a'
'ä'
'本'
'\t'
'\000'
'\007'
'\377'
'\x07'
'\xff'
'\u12e4'
'\U00101234'
'\''         // rune literal containing single quote character
'aa'         // illegal: too many characters
'\k'         // illegal: k is not recognized after a backslash
'\xa'        // illegal: too few hexadecimal digits
'\0'         // illegal: too few octal digits
'\400'       // illegal: octal value over 255
'\uDFFF'     // illegal: surrogate half
'\U00110000' // illegal: invalid Unicode code point
```

The value of a rune literal is an untyped rune constant (chapter 5, Constants).

### 4.11 String literals

A *string literal* represents a string constant obtained from concatenating a sequence of characters.
There are two forms: raw string literals and interpreted string literals.

*Raw string literals* are character sequences between back quotes, as in `` `foo` ``. Within the quotes,
any character may appear except back quote. The value of a raw string literal is the string composed of the
uninterpreted (implicitly UTF-8-encoded) characters between the quotes; in particular, backslashes have no
special meaning and the string may contain newlines. Carriage return characters (`'\r'`) inside raw string
literals are discarded from the raw string value.

*Interpreted string literals* are character sequences between double quotes, as in `"bar"`. Within the
quotes, any character may appear except newline and unescaped double quote. The text between the quotes
forms the value of the literal, with backslash escapes interpreted as they are in rune literals (except
that `\'` is illegal and `\"` is legal), with the same restrictions. The three-digit octal (`\nnn`) and
two-digit hexadecimal (`\xnn`) escapes represent individual bytes of the resulting string; all other
escapes represent the (possibly multi-byte) UTF-8 encoding of individual characters. Thus inside a string
literal `\377` and `\xFF` each stand for a single byte of value `0xFF`=255, while `ÿ`, `\u00FF`,
`\U000000FF` and `\xc3\xbf` represent the two bytes `0xc3 0xbf` of the UTF-8 encoding of character U+00FF.

**Every string literal is valid UTF-8.** Every string in Plutonium holds valid UTF-8 text (section 7.3,
String types). The bytes of a string literal, after its escapes are interpreted, must therefore form valid
UTF-8, or the literal is a compile error (E2012). A literal that spells a character with several byte
escapes is valid when the bytes together encode that character. Binary data belongs in a `[]byte` or a
byte array, which may hold any bytes.

```
string_lit             = raw_string_lit | interpreted_string_lit .
raw_string_lit         = "`" { unicode_char | newline } "`" .
interpreted_string_lit = `"` { unicode_value | byte_value } `"` .
```

```go
`abc`                // same as "abc"
`\n
\n`                  // same as "\\n\n\\n"
"\n"
"\""                 // same as `"`
"Hello, world!\n"
"日本語"
"\u65e5本\U00008a9e"
"\xff\u00FF"         // error: \xff is not valid UTF-8 (E2012)
"\377"               // error: a lone 0xFF byte is not valid UTF-8 (E2012)
"\uD800"             // illegal: surrogate half
"\U00110000"         // illegal: invalid Unicode code point
```

These examples all represent the same string:

```go
"日本語"                                 // UTF-8 input text
`日本語`                                 // UTF-8 input text as a raw literal
"\u65e5\u672c\u8a9e"                    // the explicit Unicode code points
"\U000065e5\U0000672c\U00008a9e"        // the explicit Unicode code points
"\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e"  // the explicit UTF-8 bytes
```

If the source code represents a character as two code points, such as a combining form involving an accent
and a letter, the result will be an error if placed in a rune literal (it is not a single code point), and
will appear as two code points if placed in a string literal.

The value of a string literal is an untyped string constant (chapter 5, Constants). A string literal is
static and uncounted when it is used as a string value (section 18.8, Strings).

Adapted in part from the Go specification, copyright The Go Authors, CC BY 4.0; see
[Attribution](../language.md#attribution).
