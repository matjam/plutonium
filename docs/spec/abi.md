# Plutonium x86_64 ABI

**Version:** 0.1

## Calling Convention

Plutonium follows the System V AMD64 ABI for function calls.

### Integer/Pointer Arguments

Arguments are passed in registers in this order:
1. RDI
2. RSI  
3. RDX
4. RCX
5. R8
6. R9

Additional arguments are passed on the stack (right-to-left).

### Return Values

- Single return value: RAX
- Second return value: RDX
- Multiple return values beyond two: via hidden pointer parameter

### Caller-Saved Registers

RAX, RCX, RDX, RSI, RDI, R8, R9, R10, R11

### Callee-Saved Registers

RBX, RBP, R12, R13, R14, R15

### Stack Alignment

The stack must be 16-byte aligned before a CALL instruction.

## Stack Frame Layout

```
High address
+------------------+
| args from caller |  (if any)
+------------------+
| return address   |  <- pushed by CALL
+------------------+
| saved RBP        |  <- pushed in prologue (PUSH RBP)
+------------------+  <- RBP points here
| local variables  |  (alloca'd stack slots)
| (negative from   |
|  RBP)            |
+------------------+  <- RSP (aligned to 16 bytes)
Low address
```

## Slice Layout

A slice `[]T` is a struct of three 64-bit fields:

```
struct {
    ptr *T   // pointer to backing array
    len int64
    cap int64
}
```

Size: 24 bytes. Alignment: 8 bytes.

## String Layout

A string is represented as:

```
struct {
    ptr *byte // pointer to UTF-8 bytes
    len int64 // byte length (not rune count)
}
```

Size: 16 bytes. Alignment: 8 bytes.

Note: Plutonium strings are immutable. String concatenation with `+` on non-constant strings is a compile error.

## Interface Layout

An interface value is a pair:

```
struct {
    itab *ItabEntry // pointer to method table + type info
    data unsafe.Pointer // pointer to concrete value
}
```

Since Plutonium only supports compile-time interface dispatch, interface values in v0.1 always point to a static itab generated at compile time.

## Syscall Convention (Linux x86_64)

Linux syscalls use a separate convention from function calls:

| Purpose | Register |
|---|---|
| Syscall number | RAX |
| Argument 1 | RDI |
| Argument 2 | RSI |
| Argument 3 | RDX |
| Argument 4 | R10 |
| Argument 5 | R8 |
| Argument 6 | R9 |
| Return value | RAX |
| Error indicator | RAX (negative = errno) |

The `SYSCALL` instruction is used. RCX and R11 are clobbered by the kernel.
