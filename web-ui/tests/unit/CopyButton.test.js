// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import CopyButton from "../../src/lib/components/CopyButton.svelte";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("CopyButton", () => {
  it("copies the configured value for the compact link affordance", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    const { getByLabelText } = render(CopyButton, {
      value:
        "https://example.test/o/local/w/main/threads/thread-1#message-msg-1",
      iconOnly: true,
      icon: "link",
      label: "Copy message link",
    });

    await fireEvent.click(getByLabelText("Copy message link"));

    expect(writeText).toHaveBeenCalledWith(
      "https://example.test/o/local/w/main/threads/thread-1#message-msg-1",
    );
  });
});
