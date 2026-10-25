# escape2 precision follow-ups (coordinator, 2026-10-24)

Two precision limits found while clearing the translated compiler's false
findings. Both are sound as they stand; each costs false findings in
ordinary code, so both belong before the stage-7 default switch.

## 1. Calls through a fallback-applied closure's captures

A dcall that falls back applies every literal of the signature, each with
its record bound to the rest of the function value: memory the caller's
inputs reach. A literal that calls one of its captures (deriveKey's `star`
calling `overflowed`) then makes a deferred call whose function value is
that record field, an In token of the caller's input. It is deferred again
at every caller, and at the top it falls back once more with the record
bound to heap memory, saturating the summaries it passes through (3,212
summaries held star's entry on escape2demo).

The internal/escape source was restructured to avoid it: deriveKey's
closures are now methods of keyWalk. The general fix is a capture fact.
Of each literal L, for each capture i, record the set of literals whose
closures every OpClosure of L stores in that capture, when that set is
known: a capture by value of an OpClosure register, or a captured variable
whose every store is such a register and which no other closure writes. A
call of L through capture i then resolves to that set, with the record of
the callee bound to the load of the capture, instead of being deferred.

## 2. Store pairs of one input applied as a product

A store pair whose source and destination name the same input (a
memoizing `a.actual = a.fromRHS`, types2.unalias) is applied at the
caller as the product of every token of the binding's source with every
token's destination. When the binding holds heap memory and the caller's
own input, that product stores the input's memory into heap memory: an
escape that cannot happen, since the input is one pointer when the call
runs. This saturates types2's assignableTo/implements (17-22 findings on
escape2demo).

A per-pointer application was built and tested (patch in
2026-10-24-escape2-selfstore.patch, without its test file; TestSelfStorePerPointer shows `esc 0.0*;
heapstore 0.1` becoming `store 0.0*->0.1`): each binding token tagged
(Root, false) applies the pair alone; any other token applies it to the
whole binding. It is sound and the 30k differential agrees, but on pluto
it raised findings from 9 to 40: the escapes it removes become many
distinct store pairs, past MaxStorePerInput, and widenPairs' cut of both
keys (rootKey) is coarser than the escapes were. It needs a store-pair
widening that keeps a pair of one input within that input (a cut that
leaves both keys on the input's own memory, never turning into an escape)
before it can land.

## 3. Self-stores through a joined value

A callee that stores memory loaded through its input back into that same
input (`(*vec.Vec).Append` stores `v.data`'s buffer into `*v`) has the
pair `in0[L] -> in0[]`. When the caller passes a local that joins two
values (`s := out; if c { s = errOut }; s.write(p)`), the pair becomes the
product `out -> errOut`, `errOut -> out` and so on, and internal/moves
reports a view stored into the other value (E3314), though each value
only ever holds its own buffer. os/exec's pump now writes each sink
through its own name. A fix in the analysis would keep the pair
relational: a store whose source and destination name the same joined
local applies to each member alone.

## 4. Widening order

widenKeys cuts through the second load before the first (cutTwo, then
cutKey, then the root, then the filters), so an escape within a captured
closure's record stays within the field it names. widenPairs coarsens a
pair's destination before its source: the source names what the caller's
view checks follow, and a destination cut to its root still says which
argument the pointers land in.

## 5. Binary data in strings

The self-hosted compiler's strings hold valid UTF-8: converting a
`[]byte` to a string replaces invalid bytes with U+FFFD, so two distinct
binary keys can become equal. The scheduler and defer map keys collided
this way and are now decimal text (16077a64). The summary cache still
keeps binary data in strings (cache/file.go `string(e.sum)`,
cache.go `string(d[:])`, format.go `str()`); it runs only when
`cache.EnvVar` is set, and should switch to hex or another text form
before the cache is used under the self-hosted compiler. go2pu could
warn on `string(b)` of a byte slice built with encoding/binary.
