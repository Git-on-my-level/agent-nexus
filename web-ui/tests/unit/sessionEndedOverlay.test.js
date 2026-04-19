// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import SessionEndedOverlay from "../../src/lib/components/SessionEndedOverlay.svelte";

afterEach(cleanup);

describe("SessionEndedOverlay", () => {
  it("renders nothing when open is false", () => {
    const { container } = render(SessionEndedOverlay, {
      open: false,
      cpOrigin: "https://cp.example",
      workspaceUrl: "https://ws.example/ws",
    });
    expect(container.querySelector(".session-ended-layer")).toBeNull();
  });

  it("renders heading, body, and sign-in link when open", () => {
    const { getByRole, getByText } = render(SessionEndedOverlay, {
      open: true,
      cpOrigin: "https://cp.example",
      workspaceUrl: "https://ws.example/acme",
    });
    expect(getByRole("heading", { name: /your session ended/i })).toBeTruthy();
    expect(getByText(/you've been signed out/i, { exact: false })).toBeTruthy();
    const link = getByRole("button", { name: /sign in again/i });
    expect(link.getAttribute("href")).toContain("https://cp.example");
    expect(link.getAttribute("href")).toContain("login");
    expect(link.getAttribute("href")).toContain("return_url=");
    expect(link.getAttribute("href")).toContain(
      encodeURIComponent("https://ws.example/acme"),
    );
  });

  it("does not remove the overlay on Escape", async () => {
    const { getByRole } = render(SessionEndedOverlay, {
      open: true,
      cpOrigin: "https://cp.example",
      workspaceUrl: "https://ws.example/ws",
    });
    expect(getByRole("heading", { name: /your session ended/i })).toBeTruthy();
    fireEvent.keyDown(document, { key: "Escape", bubbles: true });
    expect(getByRole("heading", { name: /your session ended/i })).toBeTruthy();
  });

  it("does not close on backdrop click", async () => {
    const { container, getByRole } = render(SessionEndedOverlay, {
      open: true,
      cpOrigin: "https://cp.example",
      workspaceUrl: "https://ws.example/ws",
    });
    const backdrop = container.querySelector(".session-ended-backdrop");
    expect(backdrop).toBeTruthy();
    fireEvent.click(backdrop, { bubbles: true });
    expect(getByRole("heading", { name: /your session ended/i })).toBeTruthy();
  });
});
