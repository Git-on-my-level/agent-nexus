// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import WorkspaceActivationAlert from "../../src/lib/components/WorkspaceActivationAlert.svelte";
import {
  WORKSPACE_NOT_FOUND_MESSAGE,
  WORKSPACE_UNREACHABLE_MESSAGE,
  WorkspaceNotFoundError,
  WorkspaceUnavailableError,
  describeActivationFailure,
} from "../../src/lib/workspaceFailure.js";

afterEach(() => cleanup());

/** What the shell renders for an activation failure (`+layout.svelte`). */
function renderFailure(error, onRetry = vi.fn()) {
  const { message, notFound } = describeActivationFailure(error);
  return render(WorkspaceActivationAlert, {
    message,
    notFound,
    pending: false,
    onRetry,
    homeHref: "/",
  });
}

describe("workspace activation failure state", () => {
  it("shows an outage as temporarily unreachable with a working Retry", async () => {
    const onRetry = vi.fn();
    const { getByRole } = renderFailure(
      new WorkspaceUnavailableError(new TypeError("Failed to fetch")),
      onRetry,
    );

    expect(getByRole("alert").textContent).toContain(
      WORKSPACE_UNREACHABLE_MESSAGE,
    );
    await fireEvent.click(getByRole("button", { name: "Retry" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("shows a missing workspace as not found, with the way back instead of Retry", () => {
    const { getByRole, queryByRole } = renderFailure(
      new WorkspaceNotFoundError(404),
    );

    expect(getByRole("alert").textContent).toContain(
      WORKSPACE_NOT_FOUND_MESSAGE,
    );
    expect(queryByRole("button", { name: "Retry" })).toBeNull();
    expect(
      getByRole("link", { name: "Go to your workspaces" }).getAttribute("href"),
    ).toBe("/");
  });

  it("keeps the generic copy and Retry for anything else", () => {
    const { getByRole } = renderFailure(new Error("boom"));

    expect(getByRole("alert").textContent).toContain(
      "Could not open this workspace",
    );
    expect(getByRole("button", { name: "Retry" })).toBeTruthy();
  });
});
