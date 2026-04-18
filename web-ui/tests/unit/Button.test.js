// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import Button from "../../src/lib/components/Button.svelte";

afterEach(cleanup);

function getButton(container) {
  return container.querySelector("button");
}

function getAnchor(container) {
  return container.querySelector("a[role='button']");
}

describe("Button", () => {
  it("renders as <button> by default", () => {
    const { container } = render(Button, { children: () => "Click me" });
    const btn = getButton(container);
    expect(btn).toBeTruthy();
  });

  it("renders as <a> when href is provided", () => {
    const { container } = render(Button, {
      href: "/test",
      children: () => "Link",
    });
    const a = getAnchor(container);
    expect(a).toBeTruthy();
    expect(a.getAttribute("href")).toBe("/test");
  });

  it("defaults to type button", () => {
    const { container } = render(Button, { children: () => "Default" });
    const btn = getButton(container);
    expect(btn.getAttribute("type")).toBe("button");
  });

  it("supports type submit", () => {
    const { container } = render(Button, {
      type: "submit",
      children: () => "Submit",
    });
    const btn = getButton(container);
    expect(btn.getAttribute("type")).toBe("submit");
  });

  describe("variants", () => {
    it("applies primary classes", () => {
      const { container } = render(Button, {
        variant: "primary",
        children: () => "Primary",
      });
      const btn = getButton(container);
      expect(btn.className).toContain("bg-accent-solid");
      expect(btn.className).toContain("text-white");
      expect(btn.className).toContain("hover:bg-accent");
    });

    it("applies secondary classes", () => {
      const { container } = render(Button, {
        variant: "secondary",
        children: () => "Secondary",
      });
      const btn = getButton(container);
      expect(btn.className).toContain("border-line");
      expect(btn.className).toContain("text-fg");
    });

    it("applies ghost classes", () => {
      const { container } = render(Button, {
        variant: "ghost",
        children: () => "Ghost",
      });
      const btn = getButton(container);
      expect(btn.className).toContain("text-fg-muted");
      expect(btn.className).toContain("hover:bg-panel-hover");
    });

    it("applies destructive classes", () => {
      const { container } = render(Button, {
        variant: "destructive",
        children: () => "Delete",
      });
      const btn = getButton(container);
      expect(btn.className).toContain("text-danger-text");
      expect(btn.className).toContain("hover:bg-danger-soft");
    });
  });

  describe("sizes", () => {
    it("applies default size classes (h-8)", () => {
      const { container } = render(Button, { children: () => "Default" });
      const btn = getButton(container);
      expect(btn.className).toContain("h-8");
    });

    it("applies compact size classes (h-7)", () => {
      const { container } = render(Button, {
        size: "compact",
        children: () => "Compact",
      });
      const btn = getButton(container);
      expect(btn.className).toContain("h-7");
    });

    it("applies large size classes (h-10)", () => {
      const { container } = render(Button, {
        size: "large",
        children: () => "Large",
      });
      const btn = getButton(container);
      expect(btn.className).toContain("h-10");
    });
  });

  it("disables the button when disabled prop is true", () => {
    const { container } = render(Button, {
      disabled: true,
      children: () => "Disabled",
    });
    const btn = getButton(container);
    expect(btn.disabled).toBe(true);
  });

  it("disables the button when busy prop is true", () => {
    const { container } = render(Button, {
      busy: true,
      children: () => "Busy",
    });
    const btn = getButton(container);
    expect(btn.disabled).toBe(true);
  });

  it("sets aria-busy when busy", () => {
    const { container } = render(Button, {
      busy: true,
      children: () => "Working",
    });
    const btn = getButton(container);
    expect(btn.getAttribute("aria-busy")).toBe("true");
  });

  it("does not set aria-busy when not busy", () => {
    const { container } = render(Button, { children: () => "Idle" });
    const btn = getButton(container);
    expect(btn.getAttribute("aria-busy")).toBeNull();
  });

  it("shows spinner SVG when busy", () => {
    const { container } = render(Button, {
      busy: true,
      children: () => "Loading",
    });
    const spinner = container.querySelector("svg.animate-spin");
    expect(spinner).toBeTruthy();
  });

  it("does not show spinner SVG when not busy", () => {
    const { container } = render(Button, { children: () => "Normal" });
    const spinner = container.querySelector("svg.animate-spin");
    expect(spinner).toBeNull();
  });

  it("relies on global focus-visible ring from CSS base layer", () => {
    const { container } = render(Button, { children: () => "Focus" });
    const btn = getButton(container);
    expect(btn.className).not.toContain("focus-visible:ring-2");
    expect(btn.tagName).toBe("BUTTON");
  });

  it("applies disabled opacity class", () => {
    const { container } = render(Button, { children: () => "Opacity" });
    const btn = getButton(container);
    expect(btn.className).toContain("disabled:opacity-50");
  });

  it("applies rounded class", () => {
    const { container } = render(Button, { children: () => "Rounded" });
    const btn = getButton(container);
    expect(btn.className).toContain("rounded");
  });

  it("applies cursor-pointer class", () => {
    const { container } = render(Button, { children: () => "Cursor" });
    const btn = getButton(container);
    expect(btn.className).toContain("cursor-pointer");
  });

  it("merges additional class names", () => {
    const { container } = render(Button, {
      class: "extra-class",
      children: () => "Extra",
    });
    const btn = getButton(container);
    expect(btn.className).toContain("extra-class");
  });

  it("renders <a> without disabled attribute even when disabled=true", () => {
    const { container } = render(Button, {
      href: "/link",
      disabled: true,
      children: () => "Link",
    });
    const a = getAnchor(container);
    expect(a).toBeTruthy();
    expect(a.hasAttribute("disabled")).toBe(false);
  });
});
