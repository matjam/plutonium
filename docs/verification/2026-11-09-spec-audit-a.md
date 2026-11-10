# Spec audit A: chapters 01 to 12, appendices A to G

Source: /home/matjam/src/plutonium/.claude/worktrees/coord (origin/main), docs/spec/language/.
Read-only. Every finding below was checked with grep across the whole language spec.

Counts: high 1, medium 2, low 5.

---

## A1. A view of a counted local can outlive the local's block

- Kind: safety hole.
- Severity: high.
- Changes a promise: yes. It narrows what Go-style code may do with `&local` across blocks. The user decides.

Sections and quotes:

- 18.3, E3313: a view of a local owner may not be used after the owner is "moved, dropped or given a new value". The rule names owners only.
- 18.2: "The frame is reclaimed when the function returns." So `&s` is a frame view that is valid until return.
- 18.8: a string is released "at the end of a variable's scope". Chapter 9 also runs drops at block end.
- 18.8 promises: "nothing frees a string's bytes while a copy can still reach them".
- 18.3 (about line 128): "A view obtained through a counted reference held in a local variable is bounded by that reference." This bounds views made through the reference. It does not bound a view of the variable itself (`&s`).

Example:

```go
var p *string
{
	s := strconv.Itoa(12345) // counted string
	p = &s                   // frame view; frame lives until return
}                            // s released here: its bytes may be freed
println(*p)                  // reads a released string header
```

No rule in 18.3 (E3001 to E3006, E3313, E3314) rejects the last line. The frame slot still exists, but the reference count was already dropped. The same holds for `shared[T]`, maps, channels and any local with drop glue that is not an owner.

Proposed resolution: extend E3313 to every local whose type has drop glue (owners and counted types): a view of it may not be used after its scope ends. Or say once, in 18.2, that a block-scoped variable's storage is dead after its block, so a view of it is bounded by the block. The first option is smaller and matches the goal "Rust-level safety without annotations": the compiler already tracks block ends for drops, so no annotation is needed.

---

## A2. `owner` as a type parameter name is ambiguous

- Kind: contradiction between prose and grammar.
- Severity: medium.
- Changes a promise: no.

Sections and quotes:

- 4.6: "In a type definition or a type parameter declaration, `owner` is the keyword when the token after it is an identifier".
- 10.9 and appendix E line 248: `TypeParamDecl = IdentifierList [ "owner" ] TypeConstraint`. The keyword may appear only after the names.

Example:

```go
func F[owner any](x owner) {} // a type parameter named owner
```

By 4.6, `owner` is followed by the identifier `any`, so it is the keyword. The grammar then has no name before it, so the declaration is a syntax error. A reader who follows the grammar sees a parameter named `owner`. Appendix D says contextual keywords "are not declared at all", which suggests the name is free.

Proposed resolution: in 4.6, say `owner` is the keyword in a type parameter declaration only in the position after the identifier list. `[owner any]` then declares a parameter named `owner`. This keeps the contextual keyword cheap and avoids a surprise for Go users who use the word as a name.

---

## A3. When a moving conversion moves its operand is not stated

- Kind: hole.
- Severity: medium.
- Changes a promise: no.

Sections and quotes:

- 11.26: "An owner variable that an expression moves is moved when the call or assignment that takes it is performed".
- 11.24 rule 3: an owner-to-owner conversion moves its operand. A conversion to an owning interface also moves (7.9).
- 11.26 places conversions with the other operands, after calls.

Example:

```go
type A owner struct{ ... }
type B A                 // also an owner
g(B(a), use(&a))         // is a moved before or after use(&a) runs?
```

Calls run first (11.26 step 1), so `use(&a)` runs first and sees `a`. But the text ties the move to "the call or assignment that takes it", and the conversion is neither. A reader could place the move at the conversion or at `g`. This changes which E3313 errors are reported and when the old owner is invalid.

Proposed resolution: add to 11.26: "A conversion that moves its operand moves it when the enclosing call or assignment is performed, like a direct argument." This keeps one rule for all moves.

---

## A4. Appendix A.3 cites the wrong section for iterators over owners

- Kind: wrong section reference.
- Severity: low.
- Changes a promise: no.

Appendix A.3 cites section 12.11 for "iterators over owners yield views". The rule is in 12.10, under "Owners in iterators". Section 12.11 only points back to 12.10.

Proposed resolution: change the reference to 12.10.

---

## A5. Chapter 7 names section 7.12 with the wrong title

- Kind: wrong section reference.
- Severity: low.
- Changes a promise: no.

07-types.md, about line 1387: "section 7.12 (Other types)". The title of 7.12 is "Enums".

Proposed resolution: fix the title, or point at the section that the sentence means.

---

## A6. The markers for illegal examples disagree

- Kind: contradiction (notation).
- Severity: low.
- Changes a promise: no.

- 02-notation.md: "a comment that begins `error:` marks a line that is a compile error".
- 1.3: examples compile unless the comment begins with `error`, `illegal` or `invalid`.
- 11.24 and 12.10 use `// illegal` and `// invalid`.

A tool that extracts examples by the chapter 2 rule will try to compile the `// illegal` lines.

Proposed resolution: state one rule in chapter 2 and make 1.3 refer to it. Either list all three words in chapter 2, or change the examples to `// error:`.

---

## A7. 8.7 makes a string's kind depend on its value

- Kind: contradiction.
- Severity: low.
- Changes a promise: no.

- 8.7: a string's kind is "counted value (a string made with unsafe.String, or in a local arena, is a view)".
- The same chapter says: "Counting is not chosen by the program: it follows from the type."

Kind is a property of the type in every other row of the table. A value-dependent kind cannot be checked statically.

Proposed resolution: say the kind of `string` is counted. Then add a separate sentence: a string made by `unsafe.String` or held in a local arena has a borrowed backing store, and the rules for that are in 20.1 and 18.9. The program is responsible for the unsafe case.

---

## A8. Verification record C12 is stale against D11

- Kind: stale text (outside the language spec, recorded as a note).
- Severity: low.
- Changes a promise: no.

docs/verification/2026-10-15-spec-consistency.md, row C12: "Two calls through the same errors.New call site compare equal by its static identity". Decision D11 and appendix A.5 now say each evaluation of errors.New gives a fresh identity. The witness tests the old rule.

Proposed resolution: update the C12 witness to D11, or mark the row as superseded by D11.

---

## Checked and dropped (decided elsewhere)

- Loop variables as a false friend: 12.10, 18.4, appendix A.3 and E3006 cover it.
- Interface frame temporaries refilled in a loop: 18.4 "interface copies the body makes" covers it.
- `or` in var declarations, return, select, defer, task and for post: 11.27, E1001 and E2465 decide them. The appendix E `OrStmt` matches 11.27.
- `main` returning `int64`: 10.12 and 15.4 agree.
- `shared[T]` exposing the methods of `*T`: 18.11 generation checks and 19.3 rule 3 cover it.
- Allocation by string conversions: 18.10 documents it.
- The range-over-func defer example: valid under E2534.
- Appendix B and D against 4.5 and 4.6: same 27 keywords, same contextual table.
- Appendix C against 11.18 to 11.23: matches. The `or` note matches 11.27.
- Appendix A.5 against D10 and D11: matches.
- Appendix E against the chapter productions for `TypeParamDecl`, `ParameterDecl`, `DefaultValue`, `FunctionDecl`, `MethodDecl`, `ShortVarDecl`, `Conversion`, `OrStmt` and `DefinedType`: identical.
- 10.13 to 10.15 (defaults, method declarations, generic methods): consistent with 7.8, 7.9, 8.3 and 18.6.

## Sections not read closely

- Chapter 8: 8.1 to 8.5 (8.4 only in part).
- Chapter 10: 10.1 to 10.5.
- Chapter 11: 11.1 to 11.23. This covers composite literals, closures and regions (11.5), selectors, method expressions and values, index and slice, type assertions, calls, inference and the operator sections.
- Chapter 12: 12.12 (only in part) and 12.13 (select).
- Appendix E beyond the productions listed above.
- Appendices F and G.
- Chapter 18: 18.5 after its start, 18.7, 18.9, 18.11, 18.12.
- Chapter 19: 19.2 and 19.3 were only searched with grep.
