/**
 * Mock entries for the session failure policy in `$lib/server/authSession`,
 * for proxy tests that mock that module. The classifier and the 503 response
 * are the real ones; `settleWorkspaceSessionFailure` follows the real rule but
 * routes the race check and the clear through the test's own spies, so the
 * proxy wiring is tested against the actual decision.
 *
 * @param {Record<string, any>} actual `await importOriginal()` of authSession
 * @param {Record<string, import('vitest').Mock>} mocks the test's spies
 */
export function sessionFailurePolicyMocks(actual, mocks) {
  return {
    classifyWorkspaceSessionFailure: actual.classifyWorkspaceSessionFailure,
    WorkspaceSessionFailure: actual.WorkspaceSessionFailure,
    workspaceSessionUnavailableResponse:
      actual.workspaceSessionUnavailableResponse,
    settleWorkspaceSessionFailure: (event, org, ws, error, options) => {
      const { REJECTED, UNAVAILABLE } = actual.WorkspaceSessionFailure;
      if (actual.classifyWorkspaceSessionFailure(error) === UNAVAILABLE)
        return UNAVAILABLE;
      if (
        mocks.isRetryableWorkspaceRefreshFailure(error, options) &&
        !mocks.shouldClearWorkspaceAuthSessionAfterRetryableFailure(
          event,
          org,
          ws,
        )
      )
        return UNAVAILABLE;
      mocks.clearWorkspaceAuthSession(event, org, ws);
      return REJECTED;
    },
  };
}
