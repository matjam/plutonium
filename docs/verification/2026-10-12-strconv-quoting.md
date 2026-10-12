# Strconv quoting verification, native Linux amd64

Status: complete. The streaming hash is the next ordered smaller item.

Library quoting follows rune/string literal escaping (sections 4.10 and 4.11), counted
string lifetimes (section 18.8) and bounded Appender storage (appendix F.3). Ordinary
library API details belong in package documentation, as appendix F states. No language
rule, test oracle, timeout or memory cap changed.

## Implementation and exact oracles

All twelve APIs are implemented: `Quote`, `QuoteToASCII`, `QuoteToGraphic`, their
`QuoteRune` counterparts, and all six `Append` forms. They share a bounded rune
emitter in `lib/pluto/strconv/quote_format.p`; numeric formatting remains in
`format.p`. Existing Unicode print/graphic tables determine which characters stay
literal. Invalid rune values become U+FFFD. Explicit unsafe views of malformed UTF-8
escape each bad byte as lowercase `\xNN`; valid U+FFFD remains a distinct rune.

String forms measure and fill with the same emission routine, allocate exactly one
counted buffer, compute its ASCII flag from emitted bytes and release the builder's
reference. Rune forms use bounded stack storage. Append forms decode the full input
byte view and flush fixed stack batches, avoiding counted substrings and temporary
strings or growable intermediate buffers. A `vec.Fixed` keeps the byte prefix that
fits, even inside UTF-8 or an escape, and records discarded bytes as truncation.

`TestQuoteDifferential` compares 12303 hex-encoded cases with Go 1.27.1 at both O0
and O2. It covers every ASCII control and delimiter, Unicode printable/graphic
differences, supplementary and invalid rune values, valid U+FFFD, malformed UTF-8,
multibyte characters across 64-byte and batching boundaries, all twelve API forms,
prefixed Vec appends and Fixed capacities including zero, exact and one-short.
Output ASCII flags agree with the actual emitted bytes.

`TestQuoteAllocationLifetimes` uses a counting wrapper over the debug allocator.
At O0/O2, six string forms allocate and free exactly six buffers after independently
held array copies survive source overwrite. All six bounded Append forms, capacities
0 through 48 and a 600-byte input, add no allocations. The debug allocator reports
no leaks or invalid frees.

## Acceptance record

Native Linux amd64, Go 1.27.1, golangci-lint 2.14.0:

| Gate | Result |
|---|---|
| Full covered suite | Exit 0; build package 544.457 seconds |
| Plain full suite from tracked-only archive | Exit 0; build package 133.191 seconds |
| Lint | Exit 0; 0 issues |
| Full SSA build checks | Exit 0; 139.090 seconds |
| Full generation-off build suite | Exit 0; 79.527 seconds |
| Focused O0/O2 exact oracles and allocation checks | Exit 0 |
| Catalog/specification and repository formatting | Exit 0 |
| Archive compiler build and embedded hello | Exit 0; exact oracle with `PLUTOROOT` unset |
| Complete coverage comparison | Exit 0; aggregate nondecrease and every package floor |

Aggregate Go compiler coverage is 47599/54133 (87.92973%) against the complete
`e643296e` baseline 47598/54133 (87.92788%). Every package has identical covered and
total statements except unchanged-source `internal/opt`, which covers one additional
statement (3977 to 3978 out of 4196, 94.78074% to 94.80458%). This counter variation
is not attributed to the Plutonium library implementation. All required package
floors pass; the complete comparison and profiles are retained with gate logs and
numeric exit files under `/tmp/plutonium-strconv-quoting-verification`.

Plain verification used a staged 1818-file tracked-only archive, tree
`e7f2beaff637c45effc0af7e631214f96149abb0`. Its compiler and embedded hello match
the exact oracle. Final status documentation is included in a refreshed tracked-only
archive and rebuilt before shipping. No generated binary or ignored input is required.

## Failed-run evidence

The test-first native O0/O2 program reproduced existing `Quote` mismatches: raw
U+0085/U+00AD/U+2028, raw NBSP where plain quoting requires an escape, and raw
malformed bytes. The new emitter passes these exact Go oracles. The first expanded
harness lost output lines because its bounded scratch array was too small for long
hex results; increasing the harness output capacity preserved every case and oracle.
`test-first.log`, `differential.log` and their exit records preserve these failures.
No broad gate failed, and no existing golden or diagnostic output needed updates.
