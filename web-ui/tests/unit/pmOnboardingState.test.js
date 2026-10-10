import { describe, expect, it } from "vitest";

import { formatWait } from "../../src/lib/inboxMailbox.js";
import {
  PM_STATES,
  isPmNotOnboardedRefusal,
  normalizePmState,
  pmConnected,
  pmFeaturesVisible,
  pmInstallCommand,
  pmKnownAbsent,
  pmLastSeenLabel,
  pmOffline,
  pmSetupOffered,
  pmStateFromPresenceResponse,
  pmStateKnown,
  pmStatusCommand,
  pmStatusLabel,
  pmStatusSummary,
  pmUninstallCommand,
} from "../../src/lib/pm/onboardingState.js";

const AT = "2026-10-09T12:00:00.000Z";
const NOW = Date.parse("2026-10-09T12:05:00.000Z");

describe("PM state normalization", () => {
  it("reads the contract fields", () => {
    expect(
      normalizePmState({
        state: "connected",
        last_seen: AT,
        runner: "Hermes",
        host: "studio",
      }),
    ).toEqual({
      state: "connected",
      lastSeen: AT,
      runner: "Hermes",
      host: "studio",
    });
  });

  it("accepts camelCase and a bare state string", () => {
    expect(normalizePmState({ state: "offline", lastSeen: AT }).lastSeen).toBe(
      AT,
    );
    expect(normalizePmState("not_onboarded")).toEqual({
      state: PM_STATES.NOT_ONBOARDED,
      lastSeen: "",
      runner: "",
      host: "",
    });
  });

  it("treats anything it cannot recognise as unknown, never as no PM", () => {
    for (const raw of [
      undefined,
      null,
      "",
      42,
      {},
      { state: "" },
      { state: "retired" },
    ]) {
      expect(normalizePmState(raw).state).toBe(PM_STATES.UNKNOWN);
    }
  });
});

describe("mapping core's GET /pm/presence answer", () => {
  it("reads the computed state and the PM's own labels", () => {
    expect(
      pmStateFromPresenceResponse({
        state: "offline",
        last_seen: AT,
        runner: "Hermes",
        host: "studio",
        configured: true,
        connected: false,
      }),
    ).toEqual({
      state: PM_STATES.OFFLINE,
      lastSeen: AT,
      runner: "Hermes",
      host: "studio",
    });
  });

  it("carries no label for a PM that does not exist", () => {
    expect(
      pmStateFromPresenceResponse({
        state: "not_onboarded",
        last_seen: null,
        runner: null,
        host: null,
        configured: false,
        connected: false,
      }),
    ).toEqual({
      state: PM_STATES.NOT_ONBOARDED,
      lastSeen: "",
      runner: "",
      host: "",
    });
  });

  it("falls back to the legacy booleans when core sends no state", () => {
    expect(
      pmStateFromPresenceResponse({ configured: false, connected: false }),
    ).toEqual({
      state: PM_STATES.NOT_ONBOARDED,
      lastSeen: "",
      runner: "",
      host: "",
    });
  });

  /*
   * Core's presence projection is newer than the PM itself, so a workspace
   * that has used its PM for months reports a registered PM with no last-seen
   * the first time it runs this core. Reading that as "no PM" would hide its
   * pending proposals and its conversations, so `configured` is what counts.
   */
  it("calls a legacy registered PM with no heartbeat offline, not absent", () => {
    expect(
      pmStateFromPresenceResponse({ configured: true, connected: false }),
    ).toEqual({
      state: PM_STATES.OFFLINE,
      lastSeen: "",
      runner: "",
      host: "",
    });
  });

  it("reads connected and offline from the legacy heartbeat", () => {
    expect(
      pmStateFromPresenceResponse({
        configured: true,
        connected: true,
        last_seen_at: AT,
        signal: "heartbeat",
      }),
    ).toEqual({
      state: PM_STATES.CONNECTED,
      lastSeen: AT,
      runner: "",
      host: "",
    });
    expect(
      pmStateFromPresenceResponse({
        configured: true,
        connected: false,
        last_seen_at: AT,
      }),
    ).toMatchObject({ state: PM_STATES.OFFLINE, lastSeen: AT });
  });

  it("ignores a state it does not recognise and uses the booleans", () => {
    expect(
      pmStateFromPresenceResponse({
        state: "retired",
        configured: true,
        connected: true,
        last_seen_at: AT,
      }),
    ).toMatchObject({ state: PM_STATES.CONNECTED, lastSeen: AT });
  });

  it("reports unknown when there is no answer to read", () => {
    for (const raw of [undefined, null, "", 42]) {
      expect(pmStateFromPresenceResponse(raw).state).toBe(PM_STATES.UNKNOWN);
    }
  });
});

describe("what each PM state may show", () => {
  const visibility = (state) => ({
    features: pmFeaturesVisible({ state }),
    setup: pmSetupOffered({ state }),
    absent: pmKnownAbsent({ state }),
    known: pmStateKnown({ state }),
  });

  it("hides every PM surface, and only offers setup, when none is onboarded", () => {
    expect(visibility(PM_STATES.NOT_ONBOARDED)).toEqual({
      features: false,
      setup: true,
      absent: true,
      known: true,
    });
  });

  it("shows PM features when one is connected", () => {
    expect(visibility(PM_STATES.CONNECTED)).toEqual({
      features: true,
      setup: false,
      absent: false,
      known: true,
    });
    expect(pmConnected({ state: PM_STATES.CONNECTED })).toBe(true);
  });

  it("keeps PM features while one is offline, and still allows sending", () => {
    expect(visibility(PM_STATES.OFFLINE)).toEqual({
      features: true,
      setup: false,
      absent: false,
      known: true,
    });
    expect(pmOffline({ state: PM_STATES.OFFLINE })).toBe(true);
  });

  /*
   * Before the first read returns, and after one fails. Neither label is
   * known, so nothing about the PM is shown: rendering Ask PM on a guess
   * makes it flash in and out on every load, and a read that never succeeds
   * would leave a button that cannot work. Setup is not offered either —
   * that would invite a second PM alongside one already running.
   */
  it("shows nothing, and offers no setup, while the state is unknown", () => {
    const nothing = {
      features: false,
      setup: false,
      absent: false,
      known: false,
    };
    expect(visibility(PM_STATES.UNKNOWN)).toEqual(nothing);
    expect(visibility(undefined)).toEqual(nothing);
    // `null` presence is the shell before it has read this workspace at all.
    expect(pmFeaturesVisible(null)).toBe(false);
    expect(pmSetupOffered(null)).toBe(false);
    expect(pmStateKnown(null)).toBe(false);
  });

  /*
   * `pmKnownAbsent` is the mirror image of `pmFeaturesVisible`, not its
   * negation: both are false while the state is unknown. That is what keeps a
   * slow or failed read from refusing a PM write for a workspace that has a
   * PM — the write defers to core, which refuses it if there really is none.
   */
  it("never claims a PM is absent on an unproven state", () => {
    for (const state of ["", "unknown", "retired", undefined]) {
      expect(pmKnownAbsent({ state })).toBe(false);
      expect(pmFeaturesVisible({ state })).toBe(false);
    }
    // Exactly one of the two is true for every state core can report.
    for (const state of [
      PM_STATES.NOT_ONBOARDED,
      PM_STATES.CONNECTED,
      PM_STATES.OFFLINE,
    ]) {
      expect(pmKnownAbsent({ state })).toBe(!pmFeaturesVisible({ state }));
    }
  });
});

describe("PM commands", () => {
  /*
   * The base URL is quoted. It is deployment configuration — `ANX_WORKSPACES`,
   * a hosted `core_origin`, the request origin — and a copyable command built
   * by concatenation runs whatever a `$(…)` in one of them says.
   */
  it("names the workspace when the deployment knows its API origin", () => {
    const options = { cliBaseUrl: "https://anx.example.test/o/local/w/ops" };
    expect(pmInstallCommand(options)).toBe(
      "anx --base-url 'https://anx.example.test/o/local/w/ops' pm install",
    );
    expect(pmStatusCommand(options)).toBe(
      "anx --base-url 'https://anx.example.test/o/local/w/ops' pm status",
    );
    expect(pmUninstallCommand(options)).toBe(
      "anx --base-url 'https://anx.example.test/o/local/w/ops' pm uninstall",
    );
  });

  it("omits --base-url rather than printing an empty flag", () => {
    expect(pmInstallCommand()).toBe("anx pm install");
    expect(pmInstallCommand({ cliBaseUrl: "   " })).toBe("anx pm install");
  });
});

/*
 * The command `/pm/setup` tells the reader to run has to reach the CLI's
 * install wizard, or they get `runner_required` and a dead end.
 *
 * `anx` detects the wizard from the argv shape: the subcommand is `pm
 * install` and no runner flag was given. Global options come before the
 * subcommand, so they shift its position — which is exactly how the first
 * version of this broke (`app.go` matched `len(args) == 2`, true only for a
 * bare `anx pm install`). These assertions pin the shape rather than the
 * position, so the copied command keeps reaching the wizard whatever global
 * options the workspace needs.
 */
describe("the setup command reaches the install wizard", () => {
  /** Global options this UI can emit, and whether they take a value. */
  const GLOBAL_OPTIONS_WITH_VALUES = new Set(["--base-url"]);

  /** The subcommand, the way a CLI reads it: global options, then the verb. */
  function subcommandOf(command) {
    const tokens = command.split(" ").filter(Boolean);
    expect(tokens.shift()).toBe("anx");
    const words = [];
    while (tokens.length) {
      const token = tokens.shift();
      if (!token.startsWith("-")) {
        words.push(token);
        continue;
      }
      if (token.includes("=")) continue;
      if (GLOBAL_OPTIONS_WITH_VALUES.has(token)) {
        expect(tokens.length).toBeGreaterThan(0);
        tokens.shift();
        continue;
      }
      words.push(token);
    }
    return words;
  }

  const SHAPES = [
    ["no base URL configured", {}],
    [
      "a workspace-scoped base URL",
      { cliBaseUrl: "https://anx.example.test/o/local/w/ops" },
    ],
  ];

  it.each(SHAPES)("parses to the wizard path with %s", (_label, options) => {
    const command = pmInstallCommand(options);
    // The verb is `pm install`, and nothing else is passed to it.
    expect(subcommandOf(command)).toEqual(["pm", "install"]);
    // No runner flag, so the wizard asks rather than refusing.
    expect(command).not.toMatch(/(^|\s)--runner(\s|=|$)/);
    expect(command).not.toMatch(/(^|\s)--wait(-timeout)?(\s|=|$)/);
    // Nothing that would make the CLI non-interactive.
    expect(command).not.toMatch(/(^|\s)--json(\s|=|$)/);
  });

  it("puts every global option before the subcommand", () => {
    const command = pmInstallCommand({
      cliBaseUrl: "https://anx.example.test/o/local/w/ops",
    });
    expect(command).toBe(
      "anx --base-url 'https://anx.example.test/o/local/w/ops' pm install",
    );
    // `pm install` is last, so a parser that reads the verb from the tail
    // and one that skips global options both land on the wizard.
    expect(command.endsWith(" pm install")).toBe(true);
  });

  /*
   * `status` and `uninstall` take the same global options and must stay
   * parseable the same way; they have no wizard, only a verb to reach.
   */
  it("keeps the manage commands parseable too", () => {
    const options = { cliBaseUrl: "https://anx.example.test/o/local/w/ops" };
    expect(subcommandOf(pmStatusCommand(options))).toEqual(["pm", "status"]);
    expect(subcommandOf(pmUninstallCommand(options))).toEqual([
      "pm",
      "uninstall",
    ]);
  });
});

describe("PM status copy", () => {
  it("says where a connected PM runs", () => {
    expect(
      pmStatusSummary(
        { state: PM_STATES.CONNECTED, runner: "Hermes", host: "studio" },
        NOW,
        formatWait,
      ),
    ).toBe("PM connected, Hermes on studio");
  });

  it("says how long an offline PM has been gone", () => {
    expect(
      pmStatusSummary(
        { state: PM_STATES.OFFLINE, lastSeen: AT, host: "studio" },
        NOW,
        formatWait,
      ),
    ).toBe("PM offline, last seen 5 min ago, on studio");
  });

  it("never renders a bare separator for a runner or host core did not report", () => {
    expect(
      pmStatusSummary({ state: PM_STATES.CONNECTED }, NOW, formatWait),
    ).toBe("PM connected");
    expect(pmStatusSummary({ state: PM_STATES.OFFLINE }, NOW, formatWait)).toBe(
      "PM offline",
    );
  });

  it("says nothing at all when no state is known", () => {
    expect(pmStatusSummary(null, NOW, formatWait)).toBe("");
    expect(
      pmStatusSummary({ state: PM_STATES.NOT_ONBOARDED }, NOW, formatWait),
    ).toBe("");
  });

  it("drops a last-seen it cannot trust", () => {
    expect(pmLastSeenLabel({ lastSeen: "" }, NOW, formatWait)).toBe("");
    expect(pmLastSeenLabel({ lastSeen: "soon" }, NOW, formatWait)).toBe("");
    // A clock-skewed future heartbeat is not "in -3m ago".
    expect(
      pmLastSeenLabel(
        { lastSeen: "2026-10-09T12:08:00.000Z" },
        NOW,
        formatWait,
      ),
    ).toBe("");
  });
});

describe("core's refusal when a workspace has no PM onboarded", () => {
  /*
   * Core does not emit this code yet. It is the reserved per-workspace
   * answer; today a missing PM identity and a PM bridge that is merely down
   * both answer 503 `unavailable`, and those two cannot be told apart from
   * the response — so a bare 503 must keep its error rather than being read
   * as "install a PM".
   */
  it("recognises the reserved code", () => {
    expect(
      isPmNotOnboardedRefusal({
        status: 409,
        body: { error: { code: "pm_not_onboarded" } },
      }),
    ).toBe(true);
  });

  it("never reads a transient PM outage as a missing PM", () => {
    expect(
      isPmNotOnboardedRefusal({
        status: 503,
        body: {
          error: { code: "unavailable", message: "PM bridge unavailable" },
        },
      }),
    ).toBe(false);
    expect(isPmNotOnboardedRefusal({ status: 503 })).toBe(false);
  });

  it("does not swallow any other failure", () => {
    expect(isPmNotOnboardedRefusal(new Error("network down"))).toBe(false);
    expect(
      isPmNotOnboardedRefusal({
        status: 401,
        body: { error: { code: "auth_required" } },
      }),
    ).toBe(false);
    expect(isPmNotOnboardedRefusal({ status: 500 })).toBe(false);
    expect(isPmNotOnboardedRefusal(undefined)).toBe(false);
  });
});

describe("pmStatusLabel", () => {
  /*
   * The header has room for a state, not for a sentence about where a
   * process is running: the badge wears the word and `pmStatusSummary`
   * becomes its tooltip.
   */
  it("is one word, with no runner or host in it", () => {
    expect(
      pmStatusLabel({
        state: PM_STATES.CONNECTED,
        runner: "Hermes",
        host: "studio",
      }),
    ).toBe("Connected");
    expect(pmStatusLabel({ state: PM_STATES.OFFLINE, host: "studio" })).toBe(
      "Offline",
    );
  });

  it("says nothing for a state core has not reported", () => {
    expect(pmStatusLabel(null)).toBe("");
    expect(pmStatusLabel({ state: PM_STATES.NOT_ONBOARDED })).toBe("");
  });
});
