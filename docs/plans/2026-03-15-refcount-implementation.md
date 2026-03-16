# Automatic Reference Counting Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement automatic reference counting so that when a heap-allocated pointer is copied to another variable, the compiler tracks shared ownership and frees only when the last reference goes out of scope.

**Architecture:** Prepend an 8-byte refcount header before every `alloc.New[T]()` allocation. The user pointer is offset +8 from the real allocation. On pointer copy (`b := a` where `a` is heap-owned), emit `incRef`. At scope exit, emit `decRef` instead of `freeRaw`. `decRef` decrements the count and only frees when it hits zero.

**Tech Stack:** Plutonium compiler (Go), x86_64 assembly for runtime helpers

---

### Task 1: Add assembly helpers readInt64/writeInt64

**Files:**
- Modify: `pluto/alloc/mem_linux_amd64.s`
- Modify: `pluto/alloc/alloc.pu` (add declarations)

**Step 1:** Add `readInt64` and `writeInt64` assembly routines (identical to readPtr/writePtr at machine level, but typed as int64 in Plutonium).

**Step 2:** Add Plutonium function declarations in `alloc.pu`.

### Task 2: Modify newRaw to prepend refcount header

**Files:**
- Modify: `pluto/alloc/alloc.pu` (change `slabAlloc` call in newRaw wrapper)
- Modify: `pluto/alloc/mem_linux_amd64.s` (update newRaw assembly wrapper)

**Step 1:** Change `alloc.newRaw` assembly to: allocate `size+8`, write refcount=1 at base, return `base+8`.

### Task 3: Add incRef and decRef runtime functions

**Files:**
- Modify: `pluto/alloc/mem_linux_amd64.s` (assembly wrappers)
- Modify: `pluto/alloc/alloc.pu` (Plutonium implementations + declarations)

**Step 1:** Implement `incRef(ptr *byte)` and `decRef(ptr *byte, size int64)` in alloc.pu.
**Step 2:** Add assembly wrapper stubs for ABI safety.

### Task 4: Compiler — emit decRef instead of freeRaw in RAII

**Files:**
- Modify: `internal/ir/lower_stmt.go` (emitRAIIFrees)

**Step 1:** Change `emitRAIIFrees` to call `alloc.decRef` instead of `alloc.freeRaw`.

### Task 5: Compiler — emit incRef on pointer copy

**Files:**
- Modify: `internal/ir/lower_stmt.go` (lowerAssign)

**Step 1:** In `lowerAssign` for `:=`, detect when RHS is a heap-owned variable and emit `alloc.incRef`.
**Step 2:** Mark the new variable as heap-owned with the same size info.

### Task 6: Compiler — emit decRef for explicit alloc.Free

**Files:**
- Modify: `internal/ir/lower_call.go` (lowerAllocSimpleCall)

**Step 1:** Change `alloc.Free` to call `alloc.decRef` instead of `alloc.freeRaw`.

### Task 7: Update tests

**Files:**
- Modify: `internal/ir/lower_test.go`

**Step 1:** Update existing RAII tests to expect `alloc.decRef` instead of `alloc.freeRaw`.
**Step 2:** Add test for refcount increment on pointer copy.

### Task 8: Add example 15-refcount

**Files:**
- Create: `examples/15-refcount/main.pu`

### Task 9: Build and verify all examples

**Step 1:** Build compiler, build and run examples 07-14 and 15.
**Step 2:** Run `go test ./...`.

### Task 10: Commit

Commit message: `feat(compiler): implement automatic reference counting for shared heap pointers`
