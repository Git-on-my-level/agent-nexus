import { describe, expect, it } from "vitest";

import { formatWait } from "../../src/lib/inboxMailbox.js";
import {
  PM_STATES,
  isPmNotOnboardedRefusal,
  normalizePmState,
  pmConnected,
  pmFeaturesVisible,
  pmInstallCommand,
  pmLastSeenLabel,
  pmOffline,
  pmSetupOffered,
  pmStateFromPresenceResponse,
  pmStatusCommand,
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
  });

  it("hides every PM surface, and only offers setup, when none is onboarded", () => {
    expect(visibility(PM_STATES.NOT_ONBOARDED)).toEqual({
      features: false,
      setup: true,
    });
  });

  it("shows PM features when one is connected", () => {
    expect(visibility(PM_STATES.CONNECTED)).toEqual({
      features: true,
      setup: false,
    });
    expect(pmConnected({ state: PM_STATES.CONNECTED })).toBe(true);
  });

  it("keeps PM features while one is offline, and still allows sending", () => {
    expect(visibility(PM_STATES.OFFLINE)).toEqual({
      features: true,
      setup: false,
    });
    expect(pmOffline({ state: PM_STATES.OFFLINE })).toBe(true);
  });

  /*
   * Back-compatibility: an older core reports no PM state, and a read can
   * fail. Neither is evidence that no PM exists, and hiding Ask PM on a
   * failed read would break a workspace whose PM is running fine. Core's own
   * refusal is what actually enforces the gate.
   */
  it("leaves PM features alone when the state is unknown, and offers no setup", () => {
    expect(visibility(PM_STATES.UNKNOWN)).toEqual({
      features: true,
      setup: false,
    });
    expect(visibility(undefined)).toEqual({
      features: true,
      setup: false,
    });
  });
});

describe("PM commands", () => {
  it("names the workspace when the deployment knows its API origin", () => {
    const options = { cliBaseUrl: "https://anx.example.test/o/local/w/ops" };
    expect(pmInstallCommand(options)).toBe(
      "anx --base-url https://anx.example.test/o/local/w/ops pm install",
    );
    expect(pmStatusCommand(options)).toBe(
      "anx --base-url https://anx.example.test/o/local/w/ops pm status",
    );
    expect(pmUninstallCommand(options)).toBe(
      "anx --base-url https://anx.example.test/o/local/w/ops pm uninstall",
    );
  });

  it("omits --base-url rather than printing an empty flag", () => {
    expect(pmInstallCommand()).toBe("anx pm install");
    expect(pmInstallCommand({ cliBaseUrl: "   " })).toBe("anx pm install");
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
    ).toBe("PM offline, last seen 5m ago, on studio");
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
