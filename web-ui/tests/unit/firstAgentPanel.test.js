// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

/**
 * The cold-start Overview panel.
 *
 * Covered here rather than in Playwright for the same reason the setup panel
 * is: what it can offer turns on the deployment's API address, and the browser
 * suite runs against a loopback core where the prompt is deliberately withheld.
 */

const coreClientMock = vi.hoisted(() => ({
  createHostEnrollmentToken: vi.fn(),
  revokeHostEnrollmentToken: vi.fn(),
}));

const clipboard = vi.hoisted(() => ({ text: "" }));

vi.mock("$lib/coreClient", () => ({ coreClient: coreClientMock }));
vi.mock("$lib/clipboard.js", () => ({
  copyText: vi.fn(async (value) => {
    clipboard.text = String(value ?? "");
    return true;
  }),
}));

const { default: FirstAgentPanel } =
  await import("../../src/lib/components/onboarding/FirstAgentPanel.svelte");

const REMOTE = "https://anx.example.test/o/acme/w/ops";

beforeEach(() => {
  clipboard.text = "";
  coreClientMock.createHostEnrollmentToken.mockReset();
  coreClientMock.revokeHostEnrollmentToken.mockReset();
  coreClientMock.createHostEnrollmentToken.mockResolvedValue({
    token: "htok_secret",
    enrollment_token: {
      id: "htok_1",
      expires_at: new Date(Date.now() + 30 * 60_000).toISOString(),
    },
  });
  coreClientMock.revokeHostEnrollmentToken.mockResolvedValue({});
});

afterEach(() => cleanup());

it("leads with the one move that fills every other surface", async () => {
  render(FirstAgentPanel, {
    props: { cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
  });

  expect(
    screen.getByRole("heading", { name: /connect your first agent/i }),
  ).toBeTruthy();
  // The reader has to know which of their tools counts as "your agent".
  expect(screen.getByText(/Claude Code, Codex or Cursor/)).toBeTruthy();
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: /copy setup prompt/i }),
    ).toBeTruthy(),
  );
});

it("says it is waiting, and says the page will notice on its own", () => {
  const { container } = render(FirstAgentPanel, {
    props: { cliBaseUrl: REMOTE },
  });

  const waiting = container.querySelector("[data-first-agent-waiting]");
  expect(waiting?.textContent).toMatch(/waiting for your first agent/i);
  expect(waiting?.textContent).toMatch(/notices it on its own/i);
});

it("shows what an ask looks like without filing one", () => {
  const { container } = render(FirstAgentPanel, {
    props: { cliBaseUrl: REMOTE },
  });

  const example = container.querySelector("[data-first-agent-example]");
  expect(example?.textContent).toMatch(/^Example/);
  // A workspace's first real Inbox item should be the reader's own: the
  // panel must not write anything to get its point across.
  expect(coreClientMock.createHostEnrollmentToken).not.toHaveBeenCalledWith(
    expect.objectContaining({ seed: expect.anything() }),
  );
});

it("flips to the arrival, and stops asking for a machine", () => {
  const { container } = render(FirstAgentPanel, {
    props: { connected: true, cliBaseUrl: REMOTE },
  });

  expect(
    container.querySelector("[data-first-agent-connected]")?.textContent,
  ).toMatch(/connected/i);
  expect(container.querySelector("[data-first-agent-waiting]")).toBeNull();
  expect(container.querySelector("[data-setup-prompt]")).toBeNull();
  // No token is minted for a workspace that no longer needs one.
  expect(coreClientMock.createHostEnrollmentToken).not.toHaveBeenCalled();
});

it("names the agent that arrived when one is known", () => {
  const { container } = render(FirstAgentPanel, {
    props: { connected: true, connectedLabel: "claude" },
  });

  expect(
    container.querySelector("[data-first-agent-connected]")?.textContent,
  ).toMatch(/claude is connected/i);
});
