# Known gaps: where today's compiler and library differ from the specification

The specification (`docs/spec/language/`) is the source of truth, and the compiler and library are fixed to match
it. These are the differences found while writing this skill. Each is backed by a block that the skill's test
compiles and runs against the current compiler. **If a block here starts to fail, the gap has been closed**: delete
the item and let the specification's text stand. Until then, write code that works today and do not "fix" the
compiler's behavior in application code.

## 1. Smaller notes

- `pluto vet` has one check, `-unsafe`; there is no general lint. `pluto fmt` is the only style tool.
- Library names that Go has and Plutonium lacks are listed in `go-to-plutonium.md`.
