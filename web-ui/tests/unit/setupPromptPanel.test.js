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
 * The setup panel itself: token lifetime, what the Copy button carries, and
 * what it refuses to offer.
 *
 * Covered here rather than in Playwright because the behaviour turns on the
 * deployment's API address, and the browser suite runs against a loopback core
 * where the panel is deliberately withholding its prompt.
 */

const coreClientMock = vi.hoisted(() => ({
  createHostEnrollmentToken: vi.fn(),
  revokeHostEnrollmentToken: vi.fn(),
}));

/**
 * jsdom owns `navigator.clipboard`, so the copy path is observed where the
 * component hands text over rather than by replacing a host API.
 */
const clipboard = vi.hoisted(() => ({ text: "" }));

vi.mock("$lib/coreClient", () => ({ coreClient: coreClientMock }));
vi.mock("$lib/clipboard.js", () => ({
  copyText: vi.fn(async (value) => {
    clipboard.text = String(value ?? "");
    return true;
  }),
}));

const { default: SetupPrompt } =
  await import("../../src/lib/components/setup/SetupPrompt.svelte");

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

/** Resolves once a token has actually landed in the panel. */
async function tokenReady() {
  await screen.findByText(/token expires in/i);
}

beforeEach(() => {
  clipboard.text = "";
  coreClientMock.createHostEnrollmentToken.mockReset();
  coreClientMock.revokeHostEnrollmentToken.mockReset();
  coreClientMock.createHostEnrollmentToken.mockResolvedValue(
    tokenResponse("htok_first"),
  );
  coreClientMock.revokeHostEnrollmentToken.mockResolvedValue({});
});

afterEach(() => cleanup());

describe("a deployment agents can reach", () => {
  it("issues one short-lived token when the panel opens", async () => {
    render(SetupPrompt, {
      props: { kind: "machine", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
    });

    await waitFor(() =>
      expect(coreClientMock.createHostEnrollmentToken).toHaveBeenCalledTimes(1),
    );
    const payload = coreClientMock.createHostEnrollmentToken.mock.calls[0][0];
    expect(payload.label).toBe("Setup prompt");
    const lifetimeMs = Date.parse(payload.expires_at) - Date.now();
    // 30 minutes, with room for the time this test takes to get here.
    expect(lifetimeMs).toBeGreaterThan(29 * 60_000);
    expect(lifetimeMs).toBeLessThanOrEqual(30 * 60_000 + 5_000);

    await expect(screen.findByText(/token expires in/i)).resolves.toBeTruthy();
  });

  it("copies a prompt carrying that token", async () => {
    render(SetupPrompt, {
      props: { kind: "machine", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
    });
    await tokenReady();

    await fireEvent.click(
      screen.getByRole("button", { name: "Copy setup prompt" }),
    );

    await waitFor(() => expect(clipboard.text).toContain("htok_first"));
    expect(clipboard.text).toContain("host enroll --token-stdin");
    expect(clipboard.text).toContain(`--base-url ${REMOTE}`);
    expect(clipboard.text).toContain('"Ops"');
  });

  it("retires the old token when a new one is asked for", async () => {
    coreClientMock.createHostEnrollmentToken
      .mockResolvedValueOnce(tokenResponse("htok_first"))
      .mockResolvedValueOnce(tokenResponse("htok_second"));

    render(SetupPrompt, {
      props: { kind: "machine", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
    });
    await tokenReady();

    await fireEvent.click(screen.getByRole("button", { name: "New token" }));
    await waitFor(() =>
      expect(coreClientMock.revokeHostEnrollmentToken).toHaveBeenCalledWith(
        "htok_htok_first",
      ),
    );

    await fireEvent.click(
      screen.getByRole("button", { name: "Copy setup prompt" }),
    );
    await waitFor(() => expect(clipboard.text).toContain("htok_second"));
    expect(clipboard.text).not.toContain("htok_first");
  });

  it("keeps the commands when the reader may not issue tokens", async () => {
    coreClientMock.createHostEnrollmentToken.mockRejectedValue(
      Object.assign(new Error("forbidden"), { status: 403 }),
    );
    render(SetupPrompt, {
      props: { kind: "machine", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
    });

    // Lands on the path that works for them, with no error banner.
    await waitFor(() => {
      const command = document.querySelector("[data-host-enroll-command]");
      expect(command?.textContent).toContain(
        `--base-url ${REMOTE} host enroll`,
      );
    });
    await fireEvent.click(
      screen.getByRole("tab", { name: "Have your agent do it" }),
    );
    await expect(
      screen.findByText(/Only workspace administrators can hand out/i),
    ).resolves.toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Copy setup prompt" }),
    ).toBeNull();
  });

  it("says so when core refuses to issue a token", async () => {
    coreClientMock.createHostEnrollmentToken.mockRejectedValue(
      Object.assign(new Error("nope"), { details: "Not an administrator." }),
    );
    render(SetupPrompt, {
      props: { kind: "machine", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
    });
    await expect(
      screen.findByText("Not an administrator."),
    ).resolves.toBeTruthy();
  });

  it("bakes the chosen PM runner into the prompt", async () => {
    render(SetupPrompt, {
      props: {
        kind: "pm",
        cliBaseUrl: REMOTE,
        workspaceLabel: "Ops",
        runnerKey: "hermes",
      },
    });
    await tokenReady();

    await fireEvent.click(
      screen.getByRole("button", { name: "Copy setup prompt" }),
    );
    await waitFor(() => expect(clipboard.text).toContain("pm install"));
    expect(clipboard.text).toContain("hermes chat --query-file");
    // One-shot: it joins the machine to the workspace when it has to.
    expect(clipboard.text).toContain("host enroll --token-stdin");
    expect(clipboard.text).toContain("already enrolled");
  });

  it("switches the runner with the picker", async () => {
    render(SetupPrompt, {
      props: { kind: "pm", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
    });
    await tokenReady();

    await fireEvent.change(screen.getByRole("combobox"), {
      target: { value: "hermes" },
    });
    await fireEvent.click(
      screen.getByRole("button", { name: "Copy setup prompt" }),
    );
    await waitFor(() => expect(clipboard.text).toContain("hermes chat"));
  });
});

describe("a deployment agents cannot reach", () => {
  it("withholds the prompt, issues no token, and keeps the commands", async () => {
    render(SetupPrompt, {
      props: {
        kind: "machine",
        cliBaseUrl: "http://127.0.0.1:8000",
        workspaceLabel: "Ops",
      },
    });

    // The reader lands on the path that still works.
    await expect(
      screen.findByText(
        /anx --base-url http:\/\/127\.0\.0\.1:8000 host enroll/,
      ),
    ).resolves.toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Copy setup prompt" }),
    ).toBeNull();
    expect(coreClientMock.createHostEnrollmentToken).not.toHaveBeenCalled();

    // The explanation is one tab away, not hidden.
    await fireEvent.click(
      screen.getByRole("tab", { name: "Have your agent do it" }),
    );
    await expect(
      screen.findByText(/would send your agent to its own computer/i),
    ).resolves.toBeTruthy();
    expect(coreClientMock.createHostEnrollmentToken).not.toHaveBeenCalled();
  });

  it("says what is missing when the deployment has no API address", async () => {
    render(SetupPrompt, {
      props: { kind: "machine", cliBaseUrl: "", workspaceLabel: "Ops" },
    });
    await fireEvent.click(
      screen.getByRole("tab", { name: "Have your agent do it" }),
    );
    await expect(
      screen.findByText(/has not told the web app its API address/i),
    ).resolves.toBeTruthy();
    expect(coreClientMock.createHostEnrollmentToken).not.toHaveBeenCalled();
  });
});
