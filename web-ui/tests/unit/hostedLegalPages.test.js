// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import HostedTermsPage from "../../src/routes/hosted/legal/terms/+page.svelte";
import HostedPrivacyPage from "../../src/routes/hosted/legal/privacy/+page.svelte";

describe("hosted legal pages", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders beta terms with stable headings", () => {
    const { container } = render(HostedTermsPage);
    expect(container.textContent).toMatch(/public beta terms/i);
    expect(container.textContent).toMatch(/Beta status/);
  });

  it("renders privacy with deletion support path", () => {
    const { container } = render(HostedPrivacyPage);
    expect(container.textContent).toMatch(/public beta privacy/i);
    expect(container.textContent).toMatch(/Data deletion request/);
  });
});
