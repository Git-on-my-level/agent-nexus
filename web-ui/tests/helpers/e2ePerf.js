/**
 * Wall-clock budgets (p95, "paints in 3s", millisecond caps) are not part of
 * the gating browser suite. Set `ANX_E2E_PERF=1` in the advisory performance
 * job to enforce them. The gating suite keeps the structural assertion.
 */
export const E2E_PERF = process.env.ANX_E2E_PERF === "1";

/** @param {() => void} assertion */
export function expectPerf(assertion) {
  if (E2E_PERF) assertion();
}
