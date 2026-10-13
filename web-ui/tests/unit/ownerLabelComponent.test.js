// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import OwnerLabel from "../../src/lib/components/OwnerLabel.svelte";
import AgentRosterRow from "../../src/lib/components/agents/AgentRosterRow.svelte";
import { buildOwnerDisplay } from "../../src/lib/ownerDisplay.js";

afterEach(() => {
  cleanup();
});

const ownerCell = buildOwnerDisplay({
  actors: [
    { id: "actor-maya", display_name: "Maya Chen (Studio producer)" },
    { id: "actor-priya", display_name: "Priya" },
  ],
  principals: [],
  agents: [
    {
      id: "agent-priya",
      actor_id: "actor-priya",
      host_id: "host-dev",
      handle: "priya.dev-host",
      display_name: "priya on dev-host",
    },
  ],
  actorsLoaded: true,
  agentsLoaded: true,
});

describe("OwnerLabel", () => {
  it("shows the name, with the role in the title and no agent marker", () => {
    const { getByText, queryByLabelText, container } = render(OwnerLabel, {
      cell: ownerCell("actor-maya"),
    });
    expect(getByText("Maya Chen")).toBeTruthy();
    expect(queryByLabelText("agent")).toBeNull();
    expect(container.querySelector("[title]")?.getAttribute("title")).toBe(
      "Maya Chen · Studio producer",
    );
  });

  it("marks an agent and keeps its host and handle in the title", () => {
    const { getByText, getByLabelText, container } = render(OwnerLabel, {
      cell: ownerCell("actor-priya"),
    });
    expect(getByText("priya")).toBeTruthy();
    expect(getByLabelText("agent")).toBeTruthy();
    expect(container.querySelector("[title]")?.getAttribute("title")).toBe(
      "priya · on dev-host · @priya.dev-host",
    );
  });

  it("holds a placeholder instead of a handle it would replace", () => {
    const pending = buildOwnerDisplay({
      actors: [],
      principals: [],
      agents: [],
      actorsLoaded: false,
      agentsLoaded: false,
    });
    const { container, getByText } = render(OwnerLabel, {
      cell: pending("actor-priya"),
    });
    expect(container.querySelector("[data-owner-pending]")).toBeTruthy();
    expect(container.textContent).not.toContain("actor-priya");
    expect(getByText("Loading owner")).toBeTruthy();
  });

  it("is an em dash when nothing owns the row", () => {
    const { container } = render(OwnerLabel, { cell: ownerCell("") });
    expect(container.textContent?.trim()).toBe("—");
  });
});

const ROSTER_MODEL = {
  state: "idle",
  headline: "Idle",
  ask: null,
  task: null,
  note: null,
  duration: "",
  durationTitle: "",
  run: null,
  moreAsks: 0,
};

describe("AgentRosterRow", () => {
  it("lists a host-derived agent once, handle on the title", () => {
    const { container, getByText } = render(AgentRosterRow, {
      agent: {
        id: "agent-priya",
        handle: "priya.dev-host",
        display_name: "priya on dev-host",
        bridge_online: false,
      },
      model: ROSTER_MODEL,
      href: "/agents/priya.dev-host",
    });
    expect(getByText("priya on dev-host")).toBeTruthy();
    expect(container.querySelector("[data-agent-handle]")).toBeNull();
    expect(
      container.querySelector("[data-agent-link]")?.getAttribute("title"),
    ).toBe("priya on dev-host · @priya.dev-host");
  });

  it("keeps the handle on its own line where it disambiguates", () => {
    const { container } = render(AgentRosterRow, {
      agent: {
        id: "agent-legacy",
        handle: "legacy.bot",
        display_name: "priya on dev-host",
        bridge_online: false,
      },
      model: ROSTER_MODEL,
      href: "/agents/legacy.bot",
    });
    expect(
      container.querySelector("[data-agent-handle]")?.textContent?.trim(),
    ).toBe("@legacy.bot");
  });
});
