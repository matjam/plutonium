# Known gaps: where today's compiler and library differ from the specification

The specification (`docs/spec/language/`, version 0.4) is the source of truth, and the compiler and library are
fixed to match it. Each line below is a known difference, with the `docs/roadmap.md` item that tracks it. When an
item is done, delete its line and let the specification's text stand. Until then, write code that works today and
do not "fix" the compiler's behavior in application code. Never rely on a program the compiler wrongly accepts.

## 1. Programs the compiler accepts that the specification rejects

- Four view-tracking cases of 18.3: a view stored into memory a local owner's method returned, a pointer to caller
  memory that loses its tie when the owner moves, a view kept in a moved local owner then stored in a package
  variable, a view of a table a map walk retired. Item 179.
- A type switch, assertion or conversion on an ordinary interface does not yet move or lend as 18.5 says. Item 148.
- `owned`, `shared`, `synced` and `weak` by value in `//pu:extern` or `//pu:export` signatures are not E2511 yet
  (20.4). Item 157.
- The conversion checks of 11.12 and 11.24 (slicing a type parameter with a string in its type set, packed types and
  byte arrays, `string(c)`, owner conversions) are not all enforced. Item 166.
- The race check does not yet treat frees, drops and moves as writes (19.3, 19.4). Item 168.
- A call through a copy of a counted reference in a field or parameter does not end views obtained through a local
  reference; a method called through an ordinary interface held in a field is taken to free nothing of the owner
  holding the field; a view of what a local owner holds kept where the walk cannot follow is not checked at the
  owner's drop at the end of its block, nor at a free when only memory loaded through the view was kept (E3313,
  E3314, 18.3, 7.9, 7.19). Item 237.
- A deferred call that drops or writes what a scoped task uses, and that can run before the task is joined, is not
  E3404 or E3402 yet (19.4). Item 201.
- A conversion to an interface of a value whose type contains a type parameter, in generic code, is not yet a frame copy for every
  instantiation, nor, for every type parameter (`[T any]` too), a view that the D3 events end (E3313, 18.3). A `T`
  local is not yet treated as having drop glue whenever a type of its type set has it. Items 204 and 208.
- A pointer from `&T{...}`, a slice literal or a variadic array made in a loop and kept past the iteration may not be
  E3006 yet (18.4). Item 206.
- A `//pu:stack` size outside 64K to 64G, and a program over an implementation limit, have no code yet (20.3, 21.4).
  Item 207.
- A closure passed through a func value or an interface may not yet count as kept for the closure region errors (11.5).
  Item 176.
- A `%w` wrapper of an error in a field of a local owner is not yet tested to be bounded by the owner (16.5).
  Item 177.
- The race check does not follow an allocator behind a parameter, a heap pointer or an `alloc.Use` window, or bulk
  calls outside scoped tasks (19.4). Item 196.
- A load of a view from a per-CPU variable is not yet a crossing, and a package variable holding a `shared[T]` used
  by two tasks is not yet E2010 (19.3, Crossings; 8.8). Item 214.
- A view kept across `c.Wait(&mu)` of a `sync.Cond`, or across a helper that waits on one, is not yet E3501
  (19.3, Views loaded under a lock). Items 214 and 222.
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
- The race check does not yet name an arena beyond the precision limits as every arena, or follow an arena's name
  through a move (19.3, Frees are writes). Item 217.

## 2. Behavior that differs at run time

- E2584, the race check and E3313 do not yet follow a `Reset`, `Release` or `Drop` of a Fixed or Chunked arena into
  the drops its entries would run (18.9, 19.3, 20.10). Item 209.
- The race check and E3313 do not yet treat a `Reset`, `Release` or `Drop` of an arena as a free of every object it
  takes back, of every type (19.3, 18.3, memory-model.md 3.8). Item 212.
- Waiting (a channel operation, `Lock`, `Wait`, `rt.Park`) inside a non-migrating region, reached through calls with
  one known target, compiles; it should be a compile error, and today it panics with "blocking with preemption
  disabled" at run time only when it parks (19.3). Items 210 and 215.
- A function marked `//pu:blocking` does not yet test preemption and the interrupt flag at entry, or before each call
  when it has no body, so calling one in a non-migrating region can hang instead of panicking (19.3, 20.3, abi.md
  12.4). Item 216.
- In kernel mode, `rt.Park` does not yet panic when interrupts are disabled, so a wait with interrupts disabled and
  no lock held is not caught at run time (19.3, abi.md 12.4). Item 213.
- A deadlock report does not name a panic in progress (19.1, 19.4). Item 205.
- Freestanding builds outside kernel mode do not yet insert explicit nil checks (17.5). Item 162.
- A handler's print can wait on a console lock its own processor holds (13.12, 20.10). Item 163.
- `vec`, `hash`, `strings` and `bytes` may not restore their invariants before every check that can panic (17.2,
  18.12). Item 165.
- A closure region does not yet run drop glue for every captured variable with cleanup (11.5, 18.8). Item 169.
- `Once.Do` does not release its lock when `f` ends its task with `rt.ExitTask`, so later calls of `Do` on that
  Once wait for good; F.5 says the next `Do` runs its own function. Item 218.
- A large owner lent to an ordinary interface does not yet give a data word that points at the variable (7.9,
  abi.md 5). Item 171.
- An owner declared in a for clause may not move between iterations and drop once (12.10, 18.6). Item 174.

## 3. Checks that are missing or too strict

- Comma-ok `ok` is not followed through direct tests, so some correct programs get E3302 (11.13, 18.5). Item 149.
- Interrupt handlers are not yet race parties from the point one may be installed (19.3). Item 156.
- `pluto vet -unsafe` still lists `x86.Pause`, `Cpuid`, `Rdtsc` and `Rdtscp`, and does not list `//pu:export`
  functions (18.12). Item 161.
- `[owner any]` may not parse as a type parameter named `owner` (4.6). Item 164.
- Conformance checks for `alloc.Default()`, the arena window, the panic buffer, F.3 and F.6 names and debug
  allocator tags are not all tested. Item 167.
- Data written only inside `once.Do` and read after `Do` is still E3503: the race check has no rule for
  `sync.Once`, so it infers the Once's own lock as the guard instead of following lazy initialization, and it
  does not reject a write outside the Once's function as such (19.3, Lazy initialization). Item 218.
- `rt.Yield` inside a non-migrating region compiles and does not panic, so a task can resume on another processor
  while it holds a view of per-CPU data (19.3, Switching tasks in a non-migrating region). Item 229.
- A function started by `time.AfterFunc` counts as started once, so two runs after a `Reset` may race on what it
  writes without an error (19.3, Parties). Item 230.
- A `pluto/sync` method call or channel operation is not collected as an atomic access, so freeing memory that
  holds a mutex another task locks is not rejected (19.3, Accesses). Item 231.
- A range body does not get the locks its iterator holds at `yield`, so a locked iterator is E3502 or E3501 as a
  range loop while the same literal passed to it compiles; a `Do` before a call does not count in the callee
  (19.3, Part of the enclosing function). This includes the method shape `range sc.All()`, whose closure locks
  `s.mu` (19.3, Locks and names). Item 232.
- Maps are grouped by type, so two objects that each guard a map field with their own mutex are E3502 (19.3, Maps
  kept in place). Item 192.

## 4. Smaller notes

- `pluto vet` has one check, `-unsafe`; there is no general lint. `pluto fmt` is the only style tool.
- Library names that Go has and Plutonium lacks are listed in `go-to-plutonium.md`.
