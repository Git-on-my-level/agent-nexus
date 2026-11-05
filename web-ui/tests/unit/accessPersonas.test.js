// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  accessPersonaSummary,
  accessSummaryLine,
  groupHostPersonas,
  idlePersonaRangeLabel,
  isPersonaActive,
} from "../../src/lib/accessPersonas.js";
vi.mock("$lib/hostBridgeStatus.js", async (importOriginal) => ({
  ...(await importOriginal()),
  observeHostBridge: () => () => {},
}));

import HostRoster from "../../src/lib/components/access/HostRoster.svelte";

const NOW = Date.parse("2026-11-05T12:00:00Z");
const DAY = 24 * 60 * 60 * 1000;
const ago = (ms) => new Date(NOW - ms).toISOString();

function agent(name, extra = {}) {
  return {
    id: `agent-${name}`,
    name,
    handle: `${name}.studio`,
    identity_kind: "derived",
    state: "",
    last_used_at: null,
    last_progress_at: null,
    active_run: null,
    revoked_at: null,
    archived_at: null,
    ...extra,
  };
}

function host(agents) {
  return {
    id: "host-1",
    slug: "studio",
    display_name: "studio",
    os_user: "david",
    hostname: "studio",
    created_at: ago(30 * DAY),
    key_id: "hkey-1",
    discovered_adapters: ["codex"],
    excluded_names: [],
    revoked_at: null,
    agents,
  };
}

const roster = [
  agent("codex"), // adapter: always shown even though never used
  agent("claude", { last_used_at: ago(2 * DAY) }),
  agent("config-judge", { last_used_at: ago(12 * 60 * 1000) }),
  agent("codex-spkfinal", { last_used_at: ago(9 * DAY) }),
  agent("codex-spkfinal-r2", { last_used_at: ago(20 * DAY) }),
  agent("never-used"),
  agent("old-run", {
    archived_at: ago(3 * DAY),
    archived_reason: "manual",
    last_used_at: ago(40 * DAY),
  }),
];

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("persona activity", () => {
  it("is per persona and never inferred from the host bridge", () => {
    // bridge_online and last_signal_at describe the host, not this persona.
    const bridged = agent("quiet", {
      bridge_online: true,
      last_signal_at: ago(60 * 1000),
    });
    expect(isPersonaActive(bridged, NOW)).toBe(false);
    expect(
      isPersonaActive(agent("x", { last_used_at: ago(6 * DAY) }), NOW),
    ).toBe(true);
    expect(
      isPersonaActive(agent("x", { last_used_at: ago(8 * DAY) }), NOW),
    ).toBe(false);
    expect(isPersonaActive(agent("x", { state: "working" }), NOW)).toBe(true);
  });

  it("shows adapters and active personas, folds idle, separates archived", () => {
    const groups = groupHostPersonas(host(roster), roster, NOW);
    expect(groups.shown.map((a) => a.name).sort()).toEqual([
      "claude",
      "codex",
      "config-judge",
    ]);
    expect(groups.idle.map((a) => a.name)).toEqual([
      "codex-spkfinal",
      "codex-spkfinal-r2",
      "never-used",
    ]);
    expect(groups.archived.map((a) => a.name)).toEqual(["old-run"]);
    expect(idlePersonaRangeLabel(groups.idle.slice(0, 2), NOW)).toBe(
      "Last used 9d to 20d ago.",
    );
  });

  it("summarises hosts, personas and the active count", () => {
    const summary = accessPersonaSummary([host(roster)], NOW);
    expect(summary).toMatchObject({
      hosts: 1,
      personas: 6,
      active: 2,
      idle: 3,
      archived: 1,
    });
    expect(accessSummaryLine(summary)).toBe(
      "1 host · 6 personas · 2 active in the last 7 days",
    );
  });
});

describe("HostRoster", () => {
  it("collapses idle personas behind a per-host Show control", async () => {
    const { container, getByText } = render(HostRoster, {
      hosts: [host(roster)],
      now: NOW,
    });
    const shown = () =>
      [...container.querySelectorAll("[data-host-agent]")].map((row) =>
        row.getAttribute("data-host-agent"),
      );
    expect(shown()).not.toContain("never-used.studio");
    expect(
      container
        .querySelector("[data-host-idle-personas]")
        ?.textContent.replace(/\s+/g, " "),
    ).toContain("3 idle personas hidden");
    await fireEvent.click(getByText("Show 3"));
    expect(shown()).toContain("never-used.studio");
    expect(shown()).not.toContain("old-run.studio");
  });

  it("Show idle expands every host; Archived lists archived personas", async () => {
    const { container, getByText } = render(HostRoster, {
      hosts: [host(roster)],
      now: NOW,
    });
    await fireEvent.click(getByText("Show idle (3)"));
    expect(container.querySelector("[data-host-idle-personas]")).toBeNull();
    expect(
      container.querySelector('[data-host-agent="codex-spkfinal-r2.studio"]'),
    ).not.toBeNull();
    await fireEvent.click(getByText("Archived (1)"));
    expect(
      container.querySelector('[data-archived-persona="old-run.studio"]')
        ?.textContent,
    ).toContain("archived 3d ago");
  });

  it("archives a persona after confirmation and restores an archived one", async () => {
    const onarchive = vi.fn().mockResolvedValue(undefined);
    const onrestore = vi.fn().mockResolvedValue(undefined);
    const { container, getByLabelText, getByText, getAllByText } = render(
      HostRoster,
      {
        hosts: [host(roster)],
        now: NOW,
        canManage: true,
        canArchive: true,
        onarchive,
        onrestore,
      },
    );
    // Adapters are never offered for archive.
    expect(
      container.querySelector('[data-persona-archive="codex"]'),
    ).toBeNull();
    await fireEvent.click(getByLabelText("Archive config-judge"));
    expect(onarchive).not.toHaveBeenCalled();
    expect(
      container.querySelector('[data-persona-archive-confirm="config-judge"]')
        ?.textContent,
    ).toContain("loses access now");
    const confirm = getAllByText("Archive").find(
      (node) => node.closest("[data-persona-archive-confirm]") != null,
    );
    await fireEvent.click(confirm);
    expect(onarchive).toHaveBeenCalledTimes(1);
    expect(onarchive.mock.calls[0][1].name).toBe("config-judge");

    await fireEvent.click(getByText("Archived (1)"));
    await fireEvent.click(getByLabelText("Restore old-run on studio"));
    expect(onrestore).toHaveBeenCalledTimes(1);
    expect(onrestore.mock.calls[0][1].name).toBe("old-run");
  });

  it("offers no archive or restore controls to agents", async () => {
    const { container, getByText } = render(HostRoster, {
      hosts: [host(roster)],
      now: NOW,
      canManage: true,
      canArchive: false,
    });
    expect(container.querySelector("[data-persona-archive]")).toBeNull();
    await fireEvent.click(getByText("Archived (1)"));
    expect(container.querySelector("[data-persona-restore]")).toBeNull();
  });
});
