# Streaming hash verification, native Linux amd64

Status: complete. `pluto vet -unsafe` is the next ordered smaller item.

These ordinary library APIs are defined by package documentation, as the introduction
to appendix F requires. Their self-contained structs and key arrays copy as values
(sections 8.1 and 18.1); unsigned arithmetic wraps as section 11.19 specifies. No
language rule, runtime map mixer, compiler hashing implementation, entropy source,
test oracle, timeout or memory cap changed.

## Implementation and exact oracles

`hash.FNV32a` and `hash.FNV64a` are allocation-free, zero-ready FNV-1a streams.
Both implement `Write`, `WriteString`, `WriteByte`, `Reset` and nondestructive sums
(`Sum32` and `Sum64`). Writes consume every byte and return no error. Separate
initialization flags preserve a valid zero hash state. The FNV32a preimage
`cc2431c4` hashes to zero and, after empty writes and byte `2a`, to `704659998`;
the test verifies the zero preimage with Go's `hash/fnv` before compiling its corpus.

`hash.SipHash24` implements SipHash-2-4 with a caller-supplied 16-byte key and
64-bit result. `NewSipHash24` and `SetSeed` copy the key; `SetSeed` resets the
stream. `Seed` returns a copy, `Reset` preserves the key, assignment copies the
state independently, and `Sum64` finalizes a value copy. The stream retains no
input views and uses fixed state, a packed eight-byte tail and length counter.
Every operational method on an uninitialized value panics, including empty writes,
`Seed`, `Reset` and `Sum64`. An explicitly supplied all-zero key is permitted.

The algorithm and vectors are attributed to Jean-Philippe Aumasson and Daniel J.
Bernstein, whose [reference implementation](https://github.com/veorq/SipHash)
offers CC0. An independent one-shot Go oracle first passes all 64 published
[64-bit vectors](https://github.com/veorq/SipHash/blob/master/vectors.h): key
bytes `00..0f`, message prefixes `00..3f`, little-endian results. FNV outputs
are compared with Go 1.27.1 `hash/fnv`. This is algorithm equivalence with fixed
keys, not output equivalence with randomized Go `hash/maphash`.

`TestHashStreamingDifferential` compares 602 key/message pairs at O0 and O2,
each with many partition, copy and mixed-write checks. Seven keys include the
official key, all-zero, reversed bytes and four reproducible random keys. The
86 messages include every byte value, malformed byte views for `WriteString`,
all official vector lengths, all tail sizes, lengths 255/256/257 and 4096 bytes.
It tests every split for messages up to 257 bytes, representative larger splits,
empty writes, repeated sums, continued writes, byte-by-byte copied-state
continuations, successive mixed writers, reset, changed keys and key-copy isolation.

Package tests cover the known FNV32a zero result, `io.Writer` conformance, copied
streams and reset preserving the supplied key. `TestBuildTest` now includes
`pluto/hash`, so these package tests run in the full native and SSA suites.
Exact panic fixtures cover all six uninitialized SipHash operations at O0/O2.
The debug allocator observes zero added allocations, zero live allocations and
zero live bytes across 300 rounds of stream, interface, key, reset and copy operations.

SipHash's collision defense requires a secret unpredictable key, which callers
must obtain explicitly. FNV remains unsuitable for attacker-controlled keys.
This library phase neither migrates `types2` hashing nor supplies `WriteComparable`
or entropy. The later compiler port must encode comparable values explicitly and
acquire its key. Hash performance is unmeasured; tuning remains a later ordered item.

## Acceptance record

Native Linux amd64, Go 1.27.1, golangci-lint 2.14.0:

| Gate | Result |
|---|---|
| Full covered suite | Exit 0; build package 524.098 seconds |
| Plain full suite from tracked-only archive | Exit 0; build package 132.944 seconds |
| Lint | Exit 0; 0 issues |
| Full SSA build checks | Exit 0; 139.472 seconds |
| Full generation-off build suite | Exit 0; 80.297 seconds |
| Focused O0/O2 oracles, panic, allocation and package tests | Exit 0; 4.188 seconds |
| Complete coverage comparison | Exit 0; aggregate nondecrease and every package floor |
| Catalog/specification and repository formatting | Exit 0 |
| Archive compiler build and embedded hello | Exit 0; exact oracle with `PLUTOROOT` unset |

Aggregate Go compiler coverage is 47602/54133 (87.93527%) against the complete
`6eb94f75` baseline 47599/54133 (87.92973%). Unchanged-source `internal/race`
covers four additional statements (3874 to 3878 out of 4216), and
`internal/syntax/token` covers one fewer (399 to 398 out of 472). All other
package counts are identical; no source in these packages changed, and these
counter variations are not attributed to the library implementation. All package
floors pass. Gate logs, complete profiles and numeric exit records are retained
under `/tmp/plutonium-streaming-hash-verification`.

Plain verification used a staged 1826-file tracked-only archive, tree
`cdaac485ddf6981f9eec4b7a483a0e127c522c4a`. Its compiler and embedded hello match
the exact oracle. Final status documentation is included in a refreshed archive
and rebuilt before shipping. No generated binary or ignored input is required.

## Settled arena-epoch documentation consistency

A separate three-paragraph clarification in sections 1.2, 18.11 and 21.5 qualifies
the bounded-check rule with the already documented hosted arena-epoch limit.
The user's accepted 2026-10-10 decision and ABI section 1.3 allow a stale hosted
arena pointer to match again after 32,768 epochs; a kernel encoding retires the
chunk or widens the field before wrapping. Heap generations retire before wrapping,
as the accepted 2026-10-12 directory decision requires. The root review recorded
this existing authority before the edit. This corrects contradictory absolute
wording without changing the specification's established rule or runtime behavior.

## Failed-run evidence

Before implementation, the O0/O2 differential test failed because the streaming
API did not exist. The first exploratory inverse-FNV script included an extra
multiplication and produced an invalid claimed-zero fixture. Package tests rejected
it. Correcting that arithmetic supplied `cc2431c4`, and the Go oracle now verifies
the fixture before use. The production implementation did not require a change.
The failed `test-first.log` and `package.log` remain preserved. An initial archive
hello comparison ran before the command completed and observed an empty output;
after waiting for the successful run, the exact comparison passed. Its premature
exit record remains preserved separately. No oracle, compiler rule or default
resource limit was weakened.
