# Known gaps: where today's compiler and library differ from the specification

The specification (`docs/spec/language/`, version 0.4) is the source of truth, and the compiler and library are
fixed to match it. Each line below is a known difference, with the `docs/roadmap.md` item that tracks it. When an
item is done, delete its line and let the specification's text stand. Until then, write code that works today and
do not "fix" the compiler's behavior in application code. Never rely on a program the compiler wrongly accepts.

## 1. Programs the compiler accepts that the specification rejects

- A view of a table a map walk retired may outlive the walk while the map lives (18.3). Item 179.
- `owned`, `shared`, `synced` and `weak` by value in `//pu:extern` or `//pu:export` signatures are not E2511 yet
  (20.4). Item 157.
- The conversion checks of 11.12 and 11.24 (slicing a type parameter with a string in its type set, packed types and
  byte arrays, `string(c)`, owner conversions) are not all enforced. Item 166.
- A `//pu:stack` size outside 64K to 64G, and a program over an implementation limit, have no code yet (20.3, 21.4).
  Item 207.
- A range-over-func body whose iterator calls `yield` from a scoped task still counts as part of the enclosing
  function, so it may release that function's locks, and a `defer` in it is not an error (19.3, Part of the
  enclosing function). Item 223.
- Enabling interrupts (`x86.Sti`) does not yet start sharing; only installing a handler or starting a task does
  (19.3, The start of sharing). Item 224.
- The race check lowers its load limit for groups slow to settle, and drops tasks past 2000 groups instead of
  reporting an implementation limit (19.3, How precise the race check is). Item 226.
- A view of a lock-guarded variable in a scoped task is not yet ended by `Unlock` and a new `Lock` (19.4,
  Sharing). Item 227.
- The memory a per-CPU variable owns is not yet checked as part of the variable (20.9). Item 228.

## 2. Behavior that differs at run time

- E2584 and E3313 do not yet follow a `Reset`, `Release` or `Drop` of a Fixed or Chunked arena into the drops its
  entries would run (18.9, 19.3, 20.10). Item 209.
- Waiting (a channel operation, `Lock`, `Wait`, `rt.Park`) inside a non-migrating region, reached through calls with
  one known target, compiles; it should be a compile error, and today it panics with "blocking with preemption
  disabled" at run time only when it parks (19.3). Items 210 and 215.
- A function marked `//pu:blocking` does not yet test preemption and the interrupt flag at entry, or before each call
  when it has no body, so calling one in a non-migrating region can hang instead of panicking (19.3, 20.3, abi.md
  12.4). Item 216.
- In kernel mode, `rt.Park` does not yet panic when interrupts are disabled, so a wait with interrupts disabled and
  no lock held is not caught at run time (19.3, abi.md 12.4). Item 213.

## 3. Checks that are missing or too strict

- Interrupt handlers are not yet race parties from the point one may be installed (19.3). Item 156.
- Conformance checks for `alloc.Default()`, the arena window, the panic buffer, F.3 and F.6 names and debug
  allocator tags are not all tested. Item 167.
- `rt.Yield` inside a non-migrating region compiles and does not panic, so a task can resume on another processor
  while it holds a view of per-CPU data (19.3, Switching tasks in a non-migrating region). Item 229.
- A function started by `time.AfterFunc` counts as started once, so two runs after a `Reset` may race on what it
  writes without an error (19.3, Parties). Item 230.
- A range body does not get the locks its iterator holds at `yield`, so a locked iterator is E3502 or E3501 as a
  range loop while the same literal passed to it compiles; a `Do` before a call does not count in the callee
  (19.3, Part of the enclosing function). This includes the method shape `range sc.All()`, whose closure locks
  `s.mu` (19.3, Locks and names). Item 232.
- The move check is stricter than 18.3 in some cases it does not follow: a literal that gives a moved owner a new
  value and then reads it, a literal passed to a function that escape analysis says may keep it, views stored by a call
  (which lose the field they came from), a pointer to a local (which keeps the views
  the local held when the pointer was made, after the local is given new ones), a slice element
  given a new value through an index that is not a constant, or of a slice variable that is given other values
  (which ends the views into every element), and a vector in a package variable whose allocator
  is an interface (which may free through every counted local). Item 241.

## 4. Smaller notes

- `pluto vet` has one check, `-unsafe`; there is no general lint. `pluto fmt` is the only style tool.
- Library names that Go has and Plutonium lacks are listed in `go-to-plutonium.md`.
