# Systems Programming Types — Implementation Plan

> **Historical:** superseded by the specification; see section 7.14 (`packed` types), section 7.15 (`volatile[T]`), section 7.16 (`aligned[T, N]`), section 7.17 (`ring[T, N]`) and section 18.9 (Allocators). This is the phased implementation plan for the systems types; all of it landed long ago, and the compiler is described by `AGENTS.md`. The specification is the single source of truth: where this document and the specification differ, the specification wins. This document is kept for the history of the March 2026 prototype and is not maintained.

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement packed types with bitfields, volatile, aligned, ring buffers, bit-level views, and typed allocation primitives for Plutonium.

**Architecture:** The work is divided into phases. Phase 1 builds missing compiler infrastructure (struct fields, methods, const generics). Phase 2 implements packed types and bitfields as the first concrete type. Subsequent phases build the remaining types in dependency order.

**Tech Stack:** Go compiler (internal/), x86_64 codegen, Plutonium stdlib (.pu files)

**Design doc:** `docs/plans/archive/2026-03-15-systems-types-design.md`

---

## Phase 1: Compiler Infrastructure

These are prerequisites that multiple types depend on. Must be completed first.

### Task 1: Struct field access by name (not just field 0)

The lowerer's `lowerSelectorExpr` in `internal/ir/lower_expr.go:257-263` hardcodes `ExtractField(I32, base, 0)`. The codegen already supports arbitrary `FieldIdx`. We need the lowerer to resolve field names to indices.

**Files:**
- Modify: `internal/ir/lower_expr.go` (lowerSelectorExpr)
- Modify: `internal/ir/lower.go` (add struct type tracking to lowerer)
- Test: `internal/ir/lower_test.go`

**Step 1: Write the failing test**

Add to `internal/ir/lower_test.go`:

```go
func TestStructFieldAccess(t *testing.T) {
    src := `
package main
type Point struct { X int64; Y int64 }
func getY(p Point) int64 { return p.Y }
`
    prog := lowerSource(t, src)
    fn := findFunc(prog, "getY")
    // Should access field index 1, not 0
    found := false
    for _, bb := range fn.Blocks {
        for _, instr := range bb.Instrs {
            if instr.Op == OpExtractField && instr.FieldIdx == 1 {
                found = true
            }
        }
    }
    if !found {
        t.Error("expected ExtractField with FieldIdx=1 for p.Y")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/ir/ -run TestStructFieldAccess -v`
Expected: FAIL — FieldIdx is 0, not 1

**Step 3: Implement field name resolution**

In `lower_expr.go`, update `lowerSelectorExpr` to look up struct field
names. The type checker has already resolved the struct type — we need
to pass that info through to the lowerer.

Approach: when lowering a struct-typed expression, track the struct's
field list. In `lowerSelectorExpr`, match the selector name against the
field list to get the index.

Add a `structTypes` map to the lowerer that maps IR value IDs to their
type-checker struct types. Populate it when lowering struct-typed
variables.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/ir/ -run TestStructFieldAccess -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `go test ./...`
Expected: all pass

**Step 6: Verify 07-hello and 08-printf still work**

```bash
go build -o /tmp/pluto ./cmd/pluto/
/tmp/pluto build -o /tmp/hello ./examples/07-hello/ && /tmp/hello
/tmp/pluto build -o /tmp/printf_test ./examples/08-printf/ && /tmp/printf_test
```

**Step 7: Commit**

```bash
git add -A && git commit -m "feat(ir): resolve struct field names to indices in lowerSelectorExpr"
```

---

### Task 2: Composite literal field initialization

`lowerCompositeLit` in `lower_expr.go:266-276` allocates a slot but
never fills in fields. Need to iterate `e.Elts` and emit `InsertField`
for each.

**Files:**
- Modify: `internal/ir/lower_expr.go` (lowerCompositeLit)
- Test: `internal/ir/lower_test.go`

**Step 1: Write the failing test**

```go
func TestCompositeLitFields(t *testing.T) {
    src := `
package main
type Point struct { X int64; Y int64 }
func makePoint() int64 {
    p := Point{X: 10, Y: 20}
    return p.Y
}
`
    prog := lowerSource(t, src)
    fn := findFunc(prog, "makePoint")
    // Should have InsertField instructions for fields 0 and 1
    insertCount := 0
    for _, bb := range fn.Blocks {
        for _, instr := range bb.Instrs {
            if instr.Op == OpInsertField {
                insertCount++
            }
        }
    }
    if insertCount < 2 {
        t.Errorf("expected at least 2 InsertField ops, got %d", insertCount)
    }
}
```

**Step 2-7: Same pattern as Task 1** — fail, implement, pass, full suite, examples, commit.

Implementation: iterate `e.Elts`, handle both `KeyValueExpr` (named fields)
and positional initialization. Use `InsertField` for each field.

---

### Task 3: Method declaration lowering

Methods are parsed and type-checked but not lowered to IR. The receiver
needs to be treated as the first parameter.

**Files:**
- Modify: `internal/ir/lower.go` (lowerFuncBody — handle Recv)
- Modify: `internal/mono/mono.go` (preserve Recv in FuncEntry)
- Modify: `internal/ir/lower_call.go` (method call lowering)
- Test: `internal/ir/lower_test.go`

**Step 1: Write the failing test**

```go
func TestMethodLowering(t *testing.T) {
    src := `
package main
type Counter struct { val int64 }
func (c *Counter) Inc() { c.val = c.val + 1 }
func use() {
    var c Counter
    c.Inc()
}
`
    prog := lowerSource(t, src)
    // Method should be lowered as a function with receiver as first param
    fn := findFunc(prog, "Counter.Inc")
    if fn == nil {
        t.Fatal("expected function Counter.Inc")
    }
    if len(fn.Params) < 1 {
        t.Error("expected at least 1 param (receiver)")
    }
}
```

**Step 2-7: Same TDD pattern.**

Implementation approach:
- In monomorphization, include methods in the function list with qualified
  names like `Counter.Inc`
- In `lowerFuncBody`, if `d.Recv != nil`, prepend the receiver as the first
  parameter
- In call lowering, when a method call like `c.Inc()` is detected (selector
  on a struct-typed variable), prepend the receiver address as arg 0

---

### Task 4: Const generic parameters

Types like `ring[T, N]`, `aligned[T, N]`, and `bits[T, N]` need integer
const parameters. The monomorphizer only handles type name parameters.

**Files:**
- Modify: `internal/mono/mono.go` (handle BasicLit as type arg)
- Modify: `internal/types/types.go` (ConstParam type?)
- Test: `internal/mono/mono_test.go`

**Step 1: Write the failing test**

```go
func TestConstGenericParam(t *testing.T) {
    src := `
package main
func fixed[N int]() int64 { return N }
func use() int64 { return fixed[8]() }
`
    result := monomorphize(t, src)
    // Should produce fixed__8 with body returning 8
    found := false
    for _, fe := range result.Functions {
        if fe.Name == "fixed__8" {
            found = true
        }
    }
    if !found {
        t.Error("expected monomorphized fixed__8")
    }
}
```

**Step 2-7: Same TDD pattern.**

Implementation: extend `typeArgName()` to handle `*ast.BasicLit`, extend
substitution to replace const params with their values in expressions.

---

## Phase 2: Packed Types + bits[T, N]

With struct field access and const generics working, implement packed types.

### Task 5: `packed` type parsing

Add `packed` as a keyword. Parse `type Name packed [le|be] BackingType { fields }`.

**Files:**
- Modify: `internal/token/token.go` (add PACKED keyword)
- Modify: `internal/parser/parser.go` (parse packed type declarations)
- Modify: `internal/ast/expr.go` (add PackedType, BitsField AST nodes)
- Test: `internal/parser/parser_test.go`

---

### Task 6: Packed type checking

Add packed types to the type system. Compute field bit offsets and widths.
Validate bit width constraints. Enforce power-of-2 for `[N]T` backing.

**Files:**
- Modify: `internal/types/types.go` (add Packed type)
- Modify: `internal/types/resolver.go` (resolve packed types)
- Modify: `internal/types/checker.go` (validate field constraints)
- Test: `internal/types/checker_test.go`

---

### Task 7: Packed type IR lowering

Lower packed field access to mask/shift operations. A read of a
`bits[uint, 4]` field at bit offset 8 becomes:
`(backing >> 8) & 0xF`

A write becomes:
`backing = (backing & ^(0xF << 8)) | ((val & 0xF) << 8)`

**Files:**
- Modify: `internal/ir/ir.go` (add TyPacked? or reuse TyStruct with metadata)
- Modify: `internal/ir/lower_expr.go` (packed field access)
- Modify: `internal/ir/lower_stmt.go` (packed field assignment)
- Test: `internal/ir/lower_test.go`

---

### Task 8: Packed type codegen + end-to-end test

Verify that the mask/shift IR compiles to correct x86_64. Create an example
that defines a packed register, writes fields, reads them back.

**Files:**
- Create: `examples/09-packed/main.pu`
- Test: build and run

---

### Task 9: Endianness support

Add `be`/`le` keyword to packed declarations. Emit `BSWAP` on load/store
when annotation doesn't match native endianness.

**Files:**
- Modify: parser, type checker, codegen
- Test: test with `packed be uint32 { ... }` on little-endian x86_64

---

### Task 10: Named flag groups (`bits { ... }`)

Add sugar for `bits { FLAG_A, FLAG_B }` that expands to multiple
`bits[bool, 1]` fields.

**Files:**
- Modify: parser, type checker
- Test: parser and type checker tests

---

## Phase 3: volatile[T]

### Task 11: volatile type wrapper

Add `volatile[T]` as a type in the type system. The codegen marks
loads/stores as non-eliminable and non-reorderable.

**Files:**
- Modify: types, resolver, IR, codegen
- Test: verify volatile loads are not eliminated

---

## Phase 4: aligned[T, N]

### Task 12: aligned type wrapper

Add `aligned[T, N]` requiring const generic N (from Task 4). Stack alloca
emits `AND RSP, -N`. Global variables emit `.align N`.

**Files:**
- Modify: types, resolver, IR alloca handling, codegen
- Test: verify alignment in generated binary

---

## Phase 5: unsafe primitives

### Task 13: unsafe.Sizeof[T] and unsafe.Alignof[T]

Lower to compile-time constants based on IR type sizes.

### Task 14: unsafe.As[T], unsafe.From[T]

Pointer casts between unsafe.Pointer and *T. Codegen is a no-op (same
register, different type).

### Task 15: unsafe.FromUintptr / unsafe.ToUintptr

Integer ↔ pointer conversions.

---

## Phase 6: alloc package

### Task 16: Default mmap-based allocator

Implement `pluto/alloc` package with `New[T]()` and `Free()` backed by
Linux mmap syscall.

### Task 17: Allocator interface

Requires interface dispatch (significant work). Define the `Allocator`
interface, implement `NewWith[T](a)` and `SetDefault(a)`.

---

## Phase 7: ring[T, N]

### Task 18: ring type in IR and codegen

Built-in type with compiler-generated Push/Pop/Len/Cap/Full/Empty/Reset.
Power-of-2 enforcement. AND masking for index wrapping.

### Task 19: ring stack size checking

Warn or error when stack-allocated rings exceed configurable threshold.

---

## Phase 8: bits[V] lens

### Task 20: bits view over a variable

Zero-cost lens with Set/Clear/Test/Toggle/Extract/Insert/PopCount/FirstSet
methods. Compiles to BT/BTS/BTR/BEXTR/POPCNT instructions.

### Task 21: bits[V] as PackedType

`bits[var] as PackedType` applies a packed type's field layout as a view
over the variable's storage.

---

## Dependency graph

```
Phase 1 (infrastructure)
  ├─ Task 1: struct field access
  ├─ Task 2: composite lit init
  ├─ Task 3: method lowering
  └─ Task 4: const generics
       │
Phase 2 (packed + bits[T,N])
  ├─ Task 5-10: packed types
  │    │
Phase 3 │ (volatile)     Phase 4 (aligned)
  │    │                    │
Phase 5 (unsafe)            │
  │                         │
Phase 6 (alloc) ────────────┘
  │
Phase 7 (ring) ← needs methods + const generics
  │
Phase 8 (bits[V] lens)
```

Tasks 1-4 are fully independent and can be parallelized. Phase 2 depends
on all of Phase 1. Phases 3-4 can run in parallel after Phase 2. Phase 5
depends on Phase 2. Phase 6 depends on Phase 5 and interface dispatch.
Phase 7 depends on methods + const generics. Phase 8 can run any time
after Phase 2.
