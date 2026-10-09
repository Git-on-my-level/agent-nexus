// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The walkthrough stays mounted on the shell for the session. Unused
 * enrollment tokens have to be handed back when it closes, not only when
 * the tab unloads.
 */

const pageStore = vi.hoisted(() => {
  let value = {
    url: new URL("http://localhost/o/acme/w/ops/overview"),
    params: { organization: "acme", workspace: "ops" },
  };
  const subscribers = new Set();
  return {
    subscribe(fn) {
      subscribers.add(fn);
      fn(value);
      return () => subscribers.delete(fn);
    },
    reset() {
      value = {
        url: new URL("http://localhost/o/acme/w/ops/overview"),
        params: { organization: "acme", workspace: "ops" },
      };
      for (const fn of subscribers) fn(value);
    },
  };
});

const coreClientMock = vi.hoisted(() => ({
  listPrincipals: vi.fn(),
  createHostEnrollmentToken: vi.fn(),
  revokeHostEnrollmentToken: vi.fn(),
}));

const clipboard = vi.hoisted(() => ({ text: "" }));

vi.mock("$app/environment", () => ({
  browser: true,
}));

vi.mock("$app/stores", () => ({
  page: {
    subscribe: pageStore.subscribe,
  },
}));

vi.mock("$lib/coreClient", () => ({ coreClient: coreClientMock }));
vi.mock("$lib/clipboard.js", () => ({
  copyText: vi.fn(async (value) => {
    clipboard.text = String(value ?? "");
    return true;
  }),
}));

const { default: WorkspaceTour } =
  await import("../../src/lib/components/onboarding/WorkspaceTour.svelte");

const REMOTE = "https://anx.example.test/o/acme/w/ops";

function tokenResponse(secret, minutes = 30) {
  return {
    token: secret,
    enrollment_token: {
      id: `htok_${secret}`,
      expires_at: new Date(Date.now() + minutes * 60_000).toISOString(),
    },
  };
}

async function openTour() {
  render(WorkspaceTour, {
    props: {
      organizationSlug: "acme",
      workspaceSlug: "ops",
      devActorModeReady: true,
      cliBaseUrl: REMOTE,
      workspaceLabel: "Ops",
    },
  });
  await screen.findByTestId("workspace-spotlight-tour");
}

async function reachLastStep() {
  await fireEvent.click(
    screen.getByRole("button", { name: "Take the tour →" }),
  );
  for (let i = 0; i < 6; i += 1) {
    await fireEvent.click(screen.getByRole("button", { name: "Next" }));
  }
  await waitFor(() =>
    expect(coreClientMock.createHostEnrollmentToken).toHaveBeenCalled(),
  );
  await screen.findByRole("button", { name: "Copy the setup prompt →" });
}

beforeEach(() => {
  clipboard.text = "";
  localStorage.clear();
  pageStore.reset();
  coreClientMock.listPrincipals.mockReset();
  coreClientMock.createHostEnrollmentToken.mockReset();
  coreClientMock.revokeHostEnrollmentToken.mockReset();
  coreClientMock.listPrincipals.mockResolvedValue({ principals: [] });
  coreClientMock.createHostEnrollmentToken.mockResolvedValue(
    tokenResponse("htok_tour"),
  );
  coreClientMock.revokeHostEnrollmentToken.mockResolvedValue({});
});

afterEach(() => cleanup());

describe("an unused tour token is retired when the walkthrough ends", () => {
  it("hands the token back on Skip, without waiting for unmount", async () => {
    await openTour();
    await reachLastStep();

    await fireEvent.click(screen.getByRole("button", { name: "Skip tour" }));
    await waitFor(() =>
      expect(coreClientMock.revokeHostEnrollmentToken).toHaveBeenCalledWith(
        "htok_htok_tour",
      ),
    );
  });

  it("hands the token back on Close", async () => {
    await openTour();
    await reachLastStep();

    await fireEvent.click(screen.getByRole("button", { name: "Close tour" }));
    await waitFor(() =>
      expect(coreClientMock.revokeHostEnrollmentToken).toHaveBeenCalledWith(
        "htok_htok_tour",
      ),
    );
  });

  it("keeps a token the reader has already copied", async () => {
    await openTour();
    await reachLastStep();

    await fireEvent.click(
      screen.getByRole("button", { name: "Copy the setup prompt →" }),
    );
    await waitFor(() => expect(clipboard.text).toContain("htok_tour"));

    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(coreClientMock.revokeHostEnrollmentToken).not.toHaveBeenCalled();
  });
});
