# Field values

From a retained HL7 message, select a field/repetition/component and choose
**Field values** in More message actions. Counts cover the entire capture under
its current filters, not the rendered page. The displayed canonical selector
names one exact segment/repetition/component position; it counts at most one
value per message occurrence. It never expands a selected repetition to all
repetitions.

Values begin hidden for each capture/query/field scope. Readable present values
share one bucket; no value labels, raw-value hashes or distinct-value fingerprint
is returned in hidden mode. **Show values for this scope** reads decoded text
through the same parser/character-set policy as the reader. Source bytes remain
unchanged. Empty, Null, Omitted, undecodable and undecided counts are separate.
A complete scan and complete value coverage are separate facts; unresolved query
membership, decoding or grouping bounds stay visible rather than becoming a
complete value distribution.

The source is verified and the existing grid/index query owner applies filters.
Every bucket has a random opaque handle backed by its exact occurrence IDs.
**Show messages** reads at most 200 occurrences per page and verifies the source
and current query membership again. Back preserves the original message/query/
intentional multi-selection and resets reveal. Scope changes fence late replies.
No count, source value, reveal consent or bucket handle is saved in private
sessions. The backend cache keeps only memberships for at most four snapshots;
expired handles ask for Recount.

Each count page contains at most 100 buckets. An individual revealed value is
bounded to 4096 bytes, and all unique grouping text to 8 MiB; crossing either bound
produces an explicit undecided bucket without a truncated prefix. Scope is bounded
by the verified case limit of 10,000 occurrences / 64 MiB. Stop counting cancels only
the local named read, including when it runs beside a live capture.
