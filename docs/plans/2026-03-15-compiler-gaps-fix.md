# Compiler Gap Fixes — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all critical and important gaps identified in the compiler audit.

**Architecture:** Grouped by cluster for efficient implementation. Each cluster touches related files and can be tested together.

---

## Cluster 1: Control Flow (break/continue/switch/defer)

### Task 1: break and continue

**Files:** `internal/ir/lower_stmt.go`, `internal/ir/lower.go`

The lowerer needs a loop context stack so break/continue know which
loop's exit/condition blocks to jump to.

- Add `loopStack []loopContext` to the lowerer struct where
  `loopContext` has `exitBlock` and `condBlock` (or `postBlock`)
- Push context in `lowerFor` and `lowerRange` before lowering the body
- Pop after
- In `lowerStmt(BranchStmt)`:
  - `break` → `l.bb.Br(currentLoop.exitBlock)`
  - `continue` → `l.bb.Br(currentLoop.postBlock)` (for-loop) or
    `l.bb.Br(currentLoop.condBlock)` (range)
- Set `l.bb.Term` after the branch so no more code is emitted

**Test:** for loop with break on condition, for loop with continue

### Task 2: switch/case

**Files:** `internal/ir/lower_stmt.go`

Lower switch as a chain of if-else:
- Evaluate the tag expression once
- For each CaseClause, compare tag to each case value
- Branch to the matching case body
- Default clause branches unconditionally
- After each case body, branch to the merge block (no fallthrough)

**Test:** switch on integer with multiple cases and default

### Task 3: defer

**Files:** `internal/ir/lower_stmt.go`, `internal/ir/lower.go`

Defer needs a defer stack per function:
- Add `deferCalls []deferInfo` to the lowerer
- When encountering `DeferStmt`, push the call expression
- Before every return and at implicit function exit, emit the deferred
  calls in LIFO order (last defer runs first)

**Test:** defer ordering (last in, first out), defer with function exit

---

## Cluster 2: Numeric Literals and Types

### Task 4: Hex, octal, binary integer literals

**Files:** `internal/ir/lower_expr.go`

Replace `fmt.Sscanf(e.Value, "%d", &n)` with Go's `strconv.ParseInt`
which handles `0x`, `0o`, `0b` prefixes automatically.

**Test:** hex literal `0xFF`, octal `0o77`, binary `0b1010`

### Task 5: Char literals

**Files:** `internal/ir/lower_expr.go`

Replace `return ConstInt(I32, 0)` for CHAR with actual character
value parsing. Strip quotes, handle escape sequences, return the
rune value.

**Test:** `'A'` = 65, `'\n'` = 10, `'\\'` = 92

### Task 6: Float literal values

**Files:** `internal/ir/lower_expr.go`

Parse the float string with `strconv.ParseFloat` and store in
`FloatVal` on the Value.

**Test:** `3.14` parsed correctly (IR test)

### Task 7: Array length evaluation

**Files:** `internal/types/resolver.go`, `internal/ir/lower.go`

For array types `[N]T`, evaluate N as a constant integer:
- In the resolver, when `e.Len` is a `*ast.BasicLit` with INT kind,
  parse it and store in `Array.len`
- In the lowerer, do the same for `ArrayOf(n, elem)`

**Test:** `[4]int32` has length 4 in IR, `SizeOf` returns 32

---

## Cluster 3: Data Structures

### Task 8: Slice expressions (s[low:high])

**Files:** `internal/ir/lower_expr.go`

Handle `*ast.SliceExpr` in `lowerExprValue`:
- Load the base pointer and length from the slice
- Compute new pointer: `base + low * elemSize`
- Compute new length: `high - low`
- Return a new slice header (ptr, len, cap)

For v0.1, handle the common case: `s[low:high]` on a decomposed
slice (has `__len` companion).

**Test:** `s[1:3]` on a string or slice

### Task 9: for-range value binding

**Files:** `internal/ir/lower_stmt.go`

In `lowerRange`, after binding the key variable, also bind the
value variable:
- Load the base pointer from the range expression
- GEP to the current index
- Load the element
- Store to the value variable's slot

**Test:** `for _, v := range slice { use(v) }`

### Task 10: Multi-return values

**Files:** `internal/ir/lower_stmt.go`, `internal/ir/lower_call.go`,
`internal/codegen/x86_64/codegen.go`

The callee stores multiple returns into stack slots and returns void.
The caller needs to retrieve them. Approach for v0.1:

Use a register pair: first return in RAX, second in RDX. This is
the standard System V ABI for structs <= 16 bytes.

- In `lowerReturn`: for 2 returns, put first in RAX, second in RDX
- In the codegen: for `TermRet` with a struct type, MOV first field
  to RAX, second to RDX
- At the call site: after CALL, extract RAX and RDX for the two
  return values

For v0.1, only support 2-value returns (covers `val, err` and
`val, ok` patterns).

**Test:** `func swap(a, b int64) (int64, int64)` end-to-end

---

## Cluster 4: Codegen Robustness

### Task 11: Callee-saved register preservation

**Files:** `internal/codegen/x86_64/codegen.go`

The register allocator uses callee-saved registers (RBX, R12-R15)
but the function prologue/epilogue doesn't save/restore them.

- In the prologue, after `PUSH RBP`, emit `PUSH` for each
  callee-saved register that the register allocator actually used
- In the epilogue, before `POP RBP`, emit `POP` for each in reverse
- Track which callee-saved regs were used by scanning `alloc.Reg`

**Test:** function that uses many registers, called from another
function that also uses many registers — values must be preserved

### Task 12: More than 6 function arguments

**Files:** `internal/codegen/x86_64/codegen.go`

Arguments beyond the 6th must be pushed to the stack per System V ABI:
- Args 7+ are pushed right-to-left onto the stack before the CALL
- The callee accesses them at `[RBP+16]`, `[RBP+24]`, etc.
- After the CALL, the caller adjusts RSP back

For v0.1, at minimum warn or error instead of silently dropping.
Full implementation: push extra args, update `emitParamSetup` to
load from `[RBP+16+n*8]` for params beyond 6.

### Task 13: &^ (AND_NOT) operator

**Files:** `internal/ir/lower_expr.go`, `internal/ir/lower_stmt.go`

Add `token.AND_NOT` case to `lowerBinaryExpr`:
`x &^ y` = `x & (^y)` = `OpAnd(x, OpXor(y, -1))`

Add `token.AND_NOT_ASSIGN` to `compoundOp` — expand similarly.

### Task 14: Short-circuit && and ||

**Files:** `internal/ir/lower_expr.go`

Replace the simple AND/OR with conditional branches:
- `a && b`: evaluate a, if false → result is false (skip b)
- `a || b`: evaluate a, if true → result is true (skip b)

Create phi-like merge using an alloca:
```
slot := alloca(bool)
a := eval(lhs)
store(slot, a)
if a == 0 → merge (for &&) / if a != 0 → merge (for ||)
b := eval(rhs)
store(slot, b)
merge:
result := load(slot)
```

### Task 15: Unsigned comparison and division

**Files:** `internal/ir/lower_expr.go`, `internal/codegen/x86_64/codegen.go`

- In the lowerer, check if the operand type is unsigned (U8/U16/U32/U64)
  and emit `CmpULT`/`CmpULE`/`CmpUGT`/`CmpUGE` accordingly
- In the codegen, add the missing `CmpULE` → `SETBE` mapping
- For division: if operands are unsigned, use `XOR RDX,RDX` + `DIV`
  instead of `CQO` + `IDIV`

### Task 16: Printf %s (string formatting)

**Files:** `pluto/fmt/fmt.pu`

The `%s` verb currently prints `?`. It needs to:
- Extract the data pointer from the any arg (which is a pointer to
  a string header `{ptr, len}`)
- Read the string ptr and len from the header
- Call `Prints(ptr, len)` to write the string

Uses the existing `anyData` assembly helper to get the header pointer,
then `readPtr` to extract ptr and len fields.

---

## Cluster 5: Misc Important

### Task 17: Undefined identifiers should not silently return 0

**Files:** `internal/ir/lower_expr.go`

Change `lowerIdent` to emit a diagnostic or at least use a
distinguishable sentinel value instead of `ConstInt(I32, 0)`.
For v0.1, keep the 0 fallback but add a comment. The type checker
should catch undefined identifiers before the lowerer sees them.

### Task 18: Update examples with new features

**Files:** `examples/`

Update or create examples that exercise:
- break/continue in loops
- switch/case
- defer
- hex/char literals
- multi-return values
- string formatting with %s

Create `examples/16-control-flow/main.pu` covering control flow.

---

## Dependency order

Clusters 1-2 are independent. Cluster 3 tasks are mostly independent.
Cluster 4 tasks are independent. Execute in this order:

1. Cluster 2 (numeric) — smallest changes, quick wins
2. Cluster 1 (control flow) — medium changes, high value
3. Cluster 4 (codegen) — important correctness fixes
4. Cluster 3 (data structures) — requires working control flow
5. Cluster 5 (misc) — cleanup and examples

Total: 18 tasks. Each is focused and testable independently.
