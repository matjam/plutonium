## E. Grammar

This appendix collects the productions of the chapters, in the notation of chapter 2 (Notation). Each
production is defined in exactly one chapter, and the section that defines it is named beside it. Where a
production here and the text of a chapter seem to differ, the chapter holds. The start symbol is
`SourceFile` (section 14.1, Source file organization).

Lowercase names are lexical tokens (chapters 3 and 4). A contextual keyword (`owner`, `union`, `task`, `or`,
`bits`, `le`, `be`) is written as a quoted token; it is an identifier token that the parser treats as a
keyword only in the positions of section 4.6 (Contextual keywords). Semicolons in the productions may be
omitted in the source as section 4.3 (Semicolons) describes.

### E.1 Characters and tokens

From section 3.1 (Characters):

```
newline        = /* the Unicode code point U+000A */ .
unicode_char   = /* an arbitrary Unicode code point except newline */ .
unicode_letter = /* a Unicode code point categorized as "Letter" */ .
unicode_digit  = /* a Unicode code point categorized as "Number, decimal digit" */ .
```

From section 3.2 (Letters and digits):

```
letter        = unicode_letter | "_" .
decimal_digit = "0" … "9" .
binary_digit  = "0" | "1" .
octal_digit   = "0" … "7" .
hex_digit     = "0" … "9" | "A" … "F" | "a" … "f" .
```

From section 4.1 (Comments):

```
directive_comment = "//pu:" directive_name [ directive_args ] .
directive_name    = letter { letter | decimal_digit } .
directive_args    = ( " " | "\t" ) { unicode_char } .
go_comment = "//go:" { unicode_char } .   /* always an error */
```

From section 4.4 (Identifiers):

```
identifier = letter { letter | unicode_digit } .
```

From section 4.8 (Integer literals):

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

From section 4.9 (Floating-point literals):

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

From section 4.10 (Rune literals):

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

From section 4.11 (String literals):

```
string_lit             = raw_string_lit | interpreted_string_lit .
raw_string_lit         = "`" { unicode_char | newline } "`" .
interpreted_string_lit = `"` { unicode_value | byte_value } `"` .
```

### E.2 Types

From section 7 (Types):

```
Type        = TypeName [ TypeArgs ] | TypeLit | "(" Type ")" .
TypeName    = identifier | QualifiedIdent .
TypeArgs    = "[" TypeArgList [ "," ] "]" .
TypeArgList = TypeArg { "," TypeArg } .
TypeArg     = Type | Expression .
TypeLit     = ArrayType | StructType | PointerType | FunctionType | InterfaceType |
              SliceType | MapType | ChannelType .
DefinedType = [ "owner" ] ( Type | EnumType | UnionType | PackedType | PackedUnionType ) .
```

From section 7.4 (Array types):

```
ArrayType   = "[" ArrayLength "]" ElementType .
ArrayLength = Expression .
ElementType = Type .
```

From section 7.5 (Slice types):

```
SliceType = "[" "]" ElementType .
```

From section 7.6 (Struct types):

```
StructType    = "struct" "{" { FieldDecl ";" } "}" .
FieldDecl     = (IdentifierList Type | EmbeddedField) [ Tag ] .
EmbeddedField = [ "*" ] TypeName [ TypeArgs ] .
Tag           = string_lit .
```

From section 7.7 (Pointer types):

```
PointerType = "*" BaseType .
BaseType    = Type .
```

From section 7.8 (Function types):

```
FunctionType  = "func" Signature .
Signature     = Parameters [ Result ] .
Result        = Parameters | Type .
Parameters    = "(" [ ParameterList [ "," ] ] ")" .
ParameterList = ParameterDecl { "," ParameterDecl } .
ParameterDecl = [ IdentifierList ] [ "..." ] Type [ DefaultValue ] .
```

From section 7.9 (Interface types):

```
InterfaceType  = "interface" "{" { InterfaceElem ";" } "}" .
InterfaceElem  = MethodElem | TypeElem .
MethodElem     = MethodName Signature .
MethodName     = identifier .
TypeElem       = TypeTerm { "|" TypeTerm } .
TypeTerm       = Type | UnderlyingType .
UnderlyingType = "~" Type .
```

From section 7.10 (Map types):

```
MapType = "map" "[" KeyType "]" ElementType .
KeyType = Type .
```

From section 7.11 (Channel types):

```
ChannelType = ( "chan" | "chan" "<-" | "<-" "chan" ) ElementType .
```

From section 7.12 (Enums):

```
EnumType   = "enum" [ Type ] "{" { EnumMember ";" } "}" .
EnumMember = identifier [ "=" Expression ] .
```

From section 7.13 (Unions):

```
UnionType       = "union" "{" { VariantDecl ";" } "}" .
VariantDecl     = identifier { "," identifier } Type | identifier .
PackedUnionType = "packed" "union" [ "le" | "be" ] Type "{" TagField ";" { PackedVariant ";" } "}" .
TagField        = identifier PackedFieldType .
PackedVariant   = identifier "case" ExpressionList "{" { PackedFieldDecl ";" } "}" .
```

From section 7.14 (`packed` types):

```
PackedType      = "packed" [ "le" | "be" ] [ Backing ] "{" { PackedFieldDecl ";" } "}" .
Backing         = Type .
PackedFieldDecl = ( identifier | "_" ) PackedFieldType .
PackedFieldType = BitField | BitGroup | Type .
BitField        = "bits" "[" Type "," Expression "]" .
BitGroup        = "bits" "{" identifier { identifier } "}" .
```

### E.3 Blocks and declarations

From section 9 (Blocks):

```
Block         = "{" StatementList "}" .
StatementList = { Statement ";" } .
```

From section 10 (Declarations and scope):

```
Declaration  = ConstDecl | TypeDecl | VarDecl .
TopLevelDecl = Declaration | FunctionDecl | MethodDecl .
```

From section 10.6 (Constant declarations):

```
ConstDecl      = "const" ( ConstSpec | "(" { ConstSpec ";" } ")" ) .
ConstSpec      = IdentifierList [ [ Type ] "=" ExpressionList ] .
IdentifierList = identifier { "," identifier } .
ExpressionList = Expression { "," Expression } .
```

From section 10.8 (Type declarations):

```
TypeDecl  = "type" ( TypeSpec | "(" { TypeSpec ";" } ")" ) .
TypeSpec  = AliasDecl | TypeDef .
AliasDecl = identifier [ TypeParameters ] "=" Type .
TypeDef   = identifier [ TypeParameters ] DefinedType .
```

From section 10.9 (Type parameter declarations):

```
TypeParameters = "[" TypeParamList [ "," ] "]" .
TypeParamList  = TypeParamDecl { "," TypeParamDecl } .
TypeParamDecl  = IdentifierList [ "owner" ] TypeConstraint .
TypeConstraint = TypeElem .
```

From section 10.10 (Variable declarations):

```
VarDecl = "var" ( VarSpec | "(" { VarSpec ";" } ")" ) .
VarSpec = IdentifierList ( Type [ "=" ExpressionList ] | "=" ExpressionList ) .
```

From section 10.11 (Short variable declarations):

```
ShortVarDecl = IdentifierList ":=" ExpressionList .
```

From section 10.12 (Function declarations):

```
FunctionDecl = "func" FunctionName [ TypeParameters ] Signature [ FunctionBody ] .
FunctionName = identifier .
FunctionBody = Block .
```

From section 10.13 (Default parameter values):

```
DefaultValue = "=" Expression .
```

From section 10.14 (Method declarations):

```
MethodDecl = "func" Receiver MethodName [ TypeParameters ] Signature [ FunctionBody ] .
Receiver   = Parameters .
```

### E.4 Expressions

From section 11.1 (Operands):

```
Operand     = Literal | OperandName [ TypeArgs ] | "(" Expression ")" .
Literal     = BasicLit | CompositeLit | FunctionLit .
BasicLit    = int_lit | float_lit | rune_lit | string_lit .
OperandName = identifier | QualifiedIdent .
```

From section 11.2 (Qualified identifiers):

```
QualifiedIdent = PackageName "." identifier .
```

From section 11.3 (Composite literals):

```
CompositeLit = LiteralType LiteralValue .
LiteralType  = StructType | ArrayType | "[" "..." "]" ElementType |
               SliceType | MapType | TypeName [ TypeArgs ] .
LiteralValue = "{" [ ElementList [ "," ] ] "}" .
ElementList  = KeyedElement { "," KeyedElement } .
KeyedElement = [ Key ":" ] Element .
Key          = FieldName | Expression | LiteralValue .
FieldName    = identifier .
Element      = Expression | LiteralValue .
```

From section 11.4 (Function literals):

```
FunctionLit = "func" Signature FunctionBody .
```

From section 11.6 (Primary expressions):

```
PrimaryExpr   = Operand |
                Conversion |
                MethodExpr |
                PrimaryExpr Selector |
                PrimaryExpr TypeArgs |
                PrimaryExpr Index |
                PrimaryExpr Slice |
                PrimaryExpr TypeAssertion |
                PrimaryExpr Arguments .
Selector      = "." identifier .
Index         = "[" Expression [ "," ] "]" .
Slice         = "[" [ Expression ] ":" [ Expression ] "]" |
                "[" [ Expression ] ":" Expression ":" Expression "]" .
TypeAssertion = "." "(" Type ")" .
Arguments     = "(" [ ( ExpressionList | Type [ "," ExpressionList ] ) [ "..." ] [ "," ] ] ")" .
```

From section 11.8 (Method expressions):

```
MethodExpr   = ReceiverType "." MethodName .
ReceiverType = Type .
```

From section 11.18 (Operators):

```
Expression = UnaryExpr | Expression binary_op Expression .
UnaryExpr  = PrimaryExpr | unary_op UnaryExpr .
binary_op  = "||" | "&&" | rel_op | add_op | mul_op .
rel_op     = "==" | "!=" | "<" | "<=" | ">" | ">=" .
add_op     = "+" | "-" | "|" | "^" .
mul_op     = "*" | "/" | "%" | "<<" | ">>" | "&" | "&^" .
unary_op   = "+" | "-" | "!" | "^" | "*" | "&" | "<-" .
```

From section 11.24 (Conversions):

```
Conversion = Type "(" Expression [ "," ] ")" .
```

From section 11.27 (Or clauses):

```
Call     = PrimaryExpr Arguments .
OrStmt   = [ ExpressionList "=" | IdentifierList ":=" ] Call "or" OrClause .
OrClause = Block | ReturnStmt | BreakStmt | ContinueStmt | GotoStmt | Expression .
```

### E.5 Statements

From section 12 (Statements):

```
Statement  = Declaration | LabeledStmt | SimpleStmt |
             TaskStmt | ReturnStmt | BreakStmt | ContinueStmt | GotoStmt |
             FallthroughStmt | Block | IfStmt | SwitchStmt | SelectStmt | ForStmt |
             DeferStmt .
SimpleStmt = EmptyStmt | ExpressionStmt | SendStmt | IncDecStmt | Assignment | ShortVarDecl | OrStmt .
```

From section 12.2 (Empty statements):

```
EmptyStmt = .
```

From section 12.3 (Labeled statements):

```
LabeledStmt = Label ":" Statement .
Label       = identifier .
```

From section 12.4 (Expression statements):

```
ExpressionStmt = Expression .
```

From section 12.5 (Send statements):

```
SendStmt = Channel "<-" Expression .
Channel  = Expression .
```

From section 12.6 (IncDec statements):

```
IncDecStmt = Expression ( "++" | "--" ) .
```

From section 12.7 (Assignment statements):

```
Assignment = ExpressionList assign_op ExpressionList .
assign_op  = [ add_op | mul_op ] "=" .
```

From section 12.8 (If statements):

```
IfStmt = "if" [ SimpleStmt ";" ] Expression Block [ "else" ( IfStmt | Block ) ] .
```

From section 12.9 (Switch statements):

```
SwitchStmt = ExprSwitchStmt | TypeSwitchStmt .
ExprSwitchStmt = "switch" [ SimpleStmt ";" ] [ Expression ] "{" { ExprCaseClause } "}" .
ExprCaseClause = ExprSwitchCase ":" StatementList .
ExprSwitchCase = "case" ExpressionList | "default" .
TypeSwitchStmt  = "switch" [ SimpleStmt ";" ] TypeSwitchGuard "{" { TypeCaseClause } "}" .
TypeSwitchGuard = [ identifier ":=" ] PrimaryExpr "." "(" "type" ")" .
TypeCaseClause  = TypeSwitchCase ":" StatementList .
TypeSwitchCase  = "case" TypeList | "default" .
TypeList        = Type { "," Type } .
```

From section 12.10 (For statements):

```
ForStmt   = "for" [ Condition | ForClause | RangeClause ] Block .
Condition = Expression .
ForClause = [ InitStmt ] ";" [ Condition ] ";" [ PostStmt ] .
InitStmt  = SimpleStmt .
PostStmt  = SimpleStmt .
RangeClause = [ ExpressionList "=" | IdentifierList ":=" ] "range" Expression .
```

From section 12.12 (Task statements):

```
TaskStmt = "task" Expression .
```

From section 12.13 (Select statements):

```
SelectStmt = "select" "{" { CommClause } "}" .
CommClause = CommCase ":" StatementList .
CommCase   = "case" ( SendStmt | RecvStmt ) | "default" .
RecvStmt   = [ ExpressionList "=" | IdentifierList ":=" ] RecvExpr .
RecvExpr   = Expression .
```

From section 12.14 (Return statements):

```
ReturnStmt = "return" [ ExpressionList ] .
```

From section 12.15 (Break statements):

```
BreakStmt = "break" [ Label ] .
```

From section 12.16 (Continue statements):

```
ContinueStmt = "continue" [ Label ] .
```

From section 12.17 (Goto statements):

```
GotoStmt = "goto" Label .
```

From section 12.18 (Fallthrough statements):

```
FallthroughStmt = "fallthrough" .
```

From section 12.19 (Defer statements):

```
DeferStmt = "defer" Expression .
```

### E.6 Packages

From section 14.1 (Source file organization):

```
SourceFile = PackageClause ";" { ImportDecl ";" } { TopLevelDecl ";" } .
```

From section 14.2 (Package clause):

```
PackageClause = "package" PackageName .
PackageName   = identifier .
```

From section 14.3 (Import declarations):

```
ImportDecl = "import" ( ImportSpec | "(" { ImportSpec ";" } ")" ) .
ImportSpec = [ "." | PackageName ] ImportPath .
ImportPath = string_lit .
```

### E.7 The predeclared type forms

The predeclared type constructors are written as a type name with type arguments, `TypeName TypeArgs`
(chapter 7, Types), and are not separate productions of the grammar. Since they are predeclared
identifiers, a program may declare the same names for its own types (section 10.3, Predeclared
identifiers). Their forms are:

```
VolatileType = "volatile" "[" Type "]" .                       /* section 7.15 */
AlignedType  = "aligned" "[" Type "," Expression "]" .         /* section 7.16 */
RingType     = "ring" "[" Type "," Expression "]" .            /* section 7.17 */
BoxType      = ( "owned" | "shared" | "synced" ) "[" Type "]" . /* section 7.19 */
WeakType     = "weak" "[" Type "]" .                           /* section 7.19 */
```

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
