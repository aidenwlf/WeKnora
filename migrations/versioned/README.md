# Versioned migrations

Applied in numeric order by golang-migrate; **the number is the identity**, not
the file name. Files here come from two sources:

- `000001`.. upstream's tail — Tencent/WeKnora `main`.
- The block directly after that tail — this fork's own migrations.

## Renumbering rule for fork migrations

Upstream keeps allocating the next integer, so a fork migration that already
sits in that range will eventually collide with a different upstream file at the
same number. When merging upstream:

1. Put every fork migration **after** upstream's highest number, keeping their
   relative order and leaving no gaps.
2. Keep fork migrations idempotent (`IF NOT EXISTS`, no-op `UPDATE`), so the
   reconciliation in step 3 is safe.
3. Reconcile the deployed database, whose `schema_migrations.version` still
   counts the old numbering: set it to the last pristine upstream version,
   start the app, and let migrations run forward. Idempotent fork migrations
   re-run as no-ops, and the upstream files that were skipped by the collision
   finally execute.

Skipping step 3 leaves upstream migrations that never ran, because the recorded
version already looks past them.
