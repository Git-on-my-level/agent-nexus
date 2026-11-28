# Architectural decisions

## 2026-11-27 — Operating rulings in Decide (end-state §2.4)

Extend the workspace-local standing-ruling primitive with operating rulings.
A pending ruling row is the canonical source of its Decide item. Human
confirmation records the ruling and the supersession transitions atomically
with its replay receipt. This extends the existing decision loop with a
dedicated confirmation action and preserves ask-backed standing answers.

The CLI projects account-scope rulings from authorized workspace enrollments.
Each workspace owns its records. Fleet readers check for ID and alias
collisions across the combined projection. The local snapshot supports
offline lookup and maintenance checks, while core remains authoritative for
automatic ask answers.
