# Exact-audience stream ticks

Stream C's new readers remain disabled and unregistered. The existing SSE paths
and their privacy regressions stay authoritative until complete semantic cutover.
This package has no SQL connection/factory and consumes only typed, transaction-
bound repository operations. `Stream` aliases the frozen `scopes.Stream`.

`Tick` validates K<=64 scopes, S<=256 exact scope/family/audience streams, at most
four per scope, duplicates, row/byte budgets and encrypted cursor shape before
opening the repository. The repository must validate every scope and current
personal/role binding before **any** change range query in that transaction.
Every tick gets a fresh snapshot. Security binding includes principal generation,
scope lifecycle generation, membership generation and the exact role/audience
binding generation. Retention generation/watermark are checked separately and
request bounded resync when compacted. No workspace-global sequence is used.

The exact range key is `(scope_id,family,audience_key,seq)`. Each tick seeks at
most P+1 metadata heads per stream, P<=100, with a merge selecting a bounded
output batch before one hydration operation. Unemitted heads and per-stream
emitted positions are authenticated/encrypted. Each emitted item gets a cursor
for **its own** prefix, so disconnect after the first frame cannot acknowledge
the rest of a page. Unchanged idle ticks preserve the identical opaque token;
wrong-audience and inaccessible-scope activity cannot advance it.

Payloads are <=16 KiB; token wire size is <=256 KiB. Output admission charges
the actual encrypted per-item cursor, payload and an 8 KiB envelope reserve, with
a 1 MiB maximum tick budget. Smaller configured budgets must allow one maximum
token/payload/envelope. An oversized token state returns an explicit budget error.
`server/stream.ScopeHandler` uses JSON encoding with HTML escaping disabled so
raw JSON payloads do not expand sixfold after admission. Source newlines still
cannot create extra SSE frames. It sets per-write deadlines, emits constant idle
keepalives and a metadata-free reset on reauthorization failure. Register only
through `stream.Mount`, behind the regular authentication/classifier dispatcher.
Nil readiness keeps the helper disabled before any query.

`ApplyCanonical` forms bounded before/after projections from up to four frozen
transaction-local Changes. It validates all destinations before its writer and
loads no historical row. The writer must append each change and allocate that
exact stream's local sequence atomically with canonical state. It must enforce
scope provenance and audience destinations; fanout is never per-member. The
source hook must make role/personal destinations disjoint or define resource
deduplication before admission as the design requires.

`Store` implements `scopes.StreamTicker`. The frozen shared StreamChange carries
only a final page cursor, so this adapter emits at most one item per page to
preserve SSE disconnect safety. The full executor and ScopeHandler support
multi-item ticks using Item.Continuation. A must retain this framing path or add
an explicit per-item cursor to the shared/HTTP contract before using multi-item
shared pages for SSE. Never put a page-final cursor on its first frame.

CPU is O(S log N + SP + P log S + token serialization bytes), idle O(S log N).
Rows are <=S(P+1) metadata keys plus <=P hydrated payloads; serialized output is
<=1 MiB. Token construction currently serializes bounded S positions per emitted
item, so its CPU term is O(PS) and separately byte bounded. Required indexes are
the change range primary key, exact stream sequence/retention primary key,
membership/principal-generation primary keys, scope-domain primary key and exact
principal/scope/role-audience binding primary key. Writes use <=four destinations,
fixed indexed sequence/change operations and bounded JSON; no workspace scan.

## Remaining lead-owned gates

The durable shadow-schema tests cover 64,000 wrong-audience rows, 10,000 unavailable
scope slots, encrypted pending-head survival, reconnect mid-page, membership and
role-generation changes, retention, real wire-byte budgets and cross-scope
derivation rejection. They exercise the real executor/transport helper with test
selection; they do not exercise production route/auth registration or canonical
write hooks, migrations or durable lifecycle workers.

A must integrate production typed repositories/templates, bounded hydration,
schema/authority epochs, immutable retained change rows and local retention;
canonical atomic hooks; source-normalization/coverage metadata; derivation analyzer
and dispatcher isolation; the writer inventory proposal; route/contracts/auth and
#295's actual-query-plan/SCA-661 budgets. C's prepared Text/Payload constructors
are trusted hook operations, not declassification APIs for business code. The
factory/import and implicit-flow analyzer gates remain required before wiring.
No passing fixture authorizes enabling an incomplete phase.
