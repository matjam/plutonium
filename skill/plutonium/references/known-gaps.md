# Known gaps: where today's compiler and library differ from the specification

The specification (`docs/spec/language/`, version 0.4) is the source of truth, and the compiler and library are
fixed to match it. Each line below is a known difference, with the `docs/roadmap.md` item that tracks it. When an
item is done, delete its line and let the specification's text stand. Until then, write code that works today and
do not "fix" the compiler's behavior in application code. Never rely on a program the compiler wrongly accepts.

## 1. Programs the compiler accepts that the specification rejects

- A consuming method used as a method value (`done := c.Close`) compiles; it should be an error (11.9). Item 178.
- Four view-tracking cases of 18.3: a view stored into memory a local owner's method returned, a pointer to caller
  memory that loses its tie when the owner moves, a view kept in a moved local owner then stored in a package
  variable, a view of a table a map walk retired. Item 179.
- A local `vec.Vec` of owners whose element drops run through `rt.ReleaseValues` or `ZeroValues`: the escape
  analysis gives those calls no effects (`review3/o14`). Item 180.
- A view of a local that has cleanup but is not an owner, used after its scope ends, is not E3313 yet (18.3). Item 146.
- Conversions to an owner type, `*Owner` pointer conversions and literals of an owner struct with an unexported field
  outside the defining package are not E3212 yet (7.18, 11.24). Item 147.
- A type switch, assertion or conversion on an ordinary interface does not yet move or lend as 18.5 says. Item 148.
- An enum-to-enum conversion such as `Shade(color)` is not E2313 yet (7.12, 11.24). Item 150.
- `owned`, `shared`, `synced` and `weak` by value in `//pu:extern` or `//pu:export` signatures are not E2511 yet
  (20.4). Item 157.
- The conversion checks of 11.12 and 11.24 (slicing a type parameter with a string in its type set, packed types and
  byte arrays, `string(c)`, owner conversions) are not all enforced. Item 166.
- The race check does not yet treat frees, drops and moves as writes (19.3, 19.4). Item 168.
- E3313 is not reported at calls that may free or move out what a view points into (18.3, 18.10). Item 170.
- A moving assertion or bound type switch on a field, element or `*p` is not E3303 yet (11.13, 12.9). Item 172.
- A closure passed through a func value or an interface may not yet count as kept for the region errors (11.5).
  Item 176.
- A `%w` wrapper of an error in a field of a local owner is not yet tested to be bounded by the owner (16.5).
  Item 177.
- The race check does not follow an allocator behind a parameter, a heap pointer or an `alloc.Use` window, or bulk
  calls outside scoped tasks (19.4). Item 196.

## 2. Behavior that differs at run time

- Fixed and Chunked arenas do not count live counted objects, so `Reset`, `Release` and `Drop` do not panic with
  "arena has live counted objects" (18.9). Item 152.
- A Fixed arena still asks its parent for bookkeeping (18.9, F.2). Item 153.
- `Free` through an arena still reclaims the latest allocation and can panic with "arena allocation freed" (18.9).
  Item 154.
- The write check for cleaned places is not gated on package cleanup having started (15.4, 21.5). Item 155.
- `rt.ExitTask` mid-statement or inside a `Drop` method may not drop what 17.3 says. Item 158.
- `copy` and a first assignment to a borrowed string parameter: count-before-release is not yet checked (13.2,
  18.8). Item 159.
- The array holding a call's variadic arguments may not release its counted elements at the end of the statement
  (18.8). Item 160.
- Freestanding builds outside kernel mode do not yet insert explicit nil checks (17.5). Item 162.
- A handler's print can wait on a console lock its own processor holds (13.12, 20.10). Item 163.
- `vec`, `hash`, `strings` and `bytes` may not restore their invariants before every check that can panic (17.2,
  18.12). Item 165.
- A closure region does not yet run drop glue for every captured variable with cleanup (11.5, 18.8). Item 169.
- A large owner lent to an ordinary interface does not yet give a data word that points at the variable (7.9,
  abi.md 5). Item 171.
- A range over a counted value does not hold its own count until the loop ends (12.10). Item 173.
- An owner declared in a for clause may not move between iterations and drop once (12.10, 18.6). Item 174.

## 3. Checks that are missing or too strict

- Comma-ok `ok` is not followed through direct tests, so some correct programs get E3302 (11.13, 18.5). Item 149.
- `type V U` for a union or packed `U` is not supported as 7.13 and 7.14 define it. Item 151.
- Interrupt handlers are not yet race parties from the point one may be installed (19.3). Item 156.
- `pluto vet -unsafe` still lists `x86.Pause`, `Cpuid`, `Rdtsc` and `Rdtscp`, and does not list `//pu:export`
  functions (18.12). Item 161.
- `[owner any]` may not parse as a type parameter named `owner` (4.6). Item 164.
- Conformance checks for `alloc.Default()`, the arena window, the panic buffer, F.3 and F.6 names and debug
  allocator tags are not all tested. Item 167.
- `defer x.Drop()` may be treated as a copy of `x`, and a later move may not cancel it (18.5). Item 175.

## 4. Smaller notes

- `pluto vet` has one check, `-unsafe`; there is no general lint. `pluto fmt` is the only style tool.
- Library names that Go has and Plutonium lacks are listed in `go-to-plutonium.md`.
