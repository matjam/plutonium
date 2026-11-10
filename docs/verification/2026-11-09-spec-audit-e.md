# Spec audit E (v0.4, origin/main db2a1316)

Scope: the 61 rulings of docs/verification/2026-11-09-spec-rulings.md (B5 excluded as asked), and the
sections audits A to D left unread. All paths are under docs/spec/. Line numbers are at db2a1316.

## 1. Rulings

Verdicts: correct; incomplete (place named); wrong (reason). "Diff" means checked through the
135b0c14..db2a1316 diff and a grep for stale text, not by an attack.

| Ruling | Verdict | Notes |
|---|---|---|
| A1 | Incomplete | 18.3 "A local's scope" (lines 146-161) is right. 18.4 line 235-236 still says keeping a view and leaving the loop with `break` or `return` "is allowed", with no word on A1's E3313 for a local with cleanup. See E6. |
| A2 | Correct | 4.6 and E.x grammar agree. |
| A3 | Correct | 11.26 line 1684. |
| A4 | Correct | A.3 cites 12.10. Diff. |
| A5 | Correct | Diff. |
| A6 | Correct | Diff. |
| A7 | Correct | 8.7. Diff. |
| A8 | Correct | Diff. |
| B1 | Incomplete | 18.9 lines 777-789 state the count, but "owners are not counted" leaves owners made from a shared arena. See E3. Also "boxes" next to "owners are not counted" is unclear for `owned[T]`. See E9. |
| B2 | Correct | Diff. |
| B3 | Correct | "arena allocation freed" is gone everywhere. |
| B4 | Correct | Diff. |
| B5 | Not reviewed | Excluded by the brief. |
| B6 | Correct | Diff. |
| B7 | Correct | Diff. |
| B8 | Correct | Diff. |
| B9 | Correct | Diff. |
| B10 | Correct | 13.2 read in full. |
| B11 | Correct | Diff. |
| B12 | Correct | Diff. |
| B13 | Correct | Diff. |
| B14 | Correct | Diff. |
| B15 | Correct | 13.12 read in full. |
| B16 | Correct | No 512 left. |
| B17 | Correct | Diff. |
| B18 | Correct | Diff. |
| B19 | Correct | Diff. |
| B20 | Correct | Diff. |
| B21 | Correct | Diff. |
| B22 | Correct | Diff. |
| C1 | Incomplete | 11.24 rule 3 lets a package forge through a type it declares from the foreign owner (E2). 8.11 (lines 635-640) and the 7.18 summary (lines 1670-1680) omit rule 1's defined-from limit, so read alone they allow `p.Frame(MyFrame(0x1000))`. |
| C2 | Incomplete | The lending clause is sound for a large owner, whose data word points at the variable. For a one-word owner payload the interface holds a copy, and the D3 exemption lets it outlive a free. See E1. |
| C3 | Correct | Diff. |
| C4 | Correct | 7.12 and 11.24 agree. |
| C5 | Correct | Diff. |
| C6 | Correct | Diff. |
| C7 | Correct | Diff. |
| C8 | Correct | Diff. |
| C9 | Correct | Diff. |
| C10 | Correct | Diff. |
| C11 | Correct | 11.24. |
| C12 | Correct | 11.24. |
| C13 | Correct | Diff. |
| C14 | Correct | Diff. |
| D1 | Correct | 19.3 line 216 and 19.4 E3404 state it. A separate gap in when the scoped interval ends: E4. |
| D2 | Correct | 18.6 and 11.5 agree; no attack found. |
| D3 | Incomplete | The exemption at 18.3 line 131 covers "an ordinary interface that lends it", which is a copy for one-word values (E1). The function-literal sentence (line 143-144) and the E3314 list (lines 163-167) omit the owner field assignment event (E5). |
| D4 | Correct | 18.5 line 315, 12.19 line 1194. |
| D5 | Correct | 18.5 line 332, 18.x line 1137. |
| D6 | Correct | Diff. |
| D7 | Correct | 12.10 line 580. |
| D8 | Correct | 18.6 line 411. |
| D9 | Correct | Diff. |
| D10 | Correct | memory-model 4.2 now reads "not undefined behavior of the program". |
| D11 | Correct | Diff. |
| D12 | Correct | Diff. |
| D13 | Correct | Diff. |
| D14 | Correct | 11.5 line 252. |
| 7.17 | Correct | 7.17 line 1587. |
| 7.18 | Correct | The view rule is right. The surrounding 7.18 summary has the C1 omission. |
| 7.19 | Correct | 18.3 lines 138-142. |

Totals: 55 correct, 5 incomplete (A1, B1, C1, C2, D3), 0 wrong, 1 not reviewed (B5).

## 2. New findings

### E1. A lent one-word interface outlives a free (high)

Kind: safety hole. Sections: 18.3, 7.9, abi.md 5, A.2.

- 18.3 line 131: "A view of the variable itself (`&v`, or an ordinary interface that lends it, section
  7.9) is not ended by a call or field assignment".
- 7.9 lines 851-860 and abi.md 5: a one-word direct type (an `owned`, `shared` or `synced` box, a
  channel, a map, `weak`, a pointer, a one-field struct of a pointer) is copied into the data word. Only
  "An owner of any other type lent to an ordinary interface is not copied" (abi.md 5).

So for a one-word owner the "view of the variable itself" holds the old pointer, not the variable's
address. The exemption then keeps it alive across the free.

```go
func reset(p *owned[T]) { *p = new(T) }   // drops the old box

b := new(T)
x := any(b)       // lends b; data word = old box pointer
reset(&b)         // exempt from D3: x is "an ordinary interface that lends" b
fmt.Println(x)    // reads the freed box
```

Same with `type W owner struct{ p owned[T] }`, `x := any(w); w.p = new(T)`, with a lent map or channel
and `setNil(&m)`, and with the C2 lending assertion from an owning interface whose payload is one word.
Checked builds catch it with the generation check. Unchecked builds read freed memory. This breaks 18.11
("the rules of 18.3 prove at compile time") and memory-model 4.1.

Resolution: limit the exemption to `&v` and to an owner larger than one word, whose data word points at
the variable. An interface holding a copied one-word owner is a view obtained from `v`, ended by the D3
events. This keeps plain code free of annotations (the error only fires when code frees under a live
interface), adds no hidden cost, and keeps abi.md 5. The other fix (make every lent owner point at the
variable) changes the ABI and costs an indirection on every call through the interface.

Promise change: yes. New E3313 errors on programs that use such an interface after a freeing call.

### E2. Rule 3 lets a package forge a foreign owner (high)

Kind: safety hole. Sections: 11.24 lines 1549-1579, 8.11 lines 635-640, 7.18 lines 1670-1680.

- Rule 1 has the defined-from limit: "When the owner type `B` is defined from another owner type `A`
  ... allowed only where a conversion to `A` is allowed too".
- Rule 3: "allowed only in the package that declares `T`, or in the package that declares the
  operand's type when that type is defined from `T`, directly or through other owner types". Its "the
  limit of rule 1 applies" does not say the defined-from extension applies to the target.

```go
package q
import "p"
type Fake owner uintptr
type MyFrame owner p.Frame

func forge() p.Frame {
    f := Fake(0x1000)       // q declares Fake
    mf := MyFrame(f)        // rule 3: q declares MyFrame
    return p.Frame(mf)      // rule 3: operand type declared in q, defined from Frame
}
```

Pointer forms pass the same way: `(*MyFrame)(&f)` then `(*p.Frame)(&mf)`. 8.11 and the 7.18 summary
omit the defined-from limit of rule 1, so under their text `MyFrame(0x1000)` is also allowed.

Resolution: in rule 3, when `T` is defined from an owner type `A`, the conversion is allowed only where a
conversion to `A` is allowed too. Copy rule 1's full limit into 8.11 and 7.18, or have them cite 11.24
instead of restating it. This is what C1 meant: only `p` makes a `p.Frame`.

Promise change: yes. These conversions become errors. No correct program needs them.

### E3. Owners from a shared arena escape the B1 count (high)

Kind: safety hole (race and use after free). Sections: 18.9 lines 777-789, 13.9, 19.3.

- 18.9: "Plain views (`a.New[T]()`, `a.Slice[T](n)`) and owners are not counted."
- 13.9: `new(T, a)` gives an `owned[T]`. A `vec.Vec` grown while `alloc.Use(&s.arena)` is active keeps
  its buffer in the arena.

```go
b := new(T, &s.arena)      // owned[T], not counted
ch <- b                    // moved to another task; the 19.3 move rule does not see the arena
s.arena.Reset()            // count is 0, so Reset succeeds
// the other task now uses freed memory, racing with the next allocation
```

This is the audit B1 race. B1 counts strings, maps and channels for this reason, but the owner path is
open. The epoch check does not help: the epoch is read before the reset.

Resolution: count every allocation made through the arena's `Alloc` that is not freed yet, owners
included, and leave only plain `New` and `Slice` views uncounted (they are bounded views and cannot leave
the window). One counter increment per allocation keeps the cost near Rust's. The other fix (forbid
moving an arena-backed owner to another task) needs tracking the compiler cannot do without annotations.

Promise change: yes. `Reset` and `Release` panic in more programs.

### E4. Deferred calls run inside a scoped interval with no rule (high)

Kind: safety hole (race and use after free). Sections: 19.4 line 419, 19.4 E3404 lines 542-547.

- 19.4 line 419: "In a function's outermost block the drop runs after the deferred calls".
- No text says deferred calls belong to the interval of a task not yet joined. A grep of "interval" with
  "defer" finds nothing.

```go
v := vec.New[int]()
defer v.Drop()                 // runs before the Scope drop joins
var s sync.Scope
s.Task(func() { v.Push(1) })
return                         // v freed while the task may still push
```

A deferred function literal that writes a captured variable races the same way.

Resolution: a deferred call that can run while a task of a local Scope is not joined counts as a
statement of that task's interval, for the Sharing rules and for E3404. This keeps the D1 promise with no
run-time cost.

Promise change: yes. New E3404 errors (fix: declare the Scope before the deferred call, or call
`s.Wait()`).

### E5. D3 events missing from closures and the E3314 list (medium)

Kind: incomplete rule. Sections: 18.3 lines 143-144 and 163-167.

- Line 143-144: "calling it after the owner was moved, dropped or given a new value is E3313 too".
- Line 165-167, E3314: "moved, dropped, given a new value, or passed by pointer to a call that may
  free".
- Neither names the owner field assignment on the view's path, which the D3 bullets (lines 118-129) add.

```go
p := &*n.child
f := func() { println(*p) }
n.child = new(Leaf)     // drops the old child
f()                     // reads freed memory; no listed event fired
```

The D3 bullets may cover it by reading, but these two lists name their events and leave this one out.

Resolution: name all D3 events in both lists, or have them say "ended as section 18.3 lists".

Promise change: no (it states what D3 intended).

### E6. 18.4 still calls a kept view "allowed" (low)

Kind: stale wording. Section: 18.4 line 235-236: keeping a view and then "leaving the loop with `break`
or `return` is allowed". A1 makes using that view after the loop E3313 when the loop variable has
cleanup.

Resolution: add "unless the variable has cleanup (section 18.3, A local's scope)".

Promise change: no.

### E7. Appendix A row 80 is false for one-word values (medium)

Kind: false friend in the list of differences. Section: a-differences-from-go.md line 80: an ordinary
interface "is a view of the converted variable, not a copy, and sees its later changes". 7.9 and abi.md 5
copy a one-word value into the data word, so `x := any(n); n = 5; fmt.Println(x)` with `n` a pointer or
a map variable shows the old value. 7.9 line 798 says the same as row 80 ("valid while that variable
holds the value").

Resolution: say which values are views (large values and large owners) and which are copied (one-word
direct types), in 7.9 line 798 and in row 80. Fixing E1 makes the copy case safe; this makes it visible.

Promise change: no for the behavior (abi.md 5 already does this); yes for the stated rule.

### E8. Appendix A row 111 cites the wrong section (low)

a-differences-from-go.md row 111 cites 12.19 for "at most 64" deferred calls. The limit is stated in
chapter 21 (line 115 area), not 12.19.

Promise change: no.

### E9. "boxes" in B1 is unclear (low)

18.9 lists "boxes" among the counted objects and then says "owners are not counted". `owned[T]` is a box
and an owner. Name the box kinds: `shared[T]` and `synced[T]`. If E3 is accepted the sentence changes
anyway.

Promise change: no.

## 3. Checked and dropped

- Generic forging with `T(x)` for a type parameter `T` whose set holds a foreign owner: closed by the
  type-set rule, 11.24 line 1451-1455 ("x can be converted to each type in T's type set").
- A nested arena whose parent resets: the parent is a stated lifetime dependency and the checked epoch
  covers it.
- Releasing a string from another task: only the atomic count is touched; memory-model 3.8 gives the
  ordering.
- A vec range whose body shrinks the vector: 12.10 says the loop stops early.
- A returned closure after the function moves a captured owner: the closure sees a zero value; 11.4
  allows the move.
- A captured parameter in a closure region: 18.6 agrees with D2.
- Region refill while a closure from the previous fill is live: E3101.
- Closures stored in a returned struct or interface: the region rule covers them.
- A captured view used by a call inside the literal: E3314.
- Range over a map or string with the variable reassigned in the body: D7 temporary count (12.10 line
  580).
- Ring `Peek` view across `Pop`: 7.17 line 1587.
- A view through one `shared` copy across a free through another copy: 7.19 text at 18.3 lines 138-142.
- Stale-text greps found nothing for B1 (old sentence), B3, B16, A2, A3, C4, C11.
- Chapter 13 against B10, B15, 13.7 maps in arenas and the D58 print rule: consistent.
- 20.1 `unsafe` conversions: consistent with 18.12.
- memory-model 4.1 and 4.2: consistent with D1 ("freeing or moving it ... is a change") and D10.
- abi.md 7, 8 and 11: no conflict with the language chapters. The 8 symbol rows for closure regions and
  ring iterators match 11.5 and 7.17.

## 4. Sections not read closely

- Chapter 20 after 20.1 (20.2 to 20.11): headings only.
- Appendix A: rows A.2 to A.5 read; rows elsewhere spot-checked.
- memory-model.md: 3.8 to 4.2 read; 4.3 onward not reread.
- Rulings marked "Diff" above: checked through the diff and greps, not attacked.
- debugging.md and debug-allocator.md: only the B19 and B22 lines.
