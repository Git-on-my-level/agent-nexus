/**
 * The agent harnesses a PM service can run under, and the argv each one needs.
 *
 * These strings are the same ones `anx pm install`'s interactive wizard offers
 * (`cli/internal/app/pm_wizard.go`). The CLI owns them; this is a copy, because
 * the web UI cannot read Go at runtime and the picker has to produce a runner
 * the CLI will accept without the reader answering a prompt they cannot see.
 *
 * `tests/unit/pmRunners.test.js` reads the wizard source and fails when the two
 * drift, so a change on either side is caught rather than shipped.
 */

/** `{prompt_file}` is substituted by the PM service with a private file path. */
export const PM_RUNNERS = Object.freeze([
  Object.freeze({
    key: "hermes",
    label: "Hermes",
    argv: "hermes chat --query-file {prompt_file} -Q",
  }),
  Object.freeze({
    key: "claude",
    label: "Claude Code",
    argv: `sh -c 'exec claude -p < "$1"' sh {prompt_file}`,
  }),
]);

export const DEFAULT_PM_RUNNER_KEY = "claude";

/**
 * The runner for a key, falling back to the default rather than to nothing:
 * a prompt with no runner in it is a prompt that drops the reader into the
 * wizard this picker exists to avoid.
 *
 * @param {string} key
 */
export function pmRunnerFor(key) {
  const wanted = String(key ?? "").trim();
  return (
    PM_RUNNERS.find((runner) => runner.key === wanted) ??
    PM_RUNNERS.find((runner) => runner.key === DEFAULT_PM_RUNNER_KEY) ??
    PM_RUNNERS[0]
  );
}
