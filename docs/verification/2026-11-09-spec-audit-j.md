# Spec audit J: closing audit of version 0.4

Checkout: detached at c7e55d0f. The I edits were read as `git diff 0b16b144 c7e55d0f -- docs/spec`.
Section numbers refer to `docs/spec/language/` unless a file is named.

## 1. Verdict per I ruling

| Ruling | Result | Note |
|---|---|---|
| I1 (loads of views cross) | Partly resolved | It closes the `p = &*b` hole. It also makes ordinary guarded globals E3501 (J1, J2), and it starts too late for handlers (J3). |
| I2 (caller or callee paths) | Resolved | 19.3 line 242 counts paths from the region, and a wait on only some paths of a followed callee is reported. Lines 288 to 291 agree. |
| I3 (`//pu:blocking` test) | Resolved | 19.3, 20.3 lines 290 to 300, abi.md 12.4 line 1368 and 17.1 agree. The test at the entry of `park` repeats one `park` already makes. That is harmless. |
| I4 | Resolved | Wording. |
| I5 | Resolved | Wording. |
| I6 | Resolved | Wording. |
| I7 (arena names) | Resolved | 19.3 Frees are writes and D100 agree. |
| I8 | Resolved | Wording. |
| New E2010 case | Resolved | 8.8 makes a type that holds a `shared[T]` not sendable. Handlers are covered by "Rules 1 to 3 apply with handlers as parties". |
| Unbacked claim: silent hang | Backed | I3 gives the test at entry. |
| Unbacked claim: race freedom | Not fully backed | J3 leaves handler loads before the first task unchecked. |

## 2. I1 walk-through of five Go-style programs

1. **Cache map under a mutex.** In Go: `map[string]*Entry` under `mu`. Here `map[string]owned[Entry]` is
   illegal (7.10 line 992: "Neither the key type nor the element type of a map may be an owner"). The
   legal form is `map[string]synced[Entry]`. Copying the box out under `mu` is a counted copy, and the
   object is safe by rule 3 when its changing fields are guarded or atomic. It compiles. Written with
   `hash.Map[string, owned[Entry]]` under `mu`, it is E3501 by J1.
2. **Global logger.** A logger that writes to `os.Stderr` from several tasks is E3501 by J2 in any
   program that drops a `File` after tasks start. `fmt.Println` hits the same rule through `os.Stdout`.
3. **Registry filled at init.** It compiles. Rule 1 lets every task read a table written only during
   initialization. The pointers in it cross when loaded. Records that are static, or whose type meets the
   type rule, pass rule 2.
4. **Config pointer swapped atomically.** Go code stores a new `*Config` and lets the GC free the old.
   Here freeing the old one is a write (Frees are writes), so a task that loaded the old pointer gets
   E3501. Written as a `synced[Config]` copied out under a mutex, it compiles. This is the intended
   answer and the spec says so ("such an object is shared as the object of a `synced[T]`").
5. **Connection pool.** `vec.Vec[owned[Conn]]` under a mutex is E3501 by J1: `Pop` and `Push` load
   `v.data`, a slice of owners, from package memory that tasks write. A buffered `chan owned[Conn]`
   compiles: owners move, and a channel is not a view.

Result: two of five ordinary Go shapes (logger, mutex pool) fail with no fix the error text offers, and
the cache fails if the user picks the library map. That is the core of J1 and J2.

## 3. New findings

### J1. A container in a guarded package variable is E3501

- **Kind:** false positive; goal conflict ("ordinary code needs no annotations", "Go-familiar").
- **Severity:** medium. Ordinary code hits it, and two careful implementers would read it two ways.
- **Sections:**
  - 19.2, crossing: a load "from a package variable ... or from memory reached through one of those,
    crosses to the task that loads it".
  - 19.3 rule 2, case 2: "a package variable, or part of one, including the memory it owns (rule 1),
    that no task writes once tasks have started".
  - 19.3 rule 2: "A pointer to, or slice of, anything other than a struct crosses only in the first three
    cases."
  - Case 3 names only "the object of a `synced[T]`", not memory that object owns.
  - Against this, 8.8 says a view may cross when it points to memory "where every change is
    synchronized", and memory-model.md 4.1 says rule 1 covers "the memory it owns".
- **Example:**

  ```go
  var (
  	mu    sync.Mutex
  	items vec.Vec[int]
  )

  func add(x int) { mu.Lock(); items.Push(x); mu.Unlock() }

  func main() { for i := 0; i < 4; i++ { task add(i) } }
  ```

  `Push` (lib/pluto/vec/vec.p line 146) loads `v.data`, a `[]int`, from memory `items` owns. The load
  crosses. The slice is not of a struct, so only cases 1 to 3 apply. Case 2 fails, since `Push` writes
  that memory. So it is E3501 by the letter, although every access holds `mu`. The same holds for
  `synced[struct{ mu sync.Mutex; v vec.Vec[int] }]`, and for `hash.Map` and `strings.Builder`. The
  rulings attack at docs/verification/2026-11-09-spec-rulings.md lines 295 to 299 rejects this shape too.
- **Proposed resolution:** add a rule 2 case: "memory a package variable or the object of a `synced[T]`
  owns, when every access to it in every party holds the guard rule 1 or rule 3 inferred for the
  variable or object, and the view is used only while that guard is held". A view kept past the
  `Unlock` stays E3501. This reuses the guard inference rules 1 and 3 already make, needs no annotation,
  and keeps the safety argument: no party can touch the memory while the guard is held.
- **Rejected:**
  - Exempt the library containers by name. That does not cover user containers built the same way, and
    it is a false friend.
  - Treat loads inside unsafe library code as exempt. It hides the case rather than proving it, and user
    code that indexes `items.data` directly still fails.
  - Leave it and tell users to use channels. It breaks the most common Go shape with no error text that
    points to a fix.
- **Changes a promise:** no. It narrows a false positive. The race-freedom promise holds with the guard
  condition.

### J2. Every task that prints is E3501 once a file is dropped

- **Kind:** false positive; goal conflict (ordinary code).
- **Severity:** medium. This is the most serious finding: `fmt.Println` from a task is the first thing a
  Go user writes.
- **Sections:**
  - lib/pluto/os/os_linux.p lines 205 to 207: `Stdout = &File{...}` and the like. These are static
    objects the compiler creates in an initializer (15.4, lines 298 to 316).
  - 19.3 rule 2, case 2 covers "a package variable, or part of one". A static object an initializer
    creates is neither.
  - The `File` type writes `fdmu` atomically on each operation (os_linux.p lines 161 to 181), so it is
    not "memory that no task writes".
  - The type rule fails as soon as any code drops a `File` once tasks run ("frees, drops or moves one").
    `File.Drop` frees through `f.raw.Free()` (line 190).
- **Example:**

  ```go
  func work(name string) {
  	f := os.Open(name) or return
  	fmt.Println("opened", name) // loads os.Stdout, a *File: crosses
  	_ = f                       // f is dropped here: frees a File once tasks run
  }

  func main() { task work("a"); task work("b") }
  ```

  The `*File` loaded from `os.Stdout` crosses. Case 1 fails (`File` holds more than sync types). Case 2
  fails (not a package variable). Case 3 fails. The type rule fails because some `File` is dropped while
  tasks run. So it is E3501.
- **Proposed resolution:** extend case 2 to static memory: "a package variable, or part of one, or a
  static object an initializer created (15.4), that is never freed while tasks run, and whose fields
  each never change once tasks start, or are used only atomically, or only under one lock of the same
  object or one package-level lock". Static objects are never freed by 18.3 and are cleaned only after
  all tasks end (15.4). So the free condition of the type rule is not needed for them, and the field
  conditions stay.
- **Rejected:**
  - Make `os.Stdout` a `synced[File]`. It changes a Go-familiar API, and every user global with the same
    shape still fails.
  - Drop the type rule's free condition for all objects. That reopens the use-after-free D59 closed.
- **Changes a promise:** no.

### J3. Handler loads before the first task are not crossings

- **Kind:** safety hole in safe kernel code; rule conflict.
- **Severity:** medium. It is a hole, but only between the first handler and the first task, in kernel
  builds. The fix is one phrase.
- **Sections:**
  - 19.2, the crossing bullet: the load counts "once a task other than the main task may run". It does
    not name handlers.
  - 19.3 "The tasks" (lines 167 to 176) and memory-model.md 3.1 line 97 start handlers as parties at "the
    first statement that may install an interrupt handler ... or enable interrupts". D52 says the same.
- **Example:**

  ```go
  var (
  	b owned[Node]
  	p *Node
  )

  //pu:interrupt
  func tick() { println(p.n) }

  func main() {
  	b = new(Node)
  	p = &*b
  	idt[32] = x86.InterruptGate(tick, kernelCS, 0) // installs tick (20.8)
  	x86.Sti()
  	b = new(Node) // frees the Node tick reads; no task has started
  }
  ```

  Rule 1 applies from the handler install. But the 19.2 bullet says the load by `tick` crosses only once
  a task other than main may run, so rule 2 never checks the `Node`. A reading that follows "The tasks"
  would reject it. Two implementers differ.
- **Proposed resolution:** in 19.2, write "once a task other than the main task, or an interrupt
  handler, may run (section 19.3, The tasks)". This matches D52, rule 1 and memory-model.md.
- **Rejected:** leave it to "An interrupt handler that loads such a value is a party in the same way".
  That sentence inherits the same start condition, so it does not fix the start.
- **Changes a promise:** no. It makes the race-freedom promise true as written.

## 4. Record problems (task 3)

- **R1.** The rulings attack for I1 (docs/verification/2026-11-09-spec-rulings.md line 298) uses
  `var m map[int]owned[Node]`, which 7.10 line 992 forbids. The map case was not tested on a legal
  program. The legal shape is `map[int]synced[Node]`, which passes by rule 3.
- **R2.** roadmap.md "Order of work": phase 1 names items 146 to 176, 177 to 183, 196, 211, 214 and 217,
  but no phase names items 198 to 210, 212, 213, 215 or 216. These are ruling work items from audits E
  to I. Item 203 is an optimization and is not placed either. Phase 0 still says "done: decisions D42 to
  D69", though decisions now run to D101.
- **R3.** decisions.md D87 narrows D85 ("The compiler follows calls as the E2584 analysis does") but does
  not say it replaces that part of D85. The "Superseded decisions" section records D87 being replaced by
  D97, but not D85 being narrowed by D87.
- **R4.** decisions.md D70 says "this extends D45", but it renames D45's panic from "arena has live
  counted objects" to "arena has live allocations". D45 still states the old text with no pointer to D70.

Checked and consistent:
- Items 146 to 217 each appear once in roadmap.md.
- No planned item describes a dropped rule. Item 210 no longer calls hooks exact, and item 215 says not to
  follow them.
- known-gaps.md (skill/plutonium/references/known-gaps.md) has a line for every planned item that changes
  what a program does. The items without a line (181 to 195, 197 and 203) cover tools, tests,
  optimizations and precision.
- The "Diagnostics that need a code" list agrees with 20.3, 21.4 and 19.3. Item 207 tracks it.

## 5. Checked and dropped

- Rules that name no code: 1.3 covers them ("A rule that names no code is reported with a code of the
  group of section 21.1 it belongs to").
- 19.7 against memory-model.md section 3: consistent.
- abi.md 12.9 against 19.6 on what timer functions may do: consistent.
- E2010 for handlers: covered by "Rules 1 to 3 apply with handlers as parties".
- Boxes are not in the 19.2 list of views: they are covered as owned memory under rule 1.
- The I3 test in `park`: a repeated test, harmless.
- 18.13 examples: valid.
- `vec.New` against `vec.Make`: both exist.
- D52 against D96: they agree, except for the start condition, which is J3.
- D59 against D86: D86 widens D59's frees to arena frees of every type, and says so.
- D46 against D74 and D82: D82 says it follows D74's entries. D46's bulk-only rule stands.
- D93 and D100: D100 adds to the naming rule without changing it. There is no conflict.
- Item 192 (maps of one type guarded together): precision, matches 19.3 rule 3.

## 6. Sections not read closely

- 19.3 Frees are writes and rule 3 were read for J1 and J2 only, not line by line for other issues.
- memory-model.md 3.7, 3.8 and 4.1, and chapter 21.5: outside the brief.
- roadmap.md items before 146, and the done items: read only for phase placement.
- decisions.md D42 to D101: read in full for D45, D46, D52, D59, D70, D74, D78, D82, D85 to D88, D93 and
  D96 to D101. The rest were skimmed by title and "Lands with" line.
- The library, beyond vec.p, os_linux.p and print_linux.p.

## 7. Judgment

The spec is not closed. Three medium findings stand:

1. **J2:** add static objects to rule 2 case 2, with the type rule's field conditions and without its
   free condition.
2. **J1:** add a rule 2 case for memory a package variable or a synced object owns, when every access
   holds the inferred guard and the view is not used past it.
3. **J3:** start the 19.2 load crossing at the first handler install, as 19.3 and D52 do.

Each fix is a sentence or two in 19.2 and 19.3, plus matching lines in 8.8, memory-model.md 4.1, D96 and
item 214. None changes a promise. The four record problems are low and do not block closing.
