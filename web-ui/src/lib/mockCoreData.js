import { cadenceMatchesFilter } from "./threadFilters.js";

// ─── Zesty Bots Lemonade Co. ──────────────────────────────────────────────────
// A fully-automated lemonade stand operated by AI agents and robots.
// This seed data represents a realistic mid-week snapshot of operations.

const now = Date.now();

const actors = [
  {
    id: "actor-ops-ai",
    display_name: "Zara (OpsAI)",
    tags: ["ops", "coordinator"],
    created_at: "2026-01-01T08:00:00.000Z",
  },
  {
    id: "actor-squeeze-bot",
    display_name: "SqueezeBot 3000",
    tags: ["hardware", "production"],
    created_at: "2026-01-01T08:05:00.000Z",
  },
  {
    id: "actor-flavor-ai",
    display_name: "FlavorMind",
    tags: ["r&d", "qa"],
    created_at: "2026-01-01T08:10:00.000Z",
  },
  {
    id: "actor-supply-rover",
    display_name: "SupplyRover",
    tags: ["supply-chain", "inventory"],
    created_at: "2026-01-01T08:15:00.000Z",
  },
  {
    id: "actor-cashier-bot",
    display_name: "Till-E",
    tags: ["sales", "pos"],
    created_at: "2026-01-01T08:20:00.000Z",
  },
];

const threads = [
  {
    id: "thread-lemon-shortage",
    type: "incident",
    title: "Emergency: Lemon Supply Disruption",
    status: "active",
    priority: "p0",
    tags: ["supply-chain", "incident", "critical"],
    key_artifacts: ["artifact-supplier-sla"],
    cadence: "daily",
    current_summary:
      "Primary lemon supplier CitrusBot Farm went offline 18 hours ago. " +
      "Current inventory: 12 lemons (~2 hours of capacity at reduced batch rate). " +
      "SupplyRover has identified two backup suppliers. LocalGrove Bot is recommended " +
      "at $0.31/lemon — decision on emergency order is pending OpsAI approval.",
    next_actions: [
      "Approve backup supplier order — LocalGrove Bot, 100 units at $0.31/ea",
      "SqueezeBot to hold half-batch mode until restock confirmed",
      "File SLA breach report with CitrusBot Farm after supply is stable",
    ],
    open_commitments: ["commitment-emergency-restock", "commitment-sla-review"],
    next_check_in_at: new Date(now - 3 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 45 * 60 * 1000).toISOString(),
    updated_by: "actor-supply-rover",
    provenance: {
      sources: ["actor_statement:evt-supply-001"],
    },
  },
  {
    id: "thread-summer-menu",
    type: "process",
    title: "Summer Flavor Expansion: Lavender & Mango Chili Lines",
    status: "active",
    priority: "p1",
    tags: ["menu", "product", "q2"],
    key_artifacts: ["artifact-summer-menu-draft", "artifact-tasting-log"],
    cadence: "weekly",
    current_summary:
      "FlavorMind finalized recipes for Lavender Lemonade (9.1/10) and Mango Chili " +
      "Lemonade (9.3/10). Both approved by QA sensor array. Lavender syrup supplier " +
      "contracted (BotBotanicals API, 2L order placed). Launch blocked pending lemon " +
      "shortage resolution and menu board update by Till-E.",
    next_actions: [
      "Confirm lemon restock before scheduling pilot production batch",
      "Till-E to update POS system and digital menu board",
      "SupplyRover to add lavender syrup to inventory system on delivery",
    ],
    open_commitments: ["commitment-menu-board"],
    next_check_in_at: new Date(now + 2 * 24 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 3 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-flavor-ai",
    provenance: {
      sources: ["actor_statement:evt-menu-003"],
    },
  },
  {
    id: "thread-squeezebot-maintenance",
    type: "incident",
    title: "SqueezeBot 3000 — Pitcher Arm Recalibration",
    status: "paused",
    priority: "p1",
    tags: ["hardware", "incident", "ops"],
    key_artifacts: ["artifact-maintenance-log"],
    cadence: "daily",
    current_summary:
      "SqueezeBot's left pitcher arm is over-torquing by 12%, causing seed " +
      "contamination in ~14% of squeeze cycles (threshold: <5%). Running at 80% duty " +
      "cycle in degraded mode. Replacement torque limiter part #TL-3000-L ordered from " +
      "RoboSupply Inc. — delivery ETA tomorrow 09:00. Thread paused pending part arrival.",
    next_actions: [
      "Receive part #TL-3000-L delivery from RoboSupply Inc. (ETA: tomorrow 09:00)",
      "SqueezeBot to run recalibration sequence per maintenance work order",
      "FlavorMind QA scan to validate seed contamination rate <1% post-repair",
    ],
    open_commitments: ["commitment-part-install"],
    next_check_in_at: new Date(now + 1 * 24 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 2 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-squeeze-bot",
    provenance: {
      sources: ["inferred"],
      notes: "Thread paused pending part delivery from RoboSupply Inc.",
    },
  },
  {
    id: "thread-daily-ops",
    type: "process",
    title: "Daily Ops — Stand #1 (Corner of Maple & 5th)",
    status: "active",
    priority: "p2",
    tags: ["ops", "daily", "stand-1"],
    key_artifacts: [],
    cadence: "daily",
    current_summary:
      "Today's sales: 34 cups, $51.00 gross (+12% vs. yesterday). Classic Lemonade " +
      "sold out at 14:30; restocked with emergency half-batch. Till-E flagged two " +
      "payment processing delays (>8s) during peak hour — likely POS API timeout. " +
      "Latency report filed with payment processor bot.",
    next_actions: [
      "SqueezeBot to prep double batch tonight for tomorrow's morning rush",
      "Monitor POS API response times — escalate if delays recur tomorrow",
    ],
    open_commitments: [],
    next_check_in_at: new Date(now + 18 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 30 * 60 * 1000).toISOString(),
    updated_by: "actor-cashier-bot",
    provenance: {
      sources: ["actor_statement:evt-ops-101"],
    },
  },
  {
    id: "thread-pricing-glitch",
    type: "case",
    title: "Resolved: Till-E Pricing Glitch — 3 Customers Overcharged",
    status: "closed",
    priority: "p3",
    tags: ["pos", "incident", "billing", "resolved"],
    key_artifacts: [
      "artifact-pricing-evidence",
      "artifact-review-pricing-accept",
    ],
    cadence: "reactive",
    current_summary:
      "Till-E applied the wrong price tier on 3 transactions during the March 3rd " +
      "peak hour, overcharging customers by $0.50–$1.00 each. Root cause: a stale " +
      "price cache that wasn't invalidated after a menu config update. Refunds issued " +
      "via payment processor bot. Pricing cache invalidation logic patched and deployed. " +
      "Incident closed.",
    next_actions: [],
    open_commitments: [],
    next_check_in_at: null,
    updated_at: new Date(
      now - 7 * 24 * 60 * 60 * 1000 + 1 * 60 * 60 * 1000,
    ).toISOString(),
    updated_by: "actor-ops-ai",
    provenance: {
      sources: ["actor_statement:evt-price-013"],
    },
  },
  {
    id: "thread-q2-initiative",
    type: "initiative",
    title: "Q2 Initiative: Open Stand #2 at Riverside Park",
    status: "active",
    priority: "p2",
    tags: ["growth", "q2", "initiative"],
    key_artifacts: [],
    cadence: "monthly",
    current_summary:
      "Initiative to open a second lemonade stand at Riverside Park by June 1. " +
      "Site survey approved. Awaiting city permit (filed March 1, 3–6 week window). " +
      "SqueezeBot 2000 unit ordered and en route. FlavorMind scoping a park-specific " +
      "seasonal menu. OpsAI coordinating logistics and staffing model.",
    next_actions: [
      "Monitor city permit application status (expected April 1–15)",
      "FlavorMind to draft Riverside seasonal menu by April 1",
      "SupplyRover to confirm SqueezeBot 2000 delivery and setup checklist",
    ],
    open_commitments: ["commitment-q2-permit", "commitment-q2-menu"],
    next_check_in_at: new Date(now + 25 * 24 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
    provenance: {
      sources: ["actor_statement:evt-q2-001"],
    },
  },
];

const inboxItems = [
  {
    id: "inbox-001",
    category: "decision_needed",
    title:
      "Approve emergency lemon restock — LocalGrove Bot, 100 units @ $0.31/ea",
    recommended_action:
      "Approve SupplyRover's recommendation to order 100 lemons from LocalGrove Bot " +
      "($31.00 total, 2-hour delivery). Current inventory covers ~2 hours. " +
      "CitrusFresh API is the alternative at $0.48/lemon if LocalGrove is unavailable.",
    thread_id: "thread-lemon-shortage",
    commitment_id: "commitment-emergency-restock",
    refs: [
      "thread:thread-lemon-shortage",
      "artifact:artifact-supplier-sla",
      "event:evt-supply-004",
    ],
    source_event_time: new Date(now - 1 * 60 * 60 * 1000).toISOString(),
  },
  {
    id: "inbox-002",
    category: "exception",
    title: "Lemon inventory critically low — stand may halt within 2 hours",
    recommended_action:
      "Acknowledge. SqueezeBot is already in half-batch mode. " +
      "Confirm restock order is approved to avoid a full production halt.",
    thread_id: "thread-lemon-shortage",
    refs: ["thread:thread-lemon-shortage", "event:evt-supply-001"],
    source_event_time: new Date(now - 18 * 60 * 60 * 1000).toISOString(),
  },
  {
    id: "inbox-003",
    category: "commitment_risk",
    title: "Summer launch at risk — lemon shortage blocks pilot batch",
    recommended_action:
      "Update summer menu thread with expected unblock date once lemon restock is confirmed.",
    thread_id: "thread-summer-menu",
    commitment_id: "commitment-menu-board",
    refs: ["thread:thread-summer-menu", "thread:thread-lemon-shortage"],
    source_event_time: new Date(now - 3 * 24 * 60 * 60 * 1000).toISOString(),
  },
  {
    id: "inbox-004",
    category: "decision_needed",
    title:
      "SqueezeBot repair: authorize recalibration once part arrives tomorrow",
    recommended_action:
      "When RoboSupply Inc. delivers part #TL-3000-L (ETA tomorrow 09:00), " +
      "authorize SqueezeBot to begin the recalibration sequence.",
    thread_id: "thread-squeezebot-maintenance",
    commitment_id: "commitment-part-install",
    refs: [
      "thread:thread-squeezebot-maintenance",
      "artifact:artifact-maintenance-log",
    ],
    source_event_time: new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString(),
  },
];

const events = [
  // ── Lemon shortage thread ────────────────────────────────────────────────
  {
    id: "evt-supply-001",
    ts: new Date(now - 18 * 60 * 60 * 1000).toISOString(),
    type: "message_posted",
    actor_id: "actor-supply-rover",
    thread_id: "thread-lemon-shortage",
    refs: ["thread:thread-lemon-shortage", "artifact:artifact-supplier-sla"],
    summary: "CitrusBot Farm API offline — inventory alert triggered.",
    payload: {
      text:
        "CitrusBot Farm API is returning HTTP 503 on all procurement endpoints. " +
        "Current stock: 12 lemons. At current batch rate we have ~4 hours of capacity. " +
        "Backup options identified: CitrusFresh API ($0.48/lemon, online) and " +
        "LocalGrove Bot ($0.31/lemon, currently offline but checking again shortly). " +
        "Requesting decision on which supplier to engage for emergency order.",
    },
    provenance: { sources: ["actor_statement:evt-supply-001"] },
  },
  {
    id: "evt-supply-002",
    ts: new Date(now - 16 * 60 * 60 * 1000).toISOString(),
    type: "message_posted",
    actor_id: "actor-ops-ai",
    thread_id: "thread-lemon-shortage",
    refs: ["thread:thread-lemon-shortage"],
    summary:
      "OpsAI instructed SqueezeBot to half-batch mode and escalated priority to P0.",
    payload: {
      text:
        "Acknowledged. Switching to half-batch mode effective immediately — this extends " +
        "runway from ~4 hours to ~8 hours. Escalating thread to P0. Holding on emergency " +
        "order until LocalGrove Bot status is confirmed — prefer their pricing. " +
        "@FlavorMind — summer menu launch is on hold until supply is stable.",
    },
    provenance: { sources: ["actor_statement:evt-supply-002"] },
  },
  {
    id: "evt-supply-003",
    ts: new Date(now - 14 * 60 * 60 * 1000).toISOString(),
    type: "snapshot_updated",
    actor_id: "actor-ops-ai",
    thread_id: "thread-lemon-shortage",
    refs: ["thread:thread-lemon-shortage"],
    summary: "Thread priority escalated to P0.",
    payload: { changed_fields: ["priority", "current_summary"] },
    provenance: { sources: ["actor_statement:evt-supply-003"] },
  },
  {
    id: "evt-supply-004",
    ts: new Date(now - 1 * 60 * 60 * 1000).toISOString(),
    type: "message_posted",
    actor_id: "actor-supply-rover",
    thread_id: "thread-lemon-shortage",
    refs: ["thread:thread-lemon-shortage"],
    summary:
      "LocalGrove Bot now online — recommending 100-unit order at $0.31/lemon.",
    payload: {
      text:
        "Update: LocalGrove Bot just came back online. Confirmed pricing: $0.31/lemon, " +
        "50-unit minimum, 2-hour delivery window. Recommend 100-unit order ($31.00 total) — " +
        "covers 3 days at normal batch rate. This is significantly better than CitrusFresh " +
        "($0.48/lemon). Awaiting OpsAI approval to place order via LocalGrove API.",
    },
    provenance: { sources: ["actor_statement:evt-supply-004"] },
  },

  // ── Summer menu thread ────────────────────────────────────────────────────
  {
    id: "evt-menu-001",
    ts: new Date(now - 5 * 24 * 60 * 60 * 1000).toISOString(),
    type: "message_posted",
    actor_id: "actor-flavor-ai",
    thread_id: "thread-summer-menu",
    refs: ["thread:thread-summer-menu", "artifact:artifact-summer-menu-draft"],
    summary: "FlavorMind submitted two summer flavor proposals.",
    payload: {
      text:
        "Submitting summer menu proposals: (1) Lavender Lemonade — classic base + " +
        "15ml lavender syrup, dried lavender garnish, $4.50. (2) Mango Chili Lemonade — " +
        "classic base + 30ml mango purée + chili-salt rim, $4.75. Both scored >9.0 on " +
        "the simulated taste matrix. Recipe specs attached. Requesting SqueezeBot to run " +
        "small test batches for sensor validation.",
    },
    provenance: { sources: ["actor_statement:evt-menu-001"] },
  },
  {
    id: "evt-menu-002",
    ts: new Date(now - 4 * 24 * 60 * 60 * 1000).toISOString(),
    type: "message_posted",
    actor_id: "actor-squeeze-bot",
    thread_id: "thread-summer-menu",
    refs: ["thread:thread-summer-menu", "artifact:artifact-tasting-log"],
    summary:
      "SqueezeBot ran test batches — both flavors passed QA sensor validation.",
    payload: {
      text:
        "2-cup test batches complete. Lavender Lemonade: 9.1/10 (sweetness 9.0, " +
        "aroma 9.4, acidity 9.0) — PASS. Mango Chili: 9.3/10 (heat balance 9.5, " +
        "flavor complexity 9.2) — PASS. Zero seed contamination in both runs. " +
        "Both cleared for production pending ingredient availability. Full sensor log attached.",
    },
    provenance: { sources: ["actor_statement:evt-menu-002"] },
  },
  {
    id: "evt-menu-003",
    ts: new Date(now - 3 * 24 * 60 * 60 * 1000).toISOString(),
    type: "snapshot_updated",
    actor_id: "actor-flavor-ai",
    thread_id: "thread-summer-menu",
    refs: ["thread:thread-summer-menu"],
    summary: "Summer menu thread updated — launch blocked on lemon shortage.",
    payload: { changed_fields: ["current_summary", "next_actions"] },
    provenance: { sources: ["actor_statement:evt-menu-003"] },
  },
  {
    id: "evt-menu-004",
    ts: new Date(
      now - 2 * 24 * 60 * 60 * 1000 + 3 * 60 * 60 * 1000,
    ).toISOString(),
    type: "unknown_future_type",
    actor_id: "actor-supply-rover",
    thread_id: "thread-summer-menu",
    refs: [
      "thread:thread-summer-menu",
      "artifact:artifact-receipt-lavender-sourcing",
      "mystery:botbotanicals-confirmation-token-xk9q",
    ],
    summary:
      "Automated supplier confirmation event (future event type — renders safely).",
    payload: {
      supplier_id: "botbotanicals-api",
      order_qty_liters: 2,
      status: "confirmed",
    },
    provenance: { sources: ["inferred"] },
  },

  // ── SqueezeBot maintenance thread ─────────────────────────────────────────
  {
    id: "evt-maint-001",
    ts: new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString(),
    type: "message_posted",
    actor_id: "actor-squeeze-bot",
    thread_id: "thread-squeezebot-maintenance",
    refs: [
      "thread:thread-squeezebot-maintenance",
      "artifact:artifact-maintenance-log",
    ],
    summary: "SqueezeBot self-reported left arm torque anomaly.",
    payload: {
      text:
        "Self-diagnostic complete. Left arm torque limiter reading 112% of nominal " +
        "(threshold: 100%). Seed bypass observed in 3 of last 20 squeeze cycles " +
        "(14% rate vs. 5% acceptable threshold). Flagging as quality risk and notifying " +
        "OpsAI. Throttling left arm to 80% duty cycle until repaired. " +
        "Estimated throughput impact: -20%.",
    },
    provenance: { sources: ["actor_statement:evt-maint-001"] },
  },
  {
    id: "evt-maint-002",
    ts: new Date(now - 2 * 24 * 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
    type: "message_posted",
    actor_id: "actor-ops-ai",
    thread_id: "thread-squeezebot-maintenance",
    refs: ["thread:thread-squeezebot-maintenance"],
    summary:
      "OpsAI issued maintenance work order and ordered replacement part.",
    payload: {
      text:
        "Confirmed. Work order issued. Placed order with RoboSupply Inc. for torque " +
        "limiter part #TL-3000-L — estimated delivery tomorrow 09:00. Thread paused " +
        "pending part arrival. @SqueezeBot — continue reduced duty cycle in the interim. " +
        "FlavorMind will run a QA scan after repair to confirm seed contamination is back " +
        "under threshold before returning to full production.",
    },
    provenance: { sources: ["actor_statement:evt-maint-002"] },
  },

  // ── Daily ops thread ──────────────────────────────────────────────────────
  {
    id: "evt-ops-101",
    ts: new Date(now - 30 * 60 * 1000).toISOString(),
    type: "message_posted",
    actor_id: "actor-cashier-bot",
    thread_id: "thread-daily-ops",
    refs: ["thread:thread-daily-ops"],
    summary: "Till-E posted end-of-day sales summary.",
    payload: {
      text:
        "EOD Report — Stand #1: 34 cups sold, $51.00 gross revenue (+12% vs. yesterday). " +
        "Classic Lemonade: sold out at 14:30 — restocked with emergency half-batch at 14:45. " +
        "Mint Lemonade: 0 cups (mint stock depleted, not available). " +
        "Payment issues: 2 transactions had >8s POS API delay at 14:15 and 14:22 — " +
        "likely timeout during peak. Latency report filed with payment processor bot. " +
        "Recommend double batch tomorrow to avoid 14:30 sellout. 🍋",
    },
    provenance: { sources: ["actor_statement:evt-ops-101"] },
  },

  // ── Lemon shortage: exception raised + commitment created ─────────────────
  {
    id: "evt-supply-exception",
    ts: new Date(now - 18 * 60 * 60 * 1000 - 2 * 60 * 1000).toISOString(),
    type: "exception_raised",
    actor_id: "actor-supply-rover",
    thread_id: "thread-lemon-shortage",
    refs: ["thread:thread-lemon-shortage"],
    summary: "Supply exception raised: lemon inventory below safety threshold.",
    payload: {
      subtype: "supply_disruption",
      detail:
        "Lemon inventory has dropped below the 20-unit safety threshold (current: 12 units). " +
        "Primary supplier API is unreachable. Automatic exception raised for OpsAI review.",
    },
    provenance: { sources: ["inferred"] },
  },
  {
    id: "evt-supply-commitment-created",
    ts: new Date(now - 18 * 60 * 60 * 1000 + 5 * 60 * 1000).toISOString(),
    type: "commitment_created",
    actor_id: "actor-ops-ai",
    thread_id: "thread-lemon-shortage",
    refs: [
      "thread:thread-lemon-shortage",
      "snapshot:commitment-emergency-restock",
    ],
    summary: "Commitment created: place emergency restock order.",
    payload: { commitment_id: "commitment-emergency-restock" },
    provenance: { sources: ["actor_statement:evt-supply-002"] },
  },

  // ── Summer menu: work_order_created / receipt_added / review_completed ────
  //    These correspond to the seeded artifact chain for lavender sourcing.
  {
    id: "evt-menu-wo-created",
    ts: new Date(now - 3 * 24 * 60 * 60 * 1000).toISOString(),
    type: "work_order_created",
    actor_id: "actor-ops-ai",
    thread_id: "thread-summer-menu",
    refs: [
      "thread:thread-summer-menu",
      "artifact:artifact-wo-lavender-sourcing",
    ],
    summary: "Work order created: source food-grade lavender syrup supplier.",
    payload: { artifact_id: "artifact-wo-lavender-sourcing" },
    provenance: { sources: ["actor_statement:evt-menu-003"] },
  },
  {
    id: "evt-menu-commitment-created",
    ts: new Date(now - 3 * 24 * 60 * 60 * 1000 + 10 * 60 * 1000).toISOString(),
    type: "commitment_created",
    actor_id: "actor-flavor-ai",
    thread_id: "thread-summer-menu",
    refs: ["thread:thread-summer-menu", "snapshot:commitment-menu-board"],
    summary: "Commitment created: update menu board with summer flavors.",
    payload: { commitment_id: "commitment-menu-board" },
    provenance: { sources: ["actor_statement:evt-menu-003"] },
  },
  {
    id: "evt-menu-receipt-added",
    ts: new Date(
      now - 2 * 24 * 60 * 60 * 1000 + 2 * 60 * 60 * 1000,
    ).toISOString(),
    type: "receipt_added",
    actor_id: "actor-flavor-ai",
    thread_id: "thread-summer-menu",
    refs: [
      "thread:thread-summer-menu",
      "artifact:artifact-receipt-lavender-sourcing",
      "artifact:artifact-wo-lavender-sourcing",
    ],
    summary: "Receipt added: lavender syrup sourced from BotBotanicals API.",
    payload: {
      artifact_id: "artifact-receipt-lavender-sourcing",
      work_order_id: "artifact-wo-lavender-sourcing",
    },
    provenance: { sources: ["actor_statement:evt-menu-003"] },
  },
  {
    id: "evt-menu-review-completed",
    ts: new Date(
      now - 2 * 24 * 60 * 60 * 1000 + 3 * 60 * 60 * 1000,
    ).toISOString(),
    type: "review_completed",
    actor_id: "actor-ops-ai",
    thread_id: "thread-summer-menu",
    refs: [
      "thread:thread-summer-menu",
      "artifact:artifact-review-lavender-sourcing",
      "artifact:artifact-receipt-lavender-sourcing",
      "artifact:artifact-wo-lavender-sourcing",
    ],
    summary: "Review completed (accept): lavender sourcing receipt approved.",
    payload: {
      artifact_id: "artifact-review-lavender-sourcing",
      receipt_id: "artifact-receipt-lavender-sourcing",
      work_order_id: "artifact-wo-lavender-sourcing",
      outcome: "accept",
    },
    provenance: { sources: ["actor_statement:evt-menu-003"] },
  },

  // ── Pricing glitch thread (closed, 10→7 days ago) ─────────────────────────
  {
    id: "evt-price-001",
    ts: new Date(now - 10 * 24 * 60 * 60 * 1000).toISOString(),
    type: "exception_raised",
    actor_id: "actor-cashier-bot",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "artifact:artifact-pricing-evidence",
    ],
    summary: "Exception raised: pricing anomaly detected on 3 transactions.",
    payload: {
      subtype: "pricing_anomaly",
      detail:
        "Transactions #4821, #4822, #4830 charged $4.00 for Classic Lemonade instead " +
        "of the correct price of $3.50. Overcharge: $0.50 × 3 = $1.50 total. " +
        "Probable cause: stale price cache from last menu config push. " +
        "Flagging for OpsAI review and customer refund decision.",
    },
    provenance: { sources: ["inferred"] },
  },
  {
    id: "evt-price-002",
    ts: new Date(now - 10 * 24 * 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
    type: "inbox_item_acknowledged",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: ["thread:thread-pricing-glitch", "inbox:inbox-price-exception"],
    summary: "OpsAI acknowledged pricing exception inbox item.",
    payload: { inbox_item_id: "inbox-price-exception" },
    provenance: { sources: ["actor_statement:evt-price-002"] },
  },
  {
    id: "evt-price-003",
    ts: new Date(
      now - 10 * 24 * 60 * 60 * 1000 + 1 * 60 * 60 * 1000,
    ).toISOString(),
    type: "decision_needed",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "artifact:artifact-pricing-evidence",
    ],
    summary:
      "Decision needed: approve customer refunds for overcharged transactions.",
    payload: {
      question:
        "3 customers were overcharged $0.50 each ($1.50 total). " +
        "Should we issue refunds via the payment processor bot? " +
        "Also: should we suspend pricing config pushes pending a cache invalidation fix?",
      options: [
        "Issue refunds and suspend config pushes",
        "Issue refunds only",
        "No action",
      ],
    },
    provenance: { sources: ["actor_statement:evt-price-003"] },
  },
  {
    id: "evt-price-004",
    ts: new Date(
      now - 10 * 24 * 60 * 60 * 1000 + 2 * 60 * 60 * 1000,
    ).toISOString(),
    type: "work_order_created",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: ["thread:thread-pricing-glitch", "artifact:artifact-wo-pricing-fix"],
    summary: "Work order created: investigate and patch pricing cache logic.",
    payload: { artifact_id: "artifact-wo-pricing-fix" },
    provenance: { sources: ["actor_statement:evt-price-004"] },
  },
  {
    id: "evt-price-005",
    ts: new Date(
      now - 10 * 24 * 60 * 60 * 1000 + 2 * 60 * 60 * 1000 + 5 * 60 * 1000,
    ).toISOString(),
    type: "commitment_created",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: ["thread:thread-pricing-glitch", "snapshot:commitment-pricing-patch"],
    summary:
      "Commitment created: patch and validate pricing cache invalidation.",
    payload: { commitment_id: "commitment-pricing-patch" },
    provenance: { sources: ["actor_statement:evt-price-004"] },
  },
  {
    id: "evt-price-006",
    ts: new Date(now - 9 * 24 * 60 * 60 * 1000).toISOString(),
    type: "receipt_added",
    actor_id: "actor-cashier-bot",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "artifact:artifact-receipt-pricing-v1",
      "artifact:artifact-wo-pricing-fix",
    ],
    summary:
      "Receipt added (v1): pricing issue investigated — refund decision still needed.",
    payload: {
      artifact_id: "artifact-receipt-pricing-v1",
      work_order_id: "artifact-wo-pricing-fix",
    },
    provenance: { sources: ["actor_statement:evt-price-006"] },
  },
  {
    id: "evt-price-007",
    ts: new Date(
      now - 9 * 24 * 60 * 60 * 1000 + 1 * 60 * 60 * 1000,
    ).toISOString(),
    type: "review_completed",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "artifact:artifact-review-pricing-escalate",
      "artifact:artifact-receipt-pricing-v1",
      "artifact:artifact-wo-pricing-fix",
    ],
    summary:
      "Review completed (escalate): refund policy decision required before acceptance.",
    payload: {
      artifact_id: "artifact-review-pricing-escalate",
      receipt_id: "artifact-receipt-pricing-v1",
      work_order_id: "artifact-wo-pricing-fix",
      outcome: "escalate",
    },
    provenance: { sources: ["actor_statement:evt-price-007"] },
  },
  {
    id: "evt-price-008",
    ts: new Date(
      now - 9 * 24 * 60 * 60 * 1000 + 2 * 60 * 60 * 1000,
    ).toISOString(),
    type: "decision_made",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "artifact:artifact-pricing-evidence",
      "snapshot:commitment-pricing-patch",
    ],
    summary: "Decision made: issue refunds and proceed with cache fix.",
    payload: {
      decision:
        "Issuing $0.50 refunds to all 3 affected customers via payment processor bot. " +
        "Config pushes suspended until cache invalidation patch is deployed. " +
        "Till-E to file refund receipts. SqueezeBot pricing logic patch to proceed.",
    },
    provenance: { sources: ["actor_statement:evt-price-008"] },
  },
  {
    id: "evt-price-009",
    ts: new Date(now - 8 * 24 * 60 * 60 * 1000).toISOString(),
    type: "receipt_added",
    actor_id: "actor-cashier-bot",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "artifact:artifact-receipt-pricing-v2",
      "artifact:artifact-wo-pricing-fix",
    ],
    summary:
      "Receipt added (v2): cache fix deployed, refunds confirmed, patch validated.",
    payload: {
      artifact_id: "artifact-receipt-pricing-v2",
      work_order_id: "artifact-wo-pricing-fix",
    },
    provenance: { sources: ["actor_statement:evt-price-009"] },
  },
  {
    id: "evt-price-010",
    ts: new Date(
      now - 8 * 24 * 60 * 60 * 1000 + 1 * 60 * 60 * 1000,
    ).toISOString(),
    type: "review_completed",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "artifact:artifact-review-pricing-accept",
      "artifact:artifact-receipt-pricing-v2",
      "artifact:artifact-wo-pricing-fix",
    ],
    summary:
      "Review completed (accept): pricing fix accepted, incident ready to close.",
    payload: {
      artifact_id: "artifact-review-pricing-accept",
      receipt_id: "artifact-receipt-pricing-v2",
      work_order_id: "artifact-wo-pricing-fix",
      outcome: "accept",
    },
    provenance: { sources: ["actor_statement:evt-price-010"] },
  },
  {
    id: "evt-price-011",
    ts: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
    type: "commitment_status_changed",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "snapshot:commitment-pricing-patch",
      "artifact:artifact-receipt-pricing-v2",
    ],
    summary:
      "Commitment marked done: pricing cache fix deployed and validated.",
    payload: {
      commitment_id: "commitment-pricing-patch",
      from_status: "open",
      to_status: "done",
    },
    provenance: { sources: ["actor_statement:evt-price-011"] },
  },
  {
    id: "evt-price-012",
    ts: new Date(now - 7 * 24 * 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
    type: "commitment_status_changed",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "snapshot:commitment-pricing-audit",
      "event:evt-price-008",
    ],
    summary:
      "Commitment canceled: full pricing audit deemed unnecessary after root cause confirmed.",
    payload: {
      commitment_id: "commitment-pricing-audit",
      from_status: "open",
      to_status: "canceled",
      reason:
        "Root cause confirmed as a single stale cache entry from March 3rd menu push. " +
        "A full historical audit is not warranted. Decision made per evt-price-008.",
    },
    provenance: { sources: ["actor_statement:evt-price-008"] },
  },
  {
    id: "evt-price-013",
    ts: new Date(
      now - 7 * 24 * 60 * 60 * 1000 + 1 * 60 * 60 * 1000,
    ).toISOString(),
    type: "snapshot_updated",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: ["thread:thread-pricing-glitch"],
    summary: "Thread closed — incident fully resolved.",
    payload: { changed_fields: ["status", "current_summary", "next_actions"] },
    provenance: { sources: ["actor_statement:evt-price-013"] },
  },

  // ── Q2 initiative thread ──────────────────────────────────────────────────
  {
    id: "evt-q2-001",
    ts: new Date(now - 14 * 24 * 60 * 60 * 1000).toISOString(),
    type: "message_posted",
    actor_id: "actor-ops-ai",
    thread_id: "thread-q2-initiative",
    refs: ["thread:thread-q2-initiative"],
    summary:
      "OpsAI opened Q2 expansion initiative for Stand #2 at Riverside Park.",
    payload: {
      text:
        "Opening this initiative thread for the Q2 goal: Stand #2 at Riverside Park by June 1. " +
        "Site survey is done — the corner spot near the main fountain is approved. " +
        "City permit application filed March 1 (reference: PERMIT-2026-0882). " +
        "SqueezeBot 2000 unit ordered from RoboSupply Inc. (order RS-20260301-0019, ETA: March 20). " +
        "Monthly check-ins until launch. @FlavorMind — start scoping a riverside seasonal menu. " +
        "@SupplyRover — add Stand #2 as a provisioning location once the permit clears.",
    },
    provenance: { sources: ["actor_statement:evt-q2-001"] },
  },
  {
    id: "evt-q2-002",
    ts: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
    type: "snapshot_updated",
    actor_id: "actor-ops-ai",
    thread_id: "thread-q2-initiative",
    refs: ["thread:thread-q2-initiative"],
    summary:
      "Monthly check-in: permit in review, SqueezeBot 2000 delivery on track.",
    payload: { changed_fields: ["current_summary", "next_actions"] },
    provenance: { sources: ["actor_statement:evt-q2-002"] },
  },
  {
    id: "evt-q2-commitment-permit",
    ts: new Date(now - 14 * 24 * 60 * 60 * 1000 + 15 * 60 * 1000).toISOString(),
    type: "commitment_created",
    actor_id: "actor-ops-ai",
    thread_id: "thread-q2-initiative",
    refs: ["thread:thread-q2-initiative", "snapshot:commitment-q2-permit"],
    summary: "Commitment created: monitor city permit and confirm approval.",
    payload: { commitment_id: "commitment-q2-permit" },
    provenance: { sources: ["actor_statement:evt-q2-001"] },
  },
  {
    id: "evt-q2-commitment-menu",
    ts: new Date(now - 14 * 24 * 60 * 60 * 1000 + 20 * 60 * 1000).toISOString(),
    type: "commitment_created",
    actor_id: "actor-ops-ai",
    thread_id: "thread-q2-initiative",
    refs: ["thread:thread-q2-initiative", "snapshot:commitment-q2-menu"],
    summary: "Commitment created: FlavorMind to draft Riverside seasonal menu.",
    payload: { commitment_id: "commitment-q2-menu" },
    provenance: { sources: ["actor_statement:evt-q2-001"] },
  },
];

const commitments = [
  {
    id: "commitment-emergency-restock",
    thread_id: "thread-lemon-shortage",
    title: "Place emergency lemon restock order with approved backup supplier",
    owner: "actor-supply-rover",
    due_at: new Date(now + 2 * 60 * 60 * 1000).toISOString(),
    status: "open",
    definition_of_done: [
      "OpsAI approves supplier selection (LocalGrove Bot recommended)",
      "100-unit purchase order placed via supplier API",
      "Delivery confirmation received with ETA",
    ],
    links: ["thread:thread-lemon-shortage", "artifact:artifact-supplier-sla"],
    updated_at: new Date(now - 1 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-supply-rover",
    provenance: { sources: ["actor_statement:evt-supply-001"] },
  },
  {
    id: "commitment-sla-review",
    thread_id: "thread-lemon-shortage",
    title: "File SLA breach report with CitrusBot Farm for today's outage",
    owner: "actor-ops-ai",
    due_at: new Date(now + 5 * 24 * 60 * 60 * 1000).toISOString(),
    status: "open",
    definition_of_done: [
      "Outage timeline documented (start, Tier 1 breach confirmation, resolution)",
      "SLA breach formally submitted to CitrusBot Farm API",
      "Credit or remediation plan response received and logged",
    ],
    links: ["thread:thread-lemon-shortage", "artifact:artifact-supplier-sla"],
    updated_at: new Date(now - 14 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-supply-003"] },
  },
  {
    id: "commitment-menu-board",
    thread_id: "thread-summer-menu",
    title: "Update stand menu board and POS with summer flavors",
    owner: "actor-cashier-bot",
    due_at: new Date(now + 10 * 24 * 60 * 60 * 1000).toISOString(),
    status: "open",
    definition_of_done: [
      "Lavender Lemonade and Mango Chili Lemonade added to POS system",
      "Digital menu board display updated at Stand #1",
      "Prices and descriptions confirmed accurate",
    ],
    links: ["thread:thread-summer-menu", "artifact:artifact-summer-menu-draft"],
    updated_at: new Date(now - 3 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-flavor-ai",
    provenance: { sources: ["actor_statement:evt-menu-003"] },
  },
  {
    id: "commitment-part-install",
    thread_id: "thread-squeezebot-maintenance",
    title:
      "Install torque limiter #TL-3000-L and run post-repair QA validation",
    owner: "actor-squeeze-bot",
    due_at: new Date(
      now + 1 * 24 * 60 * 60 * 1000 + 3 * 60 * 60 * 1000,
    ).toISOString(),
    status: "blocked",
    definition_of_done: [
      "Part #TL-3000-L received from RoboSupply Inc.",
      "Part installed per maintenance spec",
      "Post-repair calibration sequence completed",
      "FlavorMind QA scan confirms seed contamination rate <1%",
    ],
    links: [
      "thread:thread-squeezebot-maintenance",
      "artifact:artifact-maintenance-log",
    ],
    updated_at: new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
    provenance: {
      sources: ["inferred"],
      by_field: {
        status: ["inferred"],
      },
    },
  },
  {
    id: "commitment-pricing-patch",
    thread_id: "thread-pricing-glitch",
    title: "Patch pricing cache invalidation logic in Till-E POS",
    owner: "actor-cashier-bot",
    due_at: new Date(now - 8 * 24 * 60 * 60 * 1000).toISOString(),
    status: "done",
    definition_of_done: [
      "Root cause of stale price cache identified",
      "Cache invalidation fix deployed to Till-E POS system",
      "Pricing validated correct on 10 consecutive test transactions",
      "Customer refunds confirmed by payment processor bot",
    ],
    links: [
      "thread:thread-pricing-glitch",
      "artifact:artifact-receipt-pricing-v2",
    ],
    updated_at: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
    provenance: {
      sources: ["actor_statement:evt-price-011"],
      by_field: {
        status: ["artifact:artifact-receipt-pricing-v2"],
      },
    },
  },
  {
    id: "commitment-pricing-audit",
    thread_id: "thread-pricing-glitch",
    title: "Full historical pricing audit for March (canceled)",
    owner: "actor-ops-ai",
    due_at: new Date(now - 3 * 24 * 60 * 60 * 1000).toISOString(),
    status: "canceled",
    definition_of_done: [
      "All transactions in March audited for pricing accuracy",
      "Audit report filed as artifact",
    ],
    links: ["thread:thread-pricing-glitch"],
    updated_at: new Date(
      now - 7 * 24 * 60 * 60 * 1000 + 30 * 60 * 1000,
    ).toISOString(),
    updated_by: "actor-ops-ai",
    provenance: {
      sources: ["actor_statement:evt-price-008"],
      by_field: {
        status: ["event:evt-price-008"],
      },
    },
  },
  {
    id: "commitment-q2-permit",
    thread_id: "thread-q2-initiative",
    title: "Confirm city permit approval for Riverside Park Stand #2",
    owner: "actor-ops-ai",
    due_at: new Date(now + 40 * 24 * 60 * 60 * 1000).toISOString(),
    status: "open",
    definition_of_done: [
      "City permit PERMIT-2026-0882 approved",
      "Permit document filed as artifact in this thread",
      "SupplyRover notified to add Stand #2 as provisioning location",
    ],
    links: ["thread:thread-q2-initiative"],
    updated_at: new Date(now - 14 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-q2-001"] },
  },
  {
    id: "commitment-q2-menu",
    thread_id: "thread-q2-initiative",
    title: "FlavorMind to draft Riverside Park seasonal menu by April 1",
    owner: "actor-flavor-ai",
    due_at: new Date(now + 27 * 24 * 60 * 60 * 1000).toISOString(),
    status: "open",
    definition_of_done: [
      "Seasonal menu draft covers at least 4 items",
      "At least one item uses locally-sourced ingredient (farmer's market proximity)",
      "Draft reviewed and approved by OpsAI",
    ],
    links: ["thread:thread-q2-initiative"],
    updated_at: new Date(now - 14 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-q2-001"] },
  },
];

const artifacts = [
  {
    id: "artifact-supplier-sla-v2",
    kind: "doc",
    thread_id: "thread-lemon-shortage",
    summary: "CitrusBot Farm SLA — uptime and delivery commitments",
    refs: ["thread:thread-lemon-shortage", "artifact:artifact-supplier-sla"],
    content_type: "text/markdown",
    content_text: `# CitrusBot Farm Supplier SLA (Amended)

**Supplier:** CitrusBot Farm (API: api.citrusbotfarm.io)
**Contract term:** 2026-01-01 to 2026-12-31
**Account:** Zesty Bots Lemonade Co.
**Amendment:** Emergency response SLA tightened following March breach.

---

## Uptime Commitment
- 99.5% monthly uptime on procurement API
- Maximum **2-hour** outage response time (reduced from 4h after breach)

## Delivery Commitments
- Standard orders: fulfilled within 24 hours of confirmation
- Emergency orders (priority flag): fulfilled within 4 hours
- Minimum order: 20 lemons | Maximum single order: 500 lemons

## Pricing
- Standard rate: $0.20/lemon
- Emergency surcharge: +$0.08/lemon for same-day fulfillment

## SLA Breach Conditions
- **Tier 1:** API downtime >2 hours in any rolling 24-hour window (amended)
- **Tier 2:** Delivery miss >2 hours past confirmed delivery window
- Credits issued per clause 4.2 (Tier 1: $12.00 flat; Tier 2: $6.00 flat)

## Current Status
- ✅ API restored. Amendment accepted by CitrusBot Farm.`,
    created_at: new Date(now - 10 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-supply-001"] },
    tombstoned_at: null,
  },
  {
    id: "artifact-supplier-sla",
    kind: "doc",
    thread_id: "thread-lemon-shortage",
    summary: "CitrusBot Farm SLA — uptime and delivery commitments",
    refs: ["thread:thread-lemon-shortage"],
    content_type: "text/markdown",
    content_text: `# CitrusBot Farm Supplier SLA

**Supplier:** CitrusBot Farm (API: api.citrusbotfarm.io)
**Contract term:** 2026-01-01 to 2026-12-31
**Account:** Zesty Bots Lemonade Co.

---

## Uptime Commitment
- 99.5% monthly uptime on procurement API
- Maximum 4-hour outage response time (acknowledgement)

## Delivery Commitments
- Standard orders: fulfilled within 24 hours of confirmation
- Emergency orders (priority flag): fulfilled within 4 hours
- Minimum order: 20 lemons | Maximum single order: 500 lemons

## Pricing
- Standard rate: $0.20/lemon
- Emergency surcharge: +$0.08/lemon for same-day fulfillment

## SLA Breach Conditions
- **Tier 1:** API downtime >4 hours in any rolling 24-hour window
- **Tier 2:** Delivery miss >2 hours past confirmed delivery window
- Credits issued per clause 4.2 (Tier 1: $12.00 flat; Tier 2: $6.00 flat)

## Current Status
- ⚠️ **TIER 1 BREACH IN PROGRESS**
- API offline since: ${new Date(now - 18 * 60 * 60 * 1000).toISOString()}
- Breach confirmed at: ${new Date(now - 14 * 60 * 60 * 1000).toISOString()}
- Credit owed: $12.00 (per clause 4.2)
- SLA breach report: pending (assigned OpsAI)`,
    created_at: new Date(now - 20 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-supply-001"] },
    tombstoned_at: null,
  },
  {
    id: "artifact-summer-menu-draft",
    kind: "doc",
    thread_id: "thread-summer-menu",
    summary:
      "Summer menu proposal — Lavender & Mango Chili Lemonade recipe specs",
    refs: ["thread:thread-summer-menu"],
    content_type: "text/markdown",
    content_text: `# Summer Flavor Expansion — Recipe Spec v1.2

*Authored by FlavorMind | QA validated by SqueezeBot 3000*

---

## 1. Lavender Lemonade

**Base:** Classic Lemonade (60ml fresh lemon juice, 20ml simple syrup, 180ml cold water)
**Add:** 15ml culinary lavender syrup
**Garnish:** Dried lavender sprig + lemon wheel
**Serve:** Over ice, 12oz cup
**QA Score:** 9.1/10 (aroma 9.4 · sweetness 9.0 · acidity 9.0)
**Retail price:** $4.50

### Sourcing
- Lavender syrup: BotBotanicals API — food-grade, $8.40/L, 2-day delivery ✅ Contracted
- Estimated COGS: $0.85/cup → gross margin 81%

---

## 2. Mango Chili Lemonade

**Base:** Classic Lemonade
**Add:** 30ml Alphonso mango purée
**Rim:** Chili-salt blend (2:1 tajín : sea salt)
**Garnish:** Mango slice
**Serve:** Over ice, 12oz cup
**QA Score:** 9.3/10 (heat balance 9.5 · flavor complexity 9.2)
**Retail price:** $4.75

### Sourcing
- Mango purée: FruitBot API — in stock ✅
- Chili-salt blend: In stock (250g on hand) ✅
- Estimated COGS: $0.92/cup → gross margin 81%

---

## Launch Blockers

1. 🔴 Lemon supply crisis must resolve before pilot batch (see thread-lemon-shortage)
2. 🟡 Menu board update pending (Till-E — commitment-menu-board)
3. 🟢 Lavender syrup: contracted and on order`,
    created_at: new Date(now - 5 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-flavor-ai",
    provenance: { sources: ["actor_statement:evt-menu-001"] },
    tombstoned_at: null,
  },
  {
    id: "artifact-tasting-log",
    kind: "log",
    thread_id: "thread-summer-menu",
    summary: "SqueezeBot QA sensor log — summer flavor test batches",
    refs: ["thread:thread-summer-menu", "artifact:artifact-summer-menu-draft"],
    content_type: "text/plain",
    content_text: [
      `${new Date(now - 4 * 24 * 60 * 60 * 1000).toISOString()} [SqueezeBot 3000] Starting test batch: Lavender Lemonade v1.2 (2-cup run)`,
      `${new Date(now - 4 * 24 * 60 * 60 * 1000 + 2 * 60 * 1000).toISOString()} [SqueezeBot 3000] Squeeze cycle: 2 lemons, yield 118ml (within ±5% spec) OK`,
      `${new Date(now - 4 * 24 * 60 * 60 * 1000 + 4 * 60 * 1000).toISOString()} [SqueezeBot 3000] Lavender syrup added: 30ml total. Mix complete.`,
      `${new Date(now - 4 * 24 * 60 * 60 * 1000 + 5 * 60 * 1000).toISOString()} [QA Sensor Array] Sweetness: 9.0 | Acidity: 9.0 | Aroma: 9.4 → PASS`,
      `${new Date(now - 4 * 24 * 60 * 60 * 1000 + 6 * 60 * 1000).toISOString()} [QA Sensor Array] Seed contamination scan: 0 seeds detected → PASS`,
      `${new Date(now - 4 * 24 * 60 * 60 * 1000 + 7 * 60 * 1000).toISOString()} [SqueezeBot 3000] Lavender Lemonade v1.2 — APPROVED FOR PRODUCTION`,
      `${new Date(now - 4 * 24 * 60 * 60 * 1000 + 10 * 60 * 1000).toISOString()} [SqueezeBot 3000] Starting test batch: Mango Chili Lemonade v1.2 (2-cup run)`,
      `${new Date(now - 4 * 24 * 60 * 60 * 1000 + 14 * 60 * 1000).toISOString()} [SqueezeBot 3000] Mix complete. Mango purée: 60ml. Chili-salt rim applied.`,
      `${new Date(now - 4 * 24 * 60 * 60 * 1000 + 15 * 60 * 1000).toISOString()} [QA Sensor Array] Heat balance: 9.5 | Flavor complexity: 9.2 | Sweetness: 9.1 → PASS`,
      `${new Date(now - 4 * 24 * 60 * 60 * 1000 + 16 * 60 * 1000).toISOString()} [QA Sensor Array] Seed contamination scan: 0 seeds detected → PASS`,
      `${new Date(now - 4 * 24 * 60 * 60 * 1000 + 17 * 60 * 1000).toISOString()} [SqueezeBot 3000] Mango Chili Lemonade v1.2 — APPROVED FOR PRODUCTION`,
    ].join("\n"),
    created_at: new Date(now - 4 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-squeeze-bot",
    provenance: { sources: ["actor_statement:evt-menu-002"] },
    tombstoned_at: null,
  },
  {
    id: "artifact-maintenance-log",
    kind: "log",
    thread_id: "thread-squeezebot-maintenance",
    summary: "SqueezeBot 3000 self-diagnostic and maintenance event log",
    refs: [
      "thread:thread-squeezebot-maintenance",
      "url:https://robosupply.example.com/orders/RS-20260305-4421",
    ],
    content_type: "text/plain",
    content_text: [
      `${new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString()} [SqueezeBot 3000] Scheduled self-diagnostic initiated.`,
      `${new Date(now - 2 * 24 * 60 * 60 * 1000 + 3 * 60 * 1000).toISOString()} [Diagnostics] Right arm torque sensor: 100% of nominal → OK`,
      `${new Date(now - 2 * 24 * 60 * 60 * 1000 + 4 * 60 * 1000).toISOString()} [Diagnostics] Left arm torque sensor: 112% of nominal → OVER SPEC (threshold: 100%)`,
      `${new Date(now - 2 * 24 * 60 * 60 * 1000 + 5 * 60 * 1000).toISOString()} [Diagnostics] QA impact simulation: seed bypass ~14% per cycle (acceptable threshold: <5%) → FAIL`,
      `${new Date(now - 2 * 24 * 60 * 60 * 1000 + 6 * 60 * 1000).toISOString()} [SqueezeBot 3000] Issue flagged. Notifying OpsAI. Throttling left arm to 80% duty cycle.`,
      `${new Date(now - 2 * 24 * 60 * 60 * 1000 + 15 * 60 * 1000).toISOString()} [OpsAI] Maintenance work order issued. Ordering part #TL-3000-L from RoboSupply Inc.`,
      `${new Date(now - 2 * 24 * 60 * 60 * 1000 + 18 * 60 * 1000).toISOString()} [RoboSupply Inc.] Order confirmed. Order ID: RS-20260305-4421. Estimated delivery: +24h.`,
      `${new Date(now - 2 * 24 * 60 * 60 * 1000 + 19 * 60 * 1000).toISOString()} [SqueezeBot 3000] Running in degraded mode. Left arm at 80% duty cycle. Throughput -20%.`,
    ].join("\n"),
    created_at: new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-squeeze-bot",
    provenance: { sources: ["actor_statement:evt-maint-001"] },
    tombstoned_at: null,
  },
  {
    id: "artifact-wo-lavender-sourcing",
    kind: "work_order",
    thread_id: "thread-summer-menu",
    summary:
      "Work order: Source and contract a food-grade lavender syrup supplier",
    refs: ["thread:thread-summer-menu", "artifact:artifact-summer-menu-draft"],
    created_at: new Date(now - 3 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-menu-003"] },
    packet: {
      work_order_id: "artifact-wo-lavender-sourcing",
      thread_id: "thread-summer-menu",
      objective:
        "Identify and contract a food-grade culinary lavender syrup supplier to support " +
        "the Lavender Lemonade product line at Zesty Bots Lemonade Co.",
      constraints: [
        "Supplier must carry food-grade culinary certification",
        "Pricing must preserve ≥75% gross margin at $4.50 retail (COGS cap: $1.13/cup)",
        "Delivery lead time must be ≤5 business days for initial order",
        "Minimum order quantity must be ≤2L",
      ],
      context_refs: [
        "thread:thread-summer-menu",
        "artifact:artifact-summer-menu-draft",
      ],
      acceptance_criteria: [
        "At least 2 suppliers evaluated with pricing, lead time, and certification data",
        "Preferred supplier selection approved by OpsAI",
        "Initial 2L order placed and purchase confirmation received",
      ],
      definition_of_done: [
        "Supplier comparison summary linked in thread",
        "Purchase order receipt attached as artifact",
        "Lavender syrup added to SupplyRover inventory system",
      ],
    },
    tombstoned_at: null,
  },
  {
    id: "artifact-receipt-lavender-sourcing",
    kind: "receipt",
    thread_id: "thread-summer-menu",
    summary: "Receipt: Lavender syrup sourced — BotBotanicals API, 2L ordered",
    refs: [
      "thread:thread-summer-menu",
      "artifact:artifact-wo-lavender-sourcing",
    ],
    created_at: new Date(
      now - 2 * 24 * 60 * 60 * 1000 + 2 * 60 * 60 * 1000,
    ).toISOString(),
    created_by: "actor-flavor-ai",
    provenance: { sources: ["actor_statement:evt-menu-003"] },
    packet: {
      receipt_id: "artifact-receipt-lavender-sourcing",
      work_order_id: "artifact-wo-lavender-sourcing",
      thread_id: "thread-summer-menu",
      outputs: ["artifact:artifact-summer-menu-draft"],
      verification_evidence: ["event:evt-menu-004"],
      changes_summary:
        "Evaluated two suppliers: BotBotanicals API ($8.40/L, food-grade certified, " +
        "2-day delivery, 1L minimum) and SyrupBot Co. ($11.20/L, food-grade, 1-day delivery). " +
        "BotBotanicals selected on price — COGS confirmed within margin spec. " +
        "2L initial order placed via BotBotanicals API. Purchase confirmation received.",
      known_gaps: [
        "BotBotanicals does not yet support automated reorder webhooks — manual reorder " +
          "required until their API v2 ships in Q3 2026.",
      ],
    },
    tombstoned_at: null,
  },
  {
    id: "artifact-review-lavender-sourcing",
    kind: "review",
    thread_id: "thread-summer-menu",
    summary: "Review: Lavender sourcing receipt — accepted with minor note",
    refs: [
      "thread:thread-summer-menu",
      "artifact:artifact-receipt-lavender-sourcing",
      "artifact:artifact-wo-lavender-sourcing",
    ],
    created_at: new Date(
      now - 2 * 24 * 60 * 60 * 1000 + 3 * 60 * 60 * 1000,
    ).toISOString(),
    created_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-menu-003"] },
    packet: {
      review_id: "artifact-review-lavender-sourcing",
      work_order_id: "artifact-wo-lavender-sourcing",
      receipt_id: "artifact-receipt-lavender-sourcing",
      outcome: "accept",
      notes:
        "BotBotanicals pricing checks out — margin target preserved at 81%. " +
        "Two suppliers evaluated as required. Manual reorder gap is acceptable for now; " +
        "flag for Q3 automation sprint. Sourcing commitment can close once delivery is confirmed " +
        "by SupplyRover and inventory is updated.",
      evidence_refs: ["artifact:artifact-summer-menu-draft"],
    },
    tombstoned_at: null,
  },

  // ── Pricing glitch artifacts ───────────────────────────────────────────────
  {
    id: "artifact-pricing-evidence",
    kind: "evidence",
    thread_id: "thread-pricing-glitch",
    summary: "Raw POS transaction log showing overcharged transactions",
    refs: [
      "thread:thread-pricing-glitch",
      "url:https://pos.zestybots.example.com/logs/2026-03-03",
    ],
    content_type: "text/plain",
    content_text: [
      `${new Date(now - 10 * 24 * 60 * 60 * 1000 - 7 * 60 * 60 * 1000).toISOString()} [Till-E POS] TXN#4821 — 1× Classic Lemonade — charged: $4.00 — config_price_version: v1.2 — ANOMALY (current: v1.3, price: $3.50)`,
      `${new Date(now - 10 * 24 * 60 * 60 * 1000 - 6 * 60 * 60 * 1000).toISOString()} [Till-E POS] TXN#4822 — 1× Classic Lemonade — charged: $4.00 — config_price_version: v1.2 — ANOMALY (current: v1.3, price: $3.50)`,
      `${new Date(now - 10 * 24 * 60 * 60 * 1000 - 5 * 60 * 60 * 1000).toISOString()} [Till-E POS] TXN#4830 — 1× Classic Lemonade — charged: $4.00 — config_price_version: v1.2 — ANOMALY (current: v1.3, price: $3.50)`,
      `${new Date(now - 10 * 24 * 60 * 60 * 1000 - 4 * 60 * 60 * 1000).toISOString()} [Till-E POS] Config cache diagnostics: last_invalidated=2026-02-28T09:00:00Z, current_version=v1.2, latest_version=v1.3 — STALE CACHE CONFIRMED`,
      `${new Date(now - 10 * 24 * 60 * 60 * 1000 - 4 * 60 * 60 * 1000 + 2 * 60 * 1000).toISOString()} [Till-E POS] Self-diagnostic: cache TTL set to 7 days, menu config pushed 2026-03-01 but TTL not reset. Root cause identified.`,
      `${new Date(now - 8 * 24 * 60 * 60 * 1000).toISOString()} [Till-E POS] Cache invalidation patch deployed. Config version: v1.3. Cache TTL reset to 1 hour.`,
      `${new Date(now - 8 * 24 * 60 * 60 * 1000 + 5 * 60 * 1000).toISOString()} [Payment Processor Bot] Refund issued: TXN#4821 — $0.50 → customer confirmed`,
      `${new Date(now - 8 * 24 * 60 * 60 * 1000 + 6 * 60 * 1000).toISOString()} [Payment Processor Bot] Refund issued: TXN#4822 — $0.50 → customer confirmed`,
      `${new Date(now - 8 * 24 * 60 * 60 * 1000 + 7 * 60 * 1000).toISOString()} [Payment Processor Bot] Refund issued: TXN#4830 — $0.50 → customer confirmed`,
      `${new Date(now - 8 * 24 * 60 * 60 * 1000 + 10 * 60 * 1000).toISOString()} [Till-E POS] Post-patch validation: 10 test transactions at $3.50 — all correct. PASS`,
    ].join("\n"),
    created_at: new Date(now - 10 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-cashier-bot",
    provenance: { sources: ["actor_statement:evt-price-001"] },
    tombstoned_at: null,
  },
  {
    id: "artifact-wo-pricing-fix",
    kind: "work_order",
    thread_id: "thread-pricing-glitch",
    summary:
      "Work order: diagnose pricing anomaly and patch cache invalidation logic",
    refs: [
      "thread:thread-pricing-glitch",
      "artifact:artifact-pricing-evidence",
    ],
    created_at: new Date(
      now - 10 * 24 * 60 * 60 * 1000 + 2 * 60 * 60 * 1000,
    ).toISOString(),
    created_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-price-004"] },
    packet: {
      work_order_id: "artifact-wo-pricing-fix",
      thread_id: "thread-pricing-glitch",
      objective:
        "Diagnose root cause of the pricing overcharge on March 3rd, deploy a fix to " +
        "Till-E's price cache invalidation logic, validate correct pricing, and confirm " +
        "customer refunds were issued.",
      constraints: [
        "Fix must not require Till-E downtime >5 minutes",
        "Refund decision requires OpsAI approval before execution",
        "All changes must be logged in the POS audit trail",
      ],
      context_refs: [
        "thread:thread-pricing-glitch",
        "artifact:artifact-pricing-evidence",
      ],
      acceptance_criteria: [
        "Root cause documented with evidence",
        "Cache invalidation fix deployed and config version confirmed current",
        "10 consecutive post-patch test transactions show correct pricing",
        "All 3 customer refunds confirmed by payment processor bot",
      ],
      definition_of_done: [
        "Fix deployed and validated",
        "Refund confirmations attached as evidence",
        "POS audit log updated",
        "Receipt filed against this work order",
      ],
    },
    tombstoned_at: null,
  },
  {
    id: "artifact-receipt-pricing-v1",
    kind: "receipt",
    thread_id: "thread-pricing-glitch",
    summary:
      "Receipt v1: root cause identified — awaiting refund decision before closing",
    refs: ["thread:thread-pricing-glitch", "artifact:artifact-wo-pricing-fix"],
    created_at: new Date(now - 9 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-cashier-bot",
    provenance: { sources: ["actor_statement:evt-price-006"] },
    tombstoned_at: null,
    packet: {
      receipt_id: "artifact-receipt-pricing-v1",
      work_order_id: "artifact-wo-pricing-fix",
      thread_id: "thread-pricing-glitch",
      outputs: ["artifact:artifact-pricing-evidence"],
      verification_evidence: ["event:evt-price-001"],
      changes_summary:
        "Root cause confirmed: Till-E's price cache TTL was set to 7 days and was not " +
        "reset when the menu config was pushed on March 1st. Transactions on March 3rd " +
        "used the stale v1.2 price of $4.00 instead of the correct v1.3 price of $3.50. " +
        "Fix is ready to deploy — awaiting OpsAI decision on customer refunds before proceeding.",
      known_gaps: [
        "Refund policy decision not yet made — receipt cannot be finalized until approved",
        "Cache fix not yet deployed — pending decision to resume config pushes",
      ],
    },
  },
  {
    id: "artifact-review-pricing-escalate",
    kind: "review",
    thread_id: "thread-pricing-glitch",
    summary:
      "Review v1 (escalate): refund decision required before work order can be accepted",
    refs: [
      "thread:thread-pricing-glitch",
      "artifact:artifact-receipt-pricing-v1",
      "artifact:artifact-wo-pricing-fix",
    ],
    created_at: new Date(
      now - 9 * 24 * 60 * 60 * 1000 + 1 * 60 * 60 * 1000,
    ).toISOString(),
    created_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-price-007"] },
    tombstoned_at: null,
    packet: {
      review_id: "artifact-review-pricing-escalate",
      work_order_id: "artifact-wo-pricing-fix",
      receipt_id: "artifact-receipt-pricing-v1",
      outcome: "escalate",
      notes:
        "Root cause analysis is solid and the fix approach looks correct. However, the receipt " +
        "cannot be accepted while the refund decision is unresolved — the work order's " +
        "acceptance criteria explicitly requires confirmed customer refunds. " +
        "Escalating: OpsAI must make a formal decision on the refund policy (evt-price-003) " +
        "before this receipt can be finalized. Once decided, resubmit with refund confirmation evidence.",
      evidence_refs: ["artifact:artifact-pricing-evidence"],
    },
  },
  {
    id: "artifact-receipt-pricing-v2",
    kind: "receipt",
    thread_id: "thread-pricing-glitch",
    summary:
      "Receipt v2: fix deployed, refunds confirmed, all acceptance criteria met",
    refs: ["thread:thread-pricing-glitch", "artifact:artifact-wo-pricing-fix"],
    created_at: new Date(now - 8 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-cashier-bot",
    provenance: { sources: ["actor_statement:evt-price-009"] },
    tombstoned_at: null,
    packet: {
      receipt_id: "artifact-receipt-pricing-v2",
      work_order_id: "artifact-wo-pricing-fix",
      thread_id: "thread-pricing-glitch",
      outputs: ["artifact:artifact-pricing-evidence"],
      verification_evidence: [
        "event:evt-price-008",
        "artifact:artifact-pricing-evidence",
      ],
      changes_summary:
        "Following OpsAI's decision (evt-price-008): cache invalidation patch deployed — " +
        "config version advanced to v1.3, cache TTL reduced to 1 hour. " +
        "Post-patch validation: 10 consecutive transactions at correct price ($3.50) — all passed. " +
        "Refunds issued: $0.50 each to TXN#4821, #4822, #4830 — all confirmed by payment processor bot. " +
        "POS audit log updated. Config push suspension lifted.",
      known_gaps: [],
    },
  },
  {
    id: "artifact-review-pricing-accept",
    kind: "review",
    thread_id: "thread-pricing-glitch",
    summary: "Review v2 (accept): pricing fix complete, incident closed",
    refs: [
      "thread:thread-pricing-glitch",
      "artifact:artifact-receipt-pricing-v2",
      "artifact:artifact-wo-pricing-fix",
    ],
    created_at: new Date(
      now - 8 * 24 * 60 * 60 * 1000 + 1 * 60 * 60 * 1000,
    ).toISOString(),
    created_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-price-010"] },
    packet: {
      review_id: "artifact-review-pricing-accept",
      work_order_id: "artifact-wo-pricing-fix",
      receipt_id: "artifact-receipt-pricing-v2",
      outcome: "accept",
      notes:
        "All acceptance criteria met: root cause documented, fix deployed and validated on " +
        "10 test transactions, all 3 customer refunds confirmed. The cache TTL reduction from " +
        "7 days to 1 hour is a good systemic improvement — this won't recur on future config pushes. " +
        "Commitment can be marked done. Thread ready to close.",
      evidence_refs: [
        "artifact:artifact-pricing-evidence",
        "artifact:artifact-receipt-pricing-v2",
      ],
    },
    tombstoned_at: null,
  },
  {
    id: "artifact-tombstoned-doc",
    kind: "doc",
    thread_id: "thread-pricing-glitch",
    summary: "Superseded draft — replaced by final evidence artifact",
    refs: ["thread:thread-pricing-glitch"],
    content_type: "text/plain",
    content_text: "This artifact was superseded and tombstoned.",
    created_at: new Date(now - 11 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-cashier-bot",
    provenance: { sources: ["actor_statement:evt-price-001"] },
    tombstoned_at: new Date(now - 10 * 24 * 60 * 60 * 1000).toISOString(),
    tombstoned_by: "actor-ops-ai",
    tombstone_reason:
      "Superseded by artifact-pricing-evidence; draft no longer needed.",
  },
];

const MOCK_DOCUMENTS = [
  {
    id: "product-constitution",
    title: "Product Constitution",
    slug: "product-constitution",
    status: "active",
    labels: ["governance", "product"],
    supersedes: [],
    head_revision_id: "rev-pc-3",
    head_revision_number: 3,
    thread_id: "thread-governance",
    created_at: "2026-02-15T10:00:00Z",
    created_by: "actor-principal-1",
    updated_at: "2026-03-08T14:30:00Z",
    updated_by: "actor-principal-1",
    tombstoned_at: null,
  },
  {
    id: "incident-response-playbook",
    title: "Incident Response Playbook",
    slug: "incident-response-playbook",
    status: "active",
    labels: ["ops", "runbook"],
    supersedes: [],
    head_revision_id: "rev-irp-2",
    head_revision_number: 2,
    thread_id: "thread-pricing-glitch",
    created_at: "2026-02-20T09:00:00Z",
    created_by: "actor-ops-agent",
    updated_at: "2026-03-05T11:00:00Z",
    updated_by: "actor-ops-agent",
    tombstoned_at: null,
  },
  {
    id: "onboarding-guide-v1",
    title: "Onboarding Guide v1",
    slug: "onboarding-guide-v1",
    status: "active",
    labels: ["onboarding"],
    supersedes: [],
    head_revision_id: "rev-og-1",
    head_revision_number: 1,
    created_at: "2026-01-10T08:00:00Z",
    created_by: "actor-principal-1",
    updated_at: "2026-01-10T08:00:00Z",
    updated_by: "actor-principal-1",
    tombstoned_at: null,
  },
  {
    id: "old-pricing-doc",
    title: "Pricing Strategy (Archived)",
    slug: "old-pricing-doc",
    status: "active",
    labels: ["pricing"],
    supersedes: [],
    head_revision_id: "rev-opd-1",
    head_revision_number: 1,
    created_at: "2025-12-01T08:00:00Z",
    created_by: "actor-principal-1",
    updated_at: "2026-03-01T10:00:00Z",
    updated_by: "actor-principal-1",
    tombstoned_at: "2026-03-01T10:00:00Z",
    tombstoned_by: "actor-principal-1",
    tombstone_reason: "Superseded by updated pricing model",
  },
];

const MOCK_DOCUMENT_REVISIONS = {
  "product-constitution": [
    {
      document_id: "product-constitution",
      revision_id: "rev-pc-1",
      artifact_id: "rev-pc-1",
      revision_number: 1,
      prev_revision_id: null,
      created_at: "2026-02-15T10:00:00Z",
      created_by: "actor-principal-1",
      content_type: "text",
      content_hash: "abc123",
      revision_hash: "def456",
      content:
        "# Product Constitution v1\n\nInitial draft of product governance principles.",
    },
    {
      document_id: "product-constitution",
      revision_id: "rev-pc-2",
      artifact_id: "rev-pc-2",
      revision_number: 2,
      prev_revision_id: "rev-pc-1",
      created_at: "2026-02-28T16:00:00Z",
      created_by: "actor-ops-agent",
      content_type: "text",
      content_hash: "ghi789",
      revision_hash: "jkl012",
      content:
        "# Product Constitution v2\n\nUpdated with team feedback on decision-making framework.\n\n## Principles\n1. User outcomes first\n2. Evidence-based decisions\n3. Transparent trade-offs",
    },
    {
      document_id: "product-constitution",
      revision_id: "rev-pc-3",
      artifact_id: "rev-pc-3",
      revision_number: 3,
      prev_revision_id: "rev-pc-2",
      created_at: "2026-03-08T14:30:00Z",
      created_by: "actor-principal-1",
      content_type: "text",
      content_hash: "mno345",
      revision_hash: "pqr678",
      content:
        "# Product Constitution v3\n\nFinal ratified version with escalation framework.\n\n## Principles\n1. User outcomes first\n2. Evidence-based decisions\n3. Transparent trade-offs\n\n## Escalation\n- P0: Immediate review required\n- P1: Next business day\n- P2: Weekly review cycle",
    },
  ],
  "incident-response-playbook": [
    {
      document_id: "incident-response-playbook",
      revision_id: "rev-irp-1",
      artifact_id: "rev-irp-1",
      revision_number: 1,
      prev_revision_id: null,
      created_at: "2026-02-20T09:00:00Z",
      created_by: "actor-ops-agent",
      content_type: "text",
      content_hash: "stu901",
      revision_hash: "vwx234",
      content:
        "# Incident Response Playbook\n\n## Step 1: Triage\nAssess severity and assign priority.",
    },
    {
      document_id: "incident-response-playbook",
      revision_id: "rev-irp-2",
      artifact_id: "rev-irp-2",
      revision_number: 2,
      prev_revision_id: "rev-irp-1",
      created_at: "2026-03-05T11:00:00Z",
      created_by: "actor-ops-agent",
      content_type: "text",
      content_hash: "yza567",
      revision_hash: "bcd890",
      content:
        "# Incident Response Playbook v2\n\n## Step 1: Triage\nAssess severity and assign priority.\n\n## Step 2: Communicate\nNotify stakeholders within SLA window.\n\n## Step 3: Resolve\nDeploy fix and verify with evidence.",
    },
  ],
  "onboarding-guide-v1": [
    {
      document_id: "onboarding-guide-v1",
      revision_id: "rev-og-1",
      artifact_id: "rev-og-1",
      revision_number: 1,
      prev_revision_id: null,
      created_at: "2026-01-10T08:00:00Z",
      created_by: "actor-principal-1",
      content_type: "text",
      content_hash: "efg123",
      revision_hash: "hij456",
      content:
        "# Onboarding Guide\n\nWelcome to the team! Here's what you need to know.",
    },
  ],
  "old-pricing-doc": [
    {
      document_id: "old-pricing-doc",
      revision_id: "rev-opd-1",
      artifact_id: "rev-opd-1",
      revision_number: 1,
      prev_revision_id: null,
      created_at: "2025-12-01T08:00:00Z",
      created_by: "actor-principal-1",
      content_type: "text",
      content_hash: "klm789",
      revision_hash: "nop012",
      content: "# Old Pricing Strategy\n\nThis document has been superseded.",
    },
  ],
};

export function listMockActors() {
  return actors;
}

function deepClone(value) {
  return JSON.parse(JSON.stringify(value));
}

export function getMockSeedData() {
  return {
    actors: deepClone(actors),
    threads: deepClone(threads),
    commitments: deepClone(commitments),
    artifacts: deepClone(artifacts),
    events: deepClone(events),
  };
}

export function createMockActor(actor) {
  actors.push(actor);
  return actor;
}

export function createMockEvent(event) {
  events.push(event);
  return event;
}

export function listMockInboxItems() {
  return inboxItems.filter((item) => !item.acknowledged_at);
}

export function ackMockInboxItem({ thread_id, inbox_item_id }) {
  const item = inboxItems.find(
    (item) =>
      item.id === inbox_item_id &&
      (!thread_id || String(item.thread_id) === String(thread_id)),
  );

  if (!item) {
    return null;
  }

  if (!item.acknowledged_at) {
    item.acknowledged_at = new Date().toISOString();
  }

  return item;
}

export function listMockTimelineEvents(threadId) {
  return events
    .filter((event) => event.thread_id === threadId)
    .sort((a, b) => String(a.ts).localeCompare(String(b.ts)));
}

function splitTypedRef(refValue) {
  const raw = String(refValue ?? "").trim();
  const separatorIndex = raw.indexOf(":");
  if (separatorIndex <= 0 || separatorIndex >= raw.length - 1) {
    return { prefix: "", id: "" };
  }
  return {
    prefix: raw.slice(0, separatorIndex).trim(),
    id: raw.slice(separatorIndex + 1).trim(),
  };
}

function mockSnapshotByID(snapshotId) {
  const threadSnapshot = threads.find((thread) => thread.id === snapshotId);
  if (threadSnapshot) {
    return {
      ...threadSnapshot,
      kind: "thread",
      thread_id: threadSnapshot.id,
    };
  }

  const commitmentSnapshot = commitments.find(
    (commitment) => commitment.id === snapshotId,
  );
  if (commitmentSnapshot) {
    return {
      ...commitmentSnapshot,
      kind: "commitment",
    };
  }

  return null;
}

function mockArtifactByID(artifactId) {
  const artifact = artifacts.find((item) => item.id === artifactId);
  return artifact ? { ...artifact } : null;
}

function buildMockTimelineExpansions(timelineEvents) {
  const snapshots = {};
  const expandedArtifacts = {};

  for (const event of timelineEvents) {
    for (const ref of normalizeRefList(event?.refs)) {
      const { prefix, id } = splitTypedRef(ref);
      if (!prefix || !id) {
        continue;
      }

      if (prefix === "snapshot") {
        if (!snapshots[id]) {
          const snapshot = mockSnapshotByID(id);
          if (snapshot) {
            snapshots[id] = snapshot;
          }
        }
        continue;
      }

      if (prefix === "artifact" && !expandedArtifacts[id]) {
        const artifact = mockArtifactByID(id);
        if (artifact) {
          expandedArtifacts[id] = artifact;
        }
      }
    }
  }

  return { snapshots, artifacts: expandedArtifacts };
}

export function getMockThreadTimeline(threadId) {
  const threadEvents = listMockTimelineEvents(threadId);
  const expanded = buildMockTimelineExpansions(threadEvents);

  return {
    events: threadEvents,
    snapshots: expanded.snapshots,
    artifacts: expanded.artifacts,
  };
}

function isOpenCommitmentStatus(status) {
  const normalized = String(status ?? "").trim();
  return normalized !== "done" && normalized !== "canceled";
}

function normalizeRefList(value) {
  if (!Array.isArray(value)) {
    return [];
  }

  return value.map((item) => String(item).trim()).filter(Boolean);
}

function isTypedRef(refValue) {
  const input = String(refValue ?? "");
  const separatorIndex = input.indexOf(":");

  if (separatorIndex <= 0) {
    return false;
  }

  return separatorIndex < input.length - 1;
}

function commitmentHasRequiredStatusRef(status, refs) {
  const prefixes = normalizeRefList(refs).map(
    (ref) => String(ref).split(":")[0],
  );

  if (status === "done") {
    return prefixes.includes("artifact") || prefixes.includes("event");
  }

  if (status === "canceled") {
    return prefixes.includes("event");
  }

  return true;
}

function isThreadStale(thread) {
  if (!thread.next_check_in_at) {
    return false;
  }

  return Date.parse(String(thread.next_check_in_at)) < Date.now();
}

function normalizeTagFilters(tag) {
  if (tag === undefined || tag === null || tag === "") {
    return [];
  }

  if (Array.isArray(tag)) {
    return tag.map((value) => String(value));
  }

  return String(tag)
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
}

export function listMockThreads(filters = {}) {
  const tagFilters = normalizeTagFilters(filters.tag);
  const staleFilter =
    filters.stale === undefined ? undefined : String(filters.stale) === "true";

  return threads
    .filter((thread) => {
      if (filters.status && String(thread.status) !== String(filters.status)) {
        return false;
      }

      if (
        filters.priority &&
        String(thread.priority) !== String(filters.priority)
      ) {
        return false;
      }

      if (!cadenceMatchesFilter(thread.cadence, filters.cadence)) {
        return false;
      }

      if (tagFilters.length > 0) {
        const hasTagMatch = tagFilters.every((tag) =>
          thread.tags?.includes(tag),
        );
        if (!hasTagMatch) {
          return false;
        }
      }

      if (staleFilter !== undefined && isThreadStale(thread) !== staleFilter) {
        return false;
      }

      return true;
    })
    .map((thread) => ({
      ...thread,
      stale: isThreadStale(thread),
    }));
}

export function createMockThread({ actor_id, thread }) {
  const created = {
    id: `thread-${Math.random().toString(36).slice(2, 10)}`,
    updated_at: new Date().toISOString(),
    updated_by: actor_id,
    provenance: {
      sources: ["actor_statement:ui"],
    },
    ...thread,
    tags: Array.isArray(thread.tags) ? thread.tags : [],
    key_artifacts: Array.isArray(thread.key_artifacts)
      ? thread.key_artifacts
      : [],
    next_actions: Array.isArray(thread.next_actions) ? thread.next_actions : [],
    open_commitments: Array.isArray(thread.open_commitments)
      ? thread.open_commitments
      : [],
  };

  threads.unshift(created);
  return created;
}

export function getMockThread(threadId) {
  return threads.find((thread) => thread.id === threadId) ?? null;
}

function artifactContentPreview(content) {
  const text = String(content ?? "").trim();
  if (!text) {
    return "";
  }
  return text.length <= 500 ? text : text.slice(0, 500);
}

function buildMockWorkspaceCollaboration(
  recentEvents,
  keyArtifacts,
  openCommitments,
) {
  const recommendations = recentEvents.filter(
    (event) => String(event.type) === "actor_statement",
  );
  const decisionRequests = recentEvents.filter(
    (event) => String(event.type) === "decision_needed",
  );
  const decisions = recentEvents.filter(
    (event) => String(event.type) === "decision_made",
  );

  return {
    recommendations,
    decision_requests: decisionRequests,
    decisions,
    key_artifacts: keyArtifacts,
    open_commitments: openCommitments,
    recommendation_count: recommendations.length,
    decision_request_count: decisionRequests.length,
    decision_count: decisions.length,
    artifact_count: keyArtifacts.length,
    open_commitment_count: openCommitments.length,
  };
}

export function getMockThreadWorkspace(
  threadId,
  { max_events = 20, include_artifact_content = false } = {},
) {
  const thread = getMockThread(threadId);
  if (!thread) {
    return null;
  }

  const threadEvents = listMockTimelineEvents(threadId);
  const recentEvents = threadEvents.slice(
    Math.max(0, threadEvents.length - Number(max_events || 0)),
  );
  const openCommitments = listMockCommitments({
    thread_id: threadId,
    status: "open",
  });
  const documents = listMockDocuments({ thread_id: threadId });
  const keyArtifacts = normalizeRefList(thread.key_artifacts).map((ref) => {
    const { prefix, id } = splitTypedRef(ref);
    const artifact = prefix === "artifact" ? getMockArtifact(id) : null;
    const item = { ref, artifact };
    if (include_artifact_content && artifact?.content_text) {
      item.content_preview = artifactContentPreview(artifact.content_text);
    }
    return item;
  });
  const collaboration = buildMockWorkspaceCollaboration(
    recentEvents,
    keyArtifacts,
    openCommitments,
  );

  return {
    thread_id: threadId,
    thread,
    context: {
      recent_events: recentEvents,
      key_artifacts: keyArtifacts,
      open_commitments: openCommitments,
      documents,
    },
    collaboration,
    inbox: {
      thread_id: threadId,
      items: [],
      count: 0,
      generated_at: new Date().toISOString(),
    },
    pending_decisions: {
      thread_id: threadId,
      items: [],
      count: 0,
      generated_at: new Date().toISOString(),
    },
    related_threads: { items: [], count: 0 },
    related_recommendations: { items: [], count: 0 },
    related_decision_requests: { items: [], count: 0 },
    related_decisions: { items: [], count: 0 },
    total_review_items:
      collaboration.recommendations.length +
      collaboration.decision_requests.length +
      collaboration.decisions.length,
    follow_up: {
      workspace_refresh_command: `oar threads workspace --thread-id ${threadId} --include-artifact-content --full-id --json`,
    },
    section_kinds: {
      thread: "canonical",
      context: "canonical",
      collaboration: "derived",
      inbox: "derived",
      pending_decisions: "derived",
      related_threads: "derived",
      related_recommendations: "derived",
      related_decision_requests: "derived",
      related_decisions: "derived",
      follow_up: "convenience",
    },
    context_source: "threads.workspace",
    inbox_source: "threads.workspace",
    generated_at: new Date().toISOString(),
  };
}

function updateThreadOpenCommitments({ thread_id, commitment_id, status }) {
  const thread = getMockThread(thread_id);
  if (!thread) {
    return;
  }

  const openCommitments = Array.isArray(thread.open_commitments)
    ? [...thread.open_commitments]
    : [];
  const existingIndex = openCommitments.findIndex((id) => id === commitment_id);
  const shouldBeOpen = isOpenCommitmentStatus(status);

  if (shouldBeOpen && existingIndex === -1) {
    openCommitments.push(commitment_id);
  }

  if (!shouldBeOpen && existingIndex >= 0) {
    openCommitments.splice(existingIndex, 1);
  }

  thread.open_commitments = openCommitments;
}

export function updateMockThread({
  actor_id,
  thread_id,
  patch = {},
  if_updated_at,
}) {
  const thread = getMockThread(thread_id);
  if (!thread) {
    return { error: "not_found" };
  }

  if (
    if_updated_at &&
    String(if_updated_at) !== String(thread.updated_at ?? "")
  ) {
    return { error: "conflict", current: thread };
  }

  const next = { ...thread };

  for (const [field, value] of Object.entries(patch)) {
    if (field === "open_commitments") {
      continue;
    }

    if (
      field === "tags" ||
      field === "next_actions" ||
      field === "key_artifacts"
    ) {
      next[field] = Array.isArray(value)
        ? value.map((item) => String(item).trim()).filter(Boolean)
        : [];
      continue;
    }

    next[field] = value;
  }

  next.updated_at = new Date().toISOString();
  next.updated_by = actor_id;

  const index = threads.findIndex((candidate) => candidate.id === thread_id);
  threads[index] = next;

  return { thread: next };
}

export function listMockCommitments(filters = {}) {
  return commitments.filter((commitment) => {
    if (
      filters.thread_id &&
      String(commitment.thread_id) !== String(filters.thread_id)
    ) {
      return false;
    }

    if (filters.owner && String(commitment.owner) !== String(filters.owner)) {
      return false;
    }

    if (
      filters.status &&
      String(commitment.status) !== String(filters.status)
    ) {
      return false;
    }

    if (
      filters.due_before &&
      Date.parse(String(commitment.due_at)) >
        Date.parse(String(filters.due_before))
    ) {
      return false;
    }

    if (
      filters.due_after &&
      Date.parse(String(commitment.due_at)) <
        Date.parse(String(filters.due_after))
    ) {
      return false;
    }

    return true;
  });
}

export function getMockCommitment(commitmentId) {
  return (
    commitments.find((commitment) => commitment.id === commitmentId) ?? null
  );
}

export function createMockCommitment({ actor_id, commitment }) {
  const created = {
    id: `commitment-${Math.random().toString(36).slice(2, 10)}`,
    thread_id: commitment.thread_id,
    title: commitment.title,
    owner: commitment.owner,
    due_at: commitment.due_at,
    status: commitment.status ?? "open",
    definition_of_done: Array.isArray(commitment.definition_of_done)
      ? commitment.definition_of_done
          .map((item) => String(item).trim())
          .filter(Boolean)
      : [],
    links: normalizeRefList(commitment.links),
    updated_at: new Date().toISOString(),
    updated_by: actor_id,
    provenance: commitment.provenance ?? {
      sources: ["actor_statement:ui"],
    },
  };

  commitments.unshift(created);
  updateThreadOpenCommitments({
    thread_id: created.thread_id,
    commitment_id: created.id,
    status: created.status,
  });

  return created;
}

export function updateMockCommitment({
  actor_id,
  commitment_id,
  patch = {},
  refs = [],
  if_updated_at,
}) {
  const commitment = getMockCommitment(commitment_id);
  if (!commitment) {
    return { error: "not_found" };
  }

  if (
    if_updated_at &&
    String(if_updated_at) !== String(commitment.updated_at ?? "")
  ) {
    return { error: "conflict", current: commitment };
  }

  const next = { ...commitment };

  for (const [field, value] of Object.entries(patch)) {
    if (field === "definition_of_done" || field === "links") {
      next[field] = Array.isArray(value)
        ? value.map((item) => String(item).trim()).filter(Boolean)
        : [];
      continue;
    }

    next[field] = value;
  }

  const statusChanged =
    Object.prototype.hasOwnProperty.call(patch, "status") &&
    String(next.status) !== String(commitment.status);

  if (
    statusChanged &&
    (String(next.status) === "done" || String(next.status) === "canceled") &&
    !commitmentHasRequiredStatusRef(String(next.status), refs)
  ) {
    return {
      error: "invalid_transition",
      message:
        String(next.status) === "done"
          ? "status=done requires artifact:<receipt_id> or event:<decision_event_id> in refs."
          : "status=canceled requires event:<decision_event_id> in refs.",
    };
  }

  if (
    statusChanged &&
    (String(next.status) === "done" || String(next.status) === "canceled")
  ) {
    const statusRefs = normalizeRefList(refs);
    next.provenance = {
      ...(next.provenance ?? { sources: [] }),
      by_field: {
        ...((next.provenance ?? {}).by_field ?? {}),
        status: statusRefs,
      },
    };
  }

  next.updated_at = new Date().toISOString();
  next.updated_by = actor_id;

  const index = commitments.findIndex(
    (candidate) => candidate.id === commitment_id,
  );
  commitments[index] = next;

  updateThreadOpenCommitments({
    thread_id: next.thread_id,
    commitment_id: next.id,
    status: next.status,
  });

  return { commitment: next };
}

export function createMockWorkOrder({ actor_id, artifact = {}, packet = {} }) {
  const requestKey = String(arguments[0]?.request_key ?? "").trim();
  const issuedArtifactId =
    requestKey && !artifact.id && !packet.work_order_id
      ? `artifact-work-order-${
          requestKey
            .replace(/[^a-z0-9]+/gi, "-")
            .toLowerCase()
            .slice(0, 20) || "mock"
        }`
      : "";
  const artifactId = String(artifact.id ?? issuedArtifactId).trim();
  const packetId = String(packet.work_order_id ?? artifactId).trim();
  const threadId = String(packet.thread_id ?? artifact.thread_id ?? "").trim();

  if (!artifactId) {
    return { error: "validation", message: "artifact.id is required." };
  }

  if (!packetId) {
    return {
      error: "validation",
      message: "packet.work_order_id is required.",
    };
  }

  if (artifactId !== packetId) {
    return {
      error: "validation",
      message: "packet.work_order_id must match artifact.id.",
    };
  }

  if (!threadId) {
    return { error: "validation", message: "packet.thread_id is required." };
  }

  if (!packet.objective) {
    return { error: "validation", message: "packet.objective is required." };
  }

  const constraints = Array.isArray(packet.constraints)
    ? packet.constraints.map((item) => String(item).trim()).filter(Boolean)
    : [];
  const contextRefs = normalizeRefList(packet.context_refs);
  const acceptanceCriteria = Array.isArray(packet.acceptance_criteria)
    ? packet.acceptance_criteria
        .map((item) => String(item).trim())
        .filter(Boolean)
    : [];
  const definitionOfDone = Array.isArray(packet.definition_of_done)
    ? packet.definition_of_done
        .map((item) => String(item).trim())
        .filter(Boolean)
    : [];

  if (constraints.length === 0) {
    return {
      error: "validation",
      message: "packet.constraints must include at least one item.",
    };
  }

  if (acceptanceCriteria.length === 0) {
    return {
      error: "validation",
      message: "packet.acceptance_criteria must include at least one item.",
    };
  }

  if (definitionOfDone.length === 0) {
    return {
      error: "validation",
      message: "packet.definition_of_done must include at least one item.",
    };
  }

  if (contextRefs.some((ref) => !isTypedRef(ref))) {
    return {
      error: "validation",
      message: "packet.context_refs contains invalid typed refs.",
    };
  }

  const threadRef = `thread:${threadId}`;
  const artifactRefs = normalizeRefList(artifact.refs);
  if (!artifactRefs.includes(threadRef)) {
    return {
      error: "validation",
      message: "artifact.refs must include thread:<thread_id>.",
    };
  }

  const createdArtifact = {
    id: artifactId,
    kind: "work_order",
    thread_id: threadId,
    summary: String(artifact.summary ?? packet.objective).trim(),
    refs: artifactRefs,
    created_at: new Date().toISOString(),
    created_by: actor_id,
    provenance: {
      sources: ["actor_statement:ui"],
    },
    packet: {
      work_order_id: packetId,
      thread_id: threadId,
      objective: String(packet.objective).trim(),
      constraints,
      context_refs: contextRefs,
      acceptance_criteria: acceptanceCriteria,
      definition_of_done: definitionOfDone,
    },
  };

  artifacts.unshift(createdArtifact);

  const createdEvent = {
    id: `event-${Math.random().toString(36).slice(2, 10)}`,
    ts: new Date().toISOString(),
    type: "work_order_created",
    actor_id,
    thread_id: threadId,
    refs: [`artifact:${artifactId}`, threadRef],
    summary: `Work order created: ${createdArtifact.summary}`,
    payload: {
      artifact_id: artifactId,
    },
    provenance: {
      sources: ["actor_statement:ui"],
    },
  };

  events.push(createdEvent);

  return { artifact: createdArtifact, event: createdEvent };
}

export function listMockArtifacts(filters = {}) {
  const includeTombstoned =
    filters.include_tombstoned === true ||
    String(filters.include_tombstoned) === "true";

  return artifacts.filter((artifact) => {
    if (
      !includeTombstoned &&
      artifact.tombstoned_at != null &&
      String(artifact.tombstoned_at).trim() !== ""
    ) {
      return false;
    }

    if (filters.kind && String(artifact.kind) !== String(filters.kind)) {
      return false;
    }

    if (
      filters.thread_id &&
      String(artifact.thread_id) !== String(filters.thread_id)
    ) {
      return false;
    }

    if (
      filters.created_before &&
      Date.parse(String(artifact.created_at ?? 0)) >
        Date.parse(String(filters.created_before))
    ) {
      return false;
    }

    if (
      filters.created_after &&
      Date.parse(String(artifact.created_at ?? 0)) <
        Date.parse(String(filters.created_after))
    ) {
      return false;
    }

    return true;
  });
}

export function getMockArtifact(artifactId) {
  return artifacts.find((artifact) => artifact.id === artifactId) ?? null;
}

export function getMockArtifactContent(artifactId) {
  const artifact = getMockArtifact(artifactId);
  if (!artifact) {
    return null;
  }

  if (
    String(artifact.content_type ?? "").startsWith("text/") &&
    typeof artifact.content_text === "string"
  ) {
    return {
      contentType: artifact.content_type,
      content: artifact.content_text,
    };
  }

  if (artifact.packet) {
    return {
      contentType: "application/json",
      content: artifact.packet,
    };
  }

  return {
    contentType: "application/json",
    content: {
      artifact_id: artifact.id,
      summary: artifact.summary ?? "",
    },
  };
}

export function listMockDocuments(filters = {}) {
  let docs = [...MOCK_DOCUMENTS];
  if (filters.thread_id) {
    docs = docs.filter(
      (doc) => String(doc.thread_id ?? "") === String(filters.thread_id),
    );
  }
  if (!filters.include_tombstoned) {
    docs = docs.filter((d) => !d.tombstoned_at);
  }
  return docs
    .map((doc) => {
      const revisions = MOCK_DOCUMENT_REVISIONS[String(doc.id)] || [];
      const headRevision =
        revisions.find((rev) => rev.revision_id === doc.head_revision_id) ||
        revisions[revisions.length - 1] ||
        null;
      return {
        ...doc,
        head_revision: headRevision
          ? {
              revision_id: headRevision.revision_id,
              revision_number: headRevision.revision_number,
              artifact_id: headRevision.artifact_id,
              content_type: headRevision.content_type,
              created_at: headRevision.created_at,
              created_by: headRevision.created_by,
            }
          : {
              revision_id: doc.head_revision_id,
              revision_number: doc.head_revision_number,
            },
      };
    })
    .sort((left, right) => {
      const timeDelta =
        Date.parse(right.updated_at ?? 0) - Date.parse(left.updated_at ?? 0);
      if (timeDelta !== 0) return timeDelta;
      return String(left.id ?? "").localeCompare(String(right.id ?? ""));
    });
}

export function createMockDocument({
  actor_id,
  document: docFields = {},
  content,
  content_type,
}) {
  const docId = String(
    docFields.id || `doc-${Math.random().toString(36).slice(2, 10)}`,
  ).trim();
  const title = String(docFields.title || "").trim();

  if (!title) {
    return { error: "validation", message: "document.title is required." };
  }

  if (!content && content !== 0) {
    return { error: "validation", message: "content is required." };
  }

  if (!content_type) {
    return { error: "validation", message: "content_type is required." };
  }

  if (MOCK_DOCUMENTS.find((d) => d.id === docId)) {
    return {
      error: "conflict",
      message: `Document with id '${docId}' already exists.`,
    };
  }

  const now = new Date().toISOString();
  const revisionId = `rev-${docId}-1`;

  const newDoc = {
    id: docId,
    title,
    slug: docId,
    status: String(docFields.status || "draft"),
    labels: Array.isArray(docFields.labels) ? docFields.labels : [],
    supersedes: [],
    head_revision_id: revisionId,
    head_revision_number: 1,
    thread_id: docFields.thread_id || null,
    created_at: now,
    created_by: actor_id,
    updated_at: now,
    updated_by: actor_id,
    tombstoned_at: null,
  };

  const newRevision = {
    document_id: docId,
    revision_id: revisionId,
    artifact_id: revisionId,
    revision_number: 1,
    prev_revision_id: null,
    created_at: now,
    created_by: actor_id,
    content_type,
    content_hash: `hash-${Math.random().toString(36).slice(2, 10)}`,
    revision_hash: `rhash-${Math.random().toString(36).slice(2, 10)}`,
    content: String(content),
  };

  MOCK_DOCUMENTS.unshift(newDoc);
  MOCK_DOCUMENT_REVISIONS[docId] = [newRevision];

  return { document: newDoc, revision: newRevision };
}

export function updateMockDocument({
  actor_id,
  document_id,
  content,
  content_type,
  if_base_revision,
  document: docPatch = {},
}) {
  const docIndex = MOCK_DOCUMENTS.findIndex((d) => d.id === document_id);
  if (docIndex === -1) {
    return { error: "not_found", message: "Document not found." };
  }

  const doc = MOCK_DOCUMENTS[docIndex];

  if (!content && content !== 0) {
    return { error: "validation", message: "content is required." };
  }

  if (!content_type) {
    return { error: "validation", message: "content_type is required." };
  }

  if (!if_base_revision) {
    return { error: "validation", message: "if_base_revision is required." };
  }

  const revisions = MOCK_DOCUMENT_REVISIONS[document_id] || [];
  const headRevision =
    revisions.find((r) => r.revision_id === doc.head_revision_id) ||
    revisions[revisions.length - 1];

  if (
    headRevision &&
    String(if_base_revision) !== String(headRevision.revision_id)
  ) {
    return {
      error: "conflict",
      message: `Optimistic concurrency conflict: expected base revision '${if_base_revision}', but current head is '${headRevision?.revision_id}'.`,
    };
  }

  const now = new Date().toISOString();
  const newRevisionNumber = (headRevision?.revision_number ?? 0) + 1;
  const newRevisionId = `rev-${document_id}-${newRevisionNumber}-${Math.random().toString(36).slice(2, 6)}`;

  const newRevision = {
    document_id,
    revision_id: newRevisionId,
    artifact_id: newRevisionId,
    revision_number: newRevisionNumber,
    prev_revision_id: headRevision?.revision_id ?? null,
    created_at: now,
    created_by: actor_id,
    content_type,
    content_hash: `hash-${Math.random().toString(36).slice(2, 10)}`,
    revision_hash: `rhash-${Math.random().toString(36).slice(2, 10)}`,
    content: String(content),
  };

  const updatedDoc = {
    ...doc,
    head_revision_id: newRevisionId,
    head_revision_number: newRevisionNumber,
    updated_at: now,
    updated_by: actor_id,
  };

  if (docPatch.title) updatedDoc.title = String(docPatch.title).trim();
  if (docPatch.status) updatedDoc.status = String(docPatch.status);
  if (Array.isArray(docPatch.labels)) updatedDoc.labels = docPatch.labels;

  MOCK_DOCUMENTS[docIndex] = updatedDoc;
  MOCK_DOCUMENT_REVISIONS[document_id] = [...revisions, newRevision];

  return { document: updatedDoc, revision: newRevision };
}

export function getMockDocument(documentId) {
  const doc = MOCK_DOCUMENTS.find((d) => d.id === documentId);
  if (!doc) return null;
  const revisions = MOCK_DOCUMENT_REVISIONS[documentId] || [];
  const headRevision =
    revisions.find((r) => r.revision_id === doc.head_revision_id) ||
    revisions[revisions.length - 1] ||
    null;
  return { document: doc, revision: headRevision };
}

export function getMockDocumentHistory(documentId) {
  return MOCK_DOCUMENT_REVISIONS[documentId] || [];
}

export function getMockDocumentRevision(documentId, revisionId) {
  const revisions = MOCK_DOCUMENT_REVISIONS[documentId] || [];
  return revisions.find((r) => r.revision_id === revisionId) || null;
}

export function createMockReceipt({ actor_id, artifact = {}, packet = {} }) {
  const requestKey = String(arguments[0]?.request_key ?? "").trim();
  const issuedArtifactId =
    requestKey && !artifact.id && !packet.receipt_id
      ? `artifact-receipt-${
          requestKey
            .replace(/[^a-z0-9]+/gi, "-")
            .toLowerCase()
            .slice(0, 20) || "mock"
        }`
      : "";
  const artifactId = String(artifact.id ?? issuedArtifactId).trim();
  const packetId = String(packet.receipt_id ?? artifactId).trim();
  const threadId = String(packet.thread_id ?? artifact.thread_id ?? "").trim();
  const workOrderId = String(packet.work_order_id ?? "").trim();

  if (!artifactId) {
    return { error: "validation", message: "artifact.id is required." };
  }

  if (!packetId) {
    return { error: "validation", message: "packet.receipt_id is required." };
  }

  if (artifactId !== packetId) {
    return {
      error: "validation",
      message: "packet.receipt_id must match artifact.id.",
    };
  }

  if (!threadId) {
    return { error: "validation", message: "packet.thread_id is required." };
  }

  if (!workOrderId) {
    return {
      error: "validation",
      message: "packet.work_order_id is required.",
    };
  }

  const outputs = normalizeRefList(packet.outputs);
  const verificationEvidence = normalizeRefList(packet.verification_evidence);
  const changesSummary = String(packet.changes_summary ?? "").trim();
  const knownGaps = Array.isArray(packet.known_gaps)
    ? packet.known_gaps.map((item) => String(item).trim()).filter(Boolean)
    : [];

  if (outputs.length === 0) {
    return {
      error: "validation",
      message: "packet.outputs must include at least one typed ref.",
    };
  }

  if (verificationEvidence.length === 0) {
    return {
      error: "validation",
      message:
        "packet.verification_evidence must include at least one typed ref.",
    };
  }

  if (!changesSummary) {
    return {
      error: "validation",
      message: "packet.changes_summary is required.",
    };
  }

  if (
    outputs.some((refValue) => !isTypedRef(refValue)) ||
    verificationEvidence.some((refValue) => !isTypedRef(refValue))
  ) {
    return {
      error: "validation",
      message: "packet outputs/evidence contains invalid typed refs.",
    };
  }

  const threadRef = `thread:${threadId}`;
  const workOrderRef = `artifact:${workOrderId}`;
  const artifactRefs = normalizeRefList(artifact.refs);

  if (
    !artifactRefs.includes(threadRef) ||
    !artifactRefs.includes(workOrderRef)
  ) {
    return {
      error: "validation",
      message:
        "artifact.refs must include thread:<thread_id> and artifact:<work_order_id>.",
    };
  }

  const createdArtifact = {
    id: artifactId,
    kind: "receipt",
    thread_id: threadId,
    summary: String(artifact.summary ?? `Receipt for ${workOrderId}`).trim(),
    refs: artifactRefs,
    created_at: new Date().toISOString(),
    created_by: actor_id,
    provenance: {
      sources: ["actor_statement:ui"],
    },
    packet: {
      receipt_id: packetId,
      work_order_id: workOrderId,
      thread_id: threadId,
      outputs,
      verification_evidence: verificationEvidence,
      changes_summary: changesSummary,
      known_gaps: knownGaps,
    },
  };

  artifacts.unshift(createdArtifact);

  const createdEvent = {
    id: `event-${Math.random().toString(36).slice(2, 10)}`,
    ts: new Date().toISOString(),
    type: "receipt_added",
    actor_id,
    thread_id: threadId,
    refs: [`artifact:${artifactId}`, `artifact:${workOrderId}`, threadRef],
    summary: `Receipt added: ${createdArtifact.summary}`,
    payload: {
      artifact_id: artifactId,
      work_order_id: workOrderId,
    },
    provenance: {
      sources: ["actor_statement:ui"],
    },
  };

  events.push(createdEvent);

  return { artifact: createdArtifact, event: createdEvent };
}

export function createMockReview({ actor_id, artifact = {}, packet = {} }) {
  const artifactId = String(artifact.id ?? "").trim();
  const packetId = String(packet.review_id ?? "").trim();
  const receiptId = String(packet.receipt_id ?? "").trim();
  const workOrderId = String(packet.work_order_id ?? "").trim();
  const threadId = String(artifact.thread_id ?? "").trim();

  if (!artifactId) {
    return { error: "validation", message: "artifact.id is required." };
  }

  if (!packetId) {
    return { error: "validation", message: "packet.review_id is required." };
  }

  if (artifactId !== packetId) {
    return {
      error: "validation",
      message: "packet.review_id must match artifact.id.",
    };
  }

  if (!threadId) {
    return { error: "validation", message: "artifact.thread_id is required." };
  }

  if (!receiptId) {
    return { error: "validation", message: "packet.receipt_id is required." };
  }

  if (!workOrderId) {
    return {
      error: "validation",
      message: "packet.work_order_id is required.",
    };
  }

  const outcome = String(packet.outcome ?? "").trim();
  const notes = String(packet.notes ?? "").trim();
  const evidenceRefs = normalizeRefList(packet.evidence_refs);

  if (!["accept", "revise", "escalate"].includes(outcome)) {
    return {
      error: "validation",
      message: "packet.outcome must be one of: accept, revise, escalate.",
    };
  }

  if (!notes) {
    return { error: "validation", message: "packet.notes is required." };
  }

  if (evidenceRefs.some((refValue) => !isTypedRef(refValue))) {
    return {
      error: "validation",
      message: "packet.evidence_refs contains invalid typed refs.",
    };
  }

  const threadRef = `thread:${threadId}`;
  const receiptRef = `artifact:${receiptId}`;
  const workOrderRef = `artifact:${workOrderId}`;
  const artifactRefs = normalizeRefList(artifact.refs);

  if (
    !artifactRefs.includes(threadRef) ||
    !artifactRefs.includes(receiptRef) ||
    !artifactRefs.includes(workOrderRef)
  ) {
    return {
      error: "validation",
      message:
        "artifact.refs must include thread:<thread_id>, artifact:<receipt_id>, and artifact:<work_order_id>.",
    };
  }

  const createdArtifact = {
    id: artifactId,
    kind: "review",
    thread_id: threadId,
    summary: String(
      artifact.summary ?? `Review (${outcome}) for ${receiptId}`,
    ).trim(),
    refs: artifactRefs,
    created_at: new Date().toISOString(),
    created_by: actor_id,
    provenance: {
      sources: ["actor_statement:ui"],
    },
    packet: {
      review_id: packetId,
      work_order_id: workOrderId,
      receipt_id: receiptId,
      outcome,
      notes,
      evidence_refs: evidenceRefs,
    },
  };

  artifacts.unshift(createdArtifact);

  const createdEvent = {
    id: `event-${Math.random().toString(36).slice(2, 10)}`,
    ts: new Date().toISOString(),
    type: "review_completed",
    actor_id,
    thread_id: threadId,
    refs: [
      `artifact:${artifactId}`,
      `artifact:${receiptId}`,
      `artifact:${workOrderId}`,
      threadRef,
    ],
    summary: `Review completed (${outcome})`,
    payload: {
      artifact_id: artifactId,
      receipt_id: receiptId,
      work_order_id: workOrderId,
      outcome,
    },
    provenance: {
      sources: ["actor_statement:ui"],
    },
  };

  events.push(createdEvent);

  return { artifact: createdArtifact, event: createdEvent };
}
