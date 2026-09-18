---
status: accepted
date: 2026-09-18
---

# The case index is a derived, disposable readmit-owned file, not a database

Finding a field in a large case needs an index. readmit builds one as a single
versioned strict-JSON `readmit-index/v1` file beside the case bundle, written
and read by `internal/index` with no storage engine underneath it. The index is
**derived and disposable**: it is a pure function of the canonical case
directory and the retention declarations an operator made, it can be deleted at
any instant, and building it again from the same evidence reproduces it exactly.

Nothing is stored only in an index. Every query opens the case it names through
the shared reader and refuses the moment the two disagree, so an index can never
become a second, quieter copy of the record.

## What this decision does not overturn

[ADR-0002](0002-case-bundles-are-directories-not-a-database.md) settled that
evidence is a versioned directory of raw payload files. This decision stays
inside it. The canonical artifacts are still the files; the index is a second
reading of them and is never the only home of anything. A corrupt, truncated,
expired, stale or deleted index is a rebuild — never data loss, and never a
reason to refuse access to evidence. `readmit timeline` reads a case with a
damaged index sitting next to it, and `internal/artifactpath` refuses an index
destination inside retained case, run, result, review or report evidence, so an
index cannot be written into the case it describes.

Roadmap #25 asks for "a rebuildable SQLite catalog/index and durable task
journal". *Rebuildable* is the load-bearing word and it is honoured here in full.
The storage engine is the part this decision answers differently, and the
argument is below. The durable task journal is a different artifact with
different requirements and is not decided here.

## Considered options

- **`mattn/go-sqlite3`.** Excluded outright.
  [ADR-0001](0001-go-single-binary-release-matrix.md) releases one static
  `CGO_ENABLED=0` executable for five targets, and this driver needs cgo. Its
  own consequence list already says so: "No cgo means pure-Go TLS and no native
  SQLite or similar."
- **`modernc.org/sqlite`, the pure-Go translation.** The serious alternative: it
  builds with `CGO_ENABLED=0` and would cross-compile for all five targets.
  Rejected on proportion, not on principle. It would become the second direct
  third-party dependency of the released executable beside Cobra, and by a wide
  margin the largest — a machine-translated C runtime whose failure modes are
  not the ones a Go reviewer reads for, carrying its own vulnerability surface
  into a binary whose whole promise is that it runs unattended inside a hospital
  network. What it would buy is indexing that is already bounded by the case
  bundle's own limits: at most 128 sources, at most 10,000 occurrences, at most
  64 MiB of evidence, one case per index, one writer, no concurrency and no
  cross-case query. A B-tree over a bounded, disposable, single-writer,
  in-memory-sized collection is not what SQLite is for. Revisit when the shape
  changes, not when the file gets slightly larger: a persistent catalogue
  spanning many bundles, concurrent writers, or incremental update of an index
  that is too large to rebuild would each justify reopening this.
- **A cgo SQLite confined to the `desktop` module.** Rejected because it would
  put the index behind the facade rather than in front of it. The desktop shell
  is [ADR-0005](0005-desktop-shell-is-a-separate-module-over-a-typed-go-facade.md)
  a view over the same engine, never a reimplementation of it; an index only the
  desktop could build would mean two answers to "which occurrences carry this
  identifier", and the command line would have the worse one.
- **A readmit-owned binary index format.** Rejected as unnecessary. The document
  is bounded and read whole; a binary layout would buy a smaller file and cost a
  second parsing surface, its own fuzzing, and a format nobody can read with the
  tools they already have. Strict JSON is what every other readmit artifact is.
- **Storing the index inside the case bundle.** Rejected. It would change the
  bundle identity every time someone searched, make evidence depend on a derived
  file, and require a new `readmit-case` contract version to hold it. Evidence
  does not acquire members because a reader wanted to be faster.
- **Trusting the index without re-reading the case.** Rejected. It is what makes
  a stale index serve answers about evidence that is no longer there. The cost
  is that one command-line query is not faster than a scan; what an index buys
  is that *many* queries cost one verification rather than one each.

## Consequences

- **Cobra remains the only direct third-party dependency of the released
  executable.** `go.mod` gains nothing, the five `CGO_ENABLED=0` targets build
  unchanged, and `tools/smoke.py` still runs the archived binaries with an empty
  PATH.
- **An index is bounded and refuses rather than truncating.** At most 16 declared
  fields, at most 128 retained bytes of any one value, at most 16 MiB for the
  whole document. Past a bound the build is refused and the operator declares
  fewer fields or a cheaper retention form. A value past the byte bound keeps its
  prefix and is marked as shortened, and a query it cannot settle is reported as
  undecided rather than as a miss.
- **A retained decoded field is patient data.** Roadmap #25: sensitive indexes
  are treated as sensitive data, not harmless metadata. What is retained, in what
  form, and until when are three declarations with no defaults. `states` retains
  nothing read out of a message; `digests` retains no value bytes but is **not**
  de-identification, because a short value from a small set is recovered by
  hashing that set, and the documentation says so rather than implying otherwise.
  Encryption of an index at rest is not decided here.
- **Corruption is detected, not authenticated.** The document carries a digest
  over its own contents, so damage is refused instead of answered. Anyone who can
  rewrite the file can recompute the digest; as
  [ADR-0004](0004-derived-evidence-and-generated-export.md) says of evidence, a
  hash is not source authentication. That is why the seal is not what an index is
  believed on: everything it restates about a case is compared against the
  verified bundle, and only the decoded field values — the part an index exists
  to avoid reproducing — are read from the file alone.
- **Retention is enforced by the engine, not by the command around it.** Past the
  end an index declares, the search itself refuses, so a facade calling the
  package directly is refused exactly as the command line is.
- **No existing artifact contract changes.** `readmit-case/v1`-`v4`,
  `readmit-project/v1`, `readmit-revisions/v1`, `readmit-collection/v1`-`v2`,
  `readmit-receiver-policy/v1`-`v2`, `readmit-run/v1`, `readmit-result/v1`,
  `readmit-test/v1`-`v2`, `readmit-observation/v1`, `readmit-report/v1` and the
  `readmit-import-*/v1` documents gain no member and change no byte. An index is
  not evidence and is never written inside any of them.
- **`docs/stack.md` no longer implies that indexing is absent.** A database, an
  ORM, a hosted backend, a message broker and a search server all remain absent.
  An index here is a file the engine writes and reads, the way it writes and
  reads every other artifact.
