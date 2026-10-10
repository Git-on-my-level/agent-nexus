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
    /*
     * The lifetime is measured by core, not by this browser: a clock a few
     * minutes slow makes core reject an absolute `expires_at` outright, and a
     * clock a day fast would buy a 23-hour credential behind a countdown that
     * claimed half an hour.
     */
    expect(payload.expires_in_seconds).toBe(1800);
    expect(payload.expires_at).toBeUndefined();

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
    expect(clipboard.text).toContain(`--base-url '${REMOTE}'`);
    // The label is a delimited data field, never instruction prose.
    expect(clipboard.text).toContain("«Ops»");
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
        `--base-url '${REMOTE}' host enroll`,
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

  it("offers nothing to copy until there is something to copy", async () => {
    /** @type {(value: unknown) => void} */
    let release = () => {};
    coreClientMock.createHostEnrollmentToken.mockImplementation(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    render(SetupPrompt, {
      props: { kind: "machine", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
    });

    // A copy control that hands over "" and then says "copied" is worse than
    // one that is not there yet.
    await expect(
      screen.findByRole("button", { name: /Preparing the prompt/i }),
    ).resolves.toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Copy setup prompt" }),
    ).toBeNull();

    release(tokenResponse("htok_late"));
    await tokenReady();
    await fireEvent.click(
      screen.getByRole("button", { name: "Copy setup prompt" }),
    );
    await waitFor(() => expect(clipboard.text).toContain("htok_late"));
  });

  it("hands back a token nobody ever received", async () => {
    const view = render(SetupPrompt, {
      props: { kind: "machine", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
    });
    await tokenReady();

    view.unmount();
    await waitFor(() =>
      expect(coreClientMock.revokeHostEnrollmentToken).toHaveBeenCalledWith(
        "htok_htok_first",
      ),
    );
  });

  it("keeps a token the reader has already copied", async () => {
    /*
     * The token's whole purpose is to leave the browser. This panel unmounts
     * on an ordinary navigation — and on one failed hosts read, which hides
     * the section for a poll tick — so revoking here would kill the paste the
     * reader is in the middle of.
     */
    const view = render(SetupPrompt, {
      props: { kind: "machine", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
    });
    await tokenReady();
    await fireEvent.click(
      screen.getByRole("button", { name: "Copy setup prompt" }),
    );
    await waitFor(() => expect(clipboard.text).toContain("htok_first"));

    view.unmount();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(coreClientMock.revokeHostEnrollmentToken).not.toHaveBeenCalled();
  });

  it("hands back a token core made but would not show", async () => {
    coreClientMock.createHostEnrollmentToken.mockResolvedValue({
      enrollment_token: {
        id: "htok_orphan",
        expires_at: "2026-01-01T00:00:00Z",
      },
    });
    render(SetupPrompt, {
      props: { kind: "machine", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
    });
    await waitFor(() =>
      expect(coreClientMock.revokeHostEnrollmentToken).toHaveBeenCalledWith(
        "htok_orphan",
      ),
    );
  });

  it("does not call a token expired when core reported no expiry", async () => {
    coreClientMock.createHostEnrollmentToken.mockResolvedValue({
      token: "htok_no_expiry",
      enrollment_token: { id: "htok_x" },
    });
    render(SetupPrompt, {
      props: { kind: "machine", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
    });

    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Copy setup prompt" }),
      ).toBeTruthy(),
    );
    // An older core that reports no expiry is not reporting an expired token.
    expect(screen.queryByText(/That token has expired/i)).toBeNull();
    await fireEvent.click(
      screen.getByRole("button", { name: "Copy setup prompt" }),
    );
    await waitFor(() => expect(clipboard.text).toContain("htok_no_expiry"));
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
    // And no control that would copy an empty string while saying "copied".
    expect(
      screen.queryByRole("button", { name: "Copy setup prompt" }),
    ).toBeNull();
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
    expect(clipboard.text).toContain("--as 'pm' pm install");
    expect(clipboard.text).toContain("--as 'pm' --json doctor");
    expect(clipboard.text).not.toContain("--as 'hermes'");
    // The machine about to run a PM needs the PM skill, not just participant.
    expect(clipboard.text).toContain("anx skills sync --pm");
    // One-shot: it joins the machine to the workspace when it has to.
    expect(clipboard.text).toContain("host enroll --token-stdin");
    expect(clipboard.text).toContain("the token unspent");
  });

  it("revokes a copied PM setup token when the PM connects", async () => {
    const props = {
      kind: "pm",
      completed: false,
      cliBaseUrl: REMOTE,
      workspaceLabel: "Ops",
      runnerKey: "hermes",
    };
    const view = render(SetupPrompt, { props });
    await tokenReady();
    await fireEvent.click(
      screen.getByRole("button", { name: "Copy setup prompt" }),
    );
    await waitFor(() => expect(clipboard.text).toContain("htok_first"));

    coreClientMock.revokeHostEnrollmentToken.mockResolvedValue({
      enrollment_token: {
        consumed_at: null,
        revoked_at: new Date().toISOString(),
      },
    });
    await view.rerender({ ...props, completed: true });
    await waitFor(() =>
      expect(coreClientMock.revokeHostEnrollmentToken).toHaveBeenCalledWith(
        "htok_htok_first",
      ),
    );
    await expect(
      screen.findByText("PM connected. The unused setup token was revoked."),
    ).resolves.toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Copy setup prompt" }),
    ).toBeNull();
  });

  it("does not call a consumed PM setup token unused or revoked", async () => {
    const props = {
      kind: "pm",
      completed: false,
      cliBaseUrl: REMOTE,
      runnerKey: "hermes",
    };
    const view = render(SetupPrompt, { props });
    await tokenReady();
    coreClientMock.revokeHostEnrollmentToken.mockRejectedValue(
      new Error("enrollment token already consumed"),
    );

    await view.rerender({ ...props, completed: true });
    await expect(
      screen.findByText(
        "PM connected. The setup token may already have been used; check Access → Hosts for its status.",
      ),
    ).resolves.toBeTruthy();
    expect(
      screen.queryByText("PM connected. The unused setup token was revoked."),
    ).toBeNull();
  });

  it("does not issue a setup token when the PM is already connected", async () => {
    render(SetupPrompt, {
      props: { kind: "pm", completed: true, cliBaseUrl: REMOTE },
    });
    await expect(
      screen.findByText("PM connected. No setup token is needed."),
    ).resolves.toBeTruthy();
    expect(coreClientMock.createHostEnrollmentToken).not.toHaveBeenCalled();
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

describe("an expired token leaves the page", () => {
  it("removes Copy, the secret and the prompt, and offers a new one", async () => {
    vi.useFakeTimers();
    try {
      coreClientMock.createHostEnrollmentToken.mockResolvedValue(
        tokenResponse("htok_short", 1),
      );
      render(SetupPrompt, {
        props: { kind: "machine", cliBaseUrl: REMOTE, workspaceLabel: "Ops" },
      });
      await vi.waitFor(() =>
        expect(coreClientMock.createHostEnrollmentToken).toHaveBeenCalled(),
      );

      await vi.advanceTimersByTimeAsync(70_000);
      expect(
        screen.queryByRole("button", { name: "Copy setup prompt" }),
      ).toBeNull();
      expect(document.body.textContent).not.toContain("htok_short");
      expect(document.body.textContent).toContain("no longer on this page");
      expect(screen.getByRole("button", { name: "New token" })).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
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
        /anx --base-url 'http:\/\/127\.0\.0\.1:8000' host enroll/,
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
