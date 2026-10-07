Describe the user-visible problem and resulting behavior.

**Validation:** relevant checks, targeted tests, and adversarial reviewer verdict.

**HTTP routes:** list every new or changed method/path, or state none.

**Per-request cost:** for new or changed reads and stream ticks, state complexity,
enforced page/selector bounds, indexes used before filtering/limits, scale-gate
results, and any justified query-plan exception. Hot-path O(workspace) cost is a
merge-blocking P1 for new or changed paths. Existing main hazards need a finite,
exact-shape baseline with a justification linked to a P1 issue; new regressions
remain blocked.

**Startup/migrations:** expected readiness time, migration progress signals, and
how deployment probes distinguish an upgrade in progress from a failed process.

**Uncertainty:** material limits, risks, and unresolved dependencies.
