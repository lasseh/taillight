# One Store type; files align to consumer interfaces; domains earn types via state, not size

`postgres.Store` (92 exported methods) is a god-object only from the inside: 25 narrow consumer-side interfaces (`handler/store.go`, `analyzer.Store`, `scheduler.*Store`, …) already form the deep-module boundary, and no consumer sees more than ~12 methods. What was broken was implementation-side navigation — `analysis_store.go` at 1,177 LOC and `store.go` at 946 LOC backing multiple unrelated interface clusters.

The fix (commit `6608c07`) was pure file realignment within `package postgres`: ~13 domain files, each answering "which consumer interface does this back?", one type, zero signature changes, zero consumer diffs. Per-domain store types, per-domain packages, a DAO/repository layer, and a facade were all rejected — sibling types in one package hide nothing, and a facade re-exposes all 92 methods with extra ceremony.

Standing rule from the review challenge: **a store domain earns its own type when it acquires state or lifecycle, not before.** `AuthStore` is the precedent — it owns a background goroutine; no other domain does.

Reopen when a domain acquires state/lifecycle (then give it a type like `AuthStore`), or when a second database engine appears (then the interfaces, not the Store, are the seam).

See `.scratch/architecture-review/REPORT.md` §5.1.
