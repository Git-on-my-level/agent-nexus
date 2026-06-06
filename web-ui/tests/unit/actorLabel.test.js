// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import ActorLabel from "../../src/lib/components/ActorLabel.svelte";

afterEach(() => {
  cleanup();
});

describe("ActorLabel", () => {
  it("renders avatar initials and display name", () => {
    const { getByText } = render(ActorLabel, {
      label: "Local Dev",
      seed: "actor-1",
      size: "sm",
    });
    expect(getByText("LD")).toBeTruthy();
    expect(getByText("Local Dev")).toBeTruthy();
  });

  it("renders optional prefix and timestamp", () => {
    const { getByText } = render(ActorLabel, {
      label: "Local Dev",
      seed: "actor-1",
      prefix: "by",
      timestamp: "2026-03-03T10:00:00.000Z",
    });
    expect(getByText("by")).toBeTruthy();
  });
});
