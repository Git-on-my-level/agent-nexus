import { cadenceMatchesFilter } from "./topicFilters.js";

// ─── Zesty Bots Lemonade Co. ──────────────────────────────────────────────────
// A fully-automated lemonade stand operated by AI agents and robots.
// This seed data represents a realistic mid-week snapshot of operations.

const now = Date.now();

const actors = [
  {
    id: "actor-dev-human-operator",
    display_name: "Jordan (Human operator)",
    tags: ["human", "operator"],
    created_at: "2026-01-01T07:55:00.000Z",
  },
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
    open_cards: ["card-emergency-restock", "card-sla-review"],
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
    open_cards: ["thread-summer-menu"],
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
      "RoboSupply Inc. — delivery ETA tomorrow 09:00. Timeline paused pending part arrival.",
    next_actions: [
      "Receive part #TL-3000-L delivery from RoboSupply Inc. (ETA: tomorrow 09:00)",
      "SqueezeBot to run recalibration sequence per maintenance card",
      "FlavorMind QA scan to validate seed contamination rate <1% post-repair",
    ],
    open_cards: ["thread-squeezebot-maintenance"],
    next_check_in_at: new Date(now + 1 * 24 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 2 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-squeeze-bot",
    provenance: {
      sources: ["inferred"],
      notes: "Timeline paused pending part delivery from RoboSupply Inc.",
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
    open_cards: [],
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
    open_cards: [],
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
    open_cards: ["card-q2-permit", "card-q2-menu"],
    next_check_in_at: new Date(now + 25 * 24 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
    provenance: {
      sources: ["actor_statement:evt-q2-001"],
    },
  },
  {
    id: "thread-onboarding",
    type: "process",
    title: "Agent onboarding and continuity",
    status: "active",
    priority: "p2",
    tags: ["onboarding", "ops", "q2"],
    key_artifacts: [],
    cadence: "weekly",
    current_summary:
      "Runbook and checklist for bringing new agents (FlavorMind, Till-E, SupplyRover) " +
      "online. Onboarding guide v1 in use. Next: document handoff steps for SqueezeBot 2000 " +
      "when Riverside stand opens.",
    next_actions: [
      "Update onboarding guide with POS and inventory system setup steps",
      "Schedule knowledge-transfer session before Riverside go-live",
    ],
    open_cards: [],
    next_check_in_at: new Date(now + 5 * 24 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 5 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
    provenance: {
      sources: ["actor_statement:evt-onboard-001"],
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
    card_id: "card-emergency-restock",
    related_refs: [
      "thread:thread-lemon-shortage",
      "artifact:artifact-supplier-sla",
      "event:evt-supply-004",
    ],
    source_event_time: new Date(now - 1 * 60 * 60 * 1000).toISOString(),
  },
  {
    id: "inbox-002",
    category: "stale_topic",
    title: "Lemon inventory critically low — stand may halt within 2 hours",
    recommended_action:
      "Acknowledge. SqueezeBot is already in half-batch mode. " +
      "Confirm restock order is approved to avoid a full production halt.",
    thread_id: "thread-lemon-shortage",
    related_refs: ["thread:thread-lemon-shortage", "event:evt-supply-001"],
    source_event_time: new Date(now - 18 * 60 * 60 * 1000).toISOString(),
  },
  {
    id: "inbox-003",
    category: "work_item_risk",
    title: "Summer launch at risk — lemon shortage blocks pilot batch",
    recommended_action:
      "Update summer menu thread with expected unblock date once lemon restock is confirmed.",
    thread_id: "thread-summer-menu",
    card_id: "thread-summer-menu",
    related_refs: ["thread:thread-summer-menu", "thread:thread-lemon-shortage"],
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
    card_id: "thread-squeezebot-maintenance",
    related_refs: [
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
    refs: ["thread:thread-lemon-shortage", "event:evt-supply-001"],
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
    type: "thread_updated",
    actor_id: "actor-ops-ai",
    thread_id: "thread-lemon-shortage",
    refs: ["thread:thread-lemon-shortage"],
    summary: "Priority raised to P0.",
    payload: { changed_fields: ["priority", "current_summary"] },
    provenance: { sources: ["actor_statement:evt-supply-003"] },
  },
  {
    id: "evt-supply-004",
    ts: new Date(now - 1 * 60 * 60 * 1000).toISOString(),
    type: "message_posted",
    actor_id: "actor-supply-rover",
    thread_id: "thread-lemon-shortage",
    refs: ["thread:thread-lemon-shortage", "event:evt-supply-002"],
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
    type: "thread_updated",
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
    summary: "OpsAI issued maintenance card and ordered replacement part.",
    payload: {
      text:
        "Confirmed. Card created. Placed order with RoboSupply Inc. for torque " +
        "limiter part #TL-3000-L — estimated delivery tomorrow 09:00. Timeline paused " +
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

  // ── Lemon shortage: exception raised + card created ──────────────────────
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
    id: "evt-supply-card-restock",
    ts: new Date(now - 18 * 60 * 60 * 1000 + 5 * 60 * 1000).toISOString(),
    type: "card_created",
    actor_id: "actor-ops-ai",
    thread_id: "thread-lemon-shortage",
    refs: [
      "thread:thread-lemon-shortage",
      "board:board-supply-crisis",
      "card:card-emergency-restock",
    ],
    summary: "Card created: place emergency restock order.",
    payload: { card_id: "card-emergency-restock" },
    provenance: { sources: ["actor_statement:evt-supply-002"] },
  },

  // ── Summer menu: receipt_added / review_completed (card-scoped) ──────────
  {
    id: "evt-menu-card-board",
    ts: new Date(now - 3 * 24 * 60 * 60 * 1000 + 10 * 60 * 1000).toISOString(),
    type: "card_created",
    actor_id: "actor-flavor-ai",
    thread_id: "thread-summer-menu",
    refs: [
      "thread:thread-summer-menu",
      "board:board-product-launch",
      "card:thread-summer-menu",
    ],
    summary: "Card created: update menu board with summer flavors.",
    payload: { card_id: "thread-summer-menu" },
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
      "card:thread-summer-menu",
      "artifact:artifact-receipt-lavender-sourcing",
    ],
    summary: "Receipt added: lavender syrup sourced from BotBotanicals API.",
    payload: {
      artifact_id: "artifact-receipt-lavender-sourcing",
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
      "card:thread-summer-menu",
      "artifact:artifact-review-lavender-sourcing",
      "artifact:artifact-receipt-lavender-sourcing",
    ],
    summary: "Review completed (accept): lavender sourcing receipt approved.",
    payload: {
      artifact_id: "artifact-review-lavender-sourcing",
      receipt_id: "artifact-receipt-lavender-sourcing",
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
      "topic:pricing-glitch",
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
    id: "evt-price-005",
    ts: new Date(
      now - 10 * 24 * 60 * 60 * 1000 + 2 * 60 * 60 * 1000 + 5 * 60 * 1000,
    ).toISOString(),
    type: "card_created",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "board:board-summer-menu",
      "card:thread-pricing-glitch",
    ],
    summary: "Card created: patch and validate pricing cache invalidation.",
    payload: { card_id: "thread-pricing-glitch" },
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
      "card:thread-pricing-glitch",
      "artifact:artifact-receipt-pricing-v1",
    ],
    summary:
      "Receipt added (v1): pricing issue investigated — refund decision still needed.",
    payload: {
      artifact_id: "artifact-receipt-pricing-v1",
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
      "card:thread-pricing-glitch",
      "artifact:artifact-review-pricing-escalate",
      "artifact:artifact-receipt-pricing-v1",
    ],
    summary:
      "Review completed (escalate): refund policy decision required before acceptance.",
    payload: {
      artifact_id: "artifact-review-pricing-escalate",
      receipt_id: "artifact-receipt-pricing-v1",
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
      "topic:pricing-glitch",
      "thread:thread-pricing-glitch",
      "artifact:artifact-pricing-evidence",
      "card:thread-pricing-glitch",
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
      "card:thread-pricing-glitch",
      "artifact:artifact-receipt-pricing-v2",
    ],
    summary:
      "Receipt added (v2): cache fix deployed, refunds confirmed, patch validated.",
    payload: {
      artifact_id: "artifact-receipt-pricing-v2",
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
      "card:thread-pricing-glitch",
      "artifact:artifact-review-pricing-accept",
      "artifact:artifact-receipt-pricing-v2",
    ],
    summary:
      "Review completed (accept): pricing fix accepted, incident ready to close.",
    payload: {
      artifact_id: "artifact-review-pricing-accept",
      receipt_id: "artifact-receipt-pricing-v2",
      outcome: "accept",
    },
    provenance: { sources: ["actor_statement:evt-price-010"] },
  },
  {
    id: "evt-price-011",
    ts: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
    type: "card_resolved",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "board:board-summer-menu",
      "card:thread-pricing-glitch",
      "artifact:artifact-receipt-pricing-v2",
    ],
    summary: "Card resolved: pricing cache fix deployed and validated.",
    payload: { resolution: "completed" },
    provenance: { sources: ["actor_statement:evt-price-011"] },
  },
  {
    id: "evt-price-012",
    ts: new Date(now - 7 * 24 * 60 * 60 * 1000 + 30 * 60 * 1000).toISOString(),
    type: "card_resolved",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: [
      "thread:thread-pricing-glitch",
      "board:board-summer-menu",
      "card:card-pricing-audit",
      "event:evt-price-008",
    ],
    summary:
      "Card resolved: full pricing audit canceled after root cause confirmed.",
    payload: {
      resolution: "canceled",
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
    type: "thread_updated",
    actor_id: "actor-ops-ai",
    thread_id: "thread-pricing-glitch",
    refs: ["thread:thread-pricing-glitch"],
    summary: "Incident closed — timeline fully resolved.",
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
    type: "thread_updated",
    actor_id: "actor-ops-ai",
    thread_id: "thread-q2-initiative",
    refs: ["thread:thread-q2-initiative"],
    summary:
      "Monthly check-in: permit in review, SqueezeBot 2000 delivery on track.",
    payload: { changed_fields: ["current_summary", "next_actions"] },
    provenance: { sources: ["actor_statement:evt-q2-002"] },
  },
  {
    id: "evt-q2-card-permit",
    ts: new Date(now - 14 * 24 * 60 * 60 * 1000 + 15 * 60 * 1000).toISOString(),
    type: "card_created",
    actor_id: "actor-ops-ai",
    thread_id: "thread-q2-initiative",
    refs: [
      "thread:thread-q2-initiative",
      "board:board-product-launch",
      "card:card-q2-permit",
    ],
    summary: "Card created: monitor city permit and confirm approval.",
    payload: { card_id: "card-q2-permit" },
    provenance: { sources: ["actor_statement:evt-q2-001"] },
  },
  {
    id: "evt-q2-card-menu",
    ts: new Date(now - 14 * 24 * 60 * 60 * 1000 + 20 * 60 * 1000).toISOString(),
    type: "card_created",
    actor_id: "actor-ops-ai",
    thread_id: "thread-q2-initiative",
    refs: [
      "thread:thread-q2-initiative",
      "board:board-product-launch",
      "card:card-q2-menu",
    ],
    summary: "Card created: FlavorMind to draft Riverside seasonal menu.",
    payload: { card_id: "card-q2-menu" },
    provenance: { sources: ["actor_statement:evt-q2-001"] },
  },

  // ── Onboarding thread ───────────────────────────────────────────────────
  {
    id: "evt-onboard-001",
    ts: new Date(now - 5 * 24 * 60 * 60 * 1000).toISOString(),
    type: "message_posted",
    actor_id: "actor-ops-ai",
    thread_id: "thread-onboarding",
    refs: ["thread:thread-onboarding", "document:onboarding-guide-v1"],
    summary: "OpsAI opened onboarding runbook thread for new agent setup.",
    payload: {
      text:
        "Tracking agent onboarding and continuity here. Onboarding guide v1 is the source " +
        "of record. When SqueezeBot 2000 arrives for Riverside, we'll add stand setup and " +
        "handoff steps. Till-E and FlavorMind were onboarded using this runbook.",
    },
    provenance: { sources: ["actor_statement:evt-onboard-001"] },
  },
];

const artifacts = [
  {
    id: "artifact-supplier-sla",
    kind: "doc",
    thread_id: "thread-lemon-shortage",
    summary: "CitrusBot Farm SLA — uptime and delivery terms",
    refs: ["thread:thread-lemon-shortage"],
    content_type: "text/markdown",
    content_text: `# CitrusBot Farm Supplier SLA

**Supplier:** CitrusBot Farm (API: api.citrusbotfarm.io)
**Contract term:** 2026-01-01 to 2026-12-31
**Account:** Zesty Bots Lemonade Co.

---

## Uptime SLA
- 99.5% monthly uptime on procurement API
- Maximum 4-hour outage response time (acknowledgement)

## Delivery windows
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
    trashed_at: null,
  },
  {
    id: "artifact-supplier-sla-v2",
    kind: "doc",
    thread_id: "thread-lemon-shortage",
    summary: "CitrusBot Farm SLA — uptime and delivery terms",
    refs: ["thread:thread-lemon-shortage", "artifact:artifact-supplier-sla"],
    content_type: "text/markdown",
    content_text: `# CitrusBot Farm Supplier SLA (Amended)

**Supplier:** CitrusBot Farm (API: api.citrusbotfarm.io)
**Contract term:** 2026-01-01 to 2026-12-31
**Account:** Zesty Bots Lemonade Co.
**Amendment:** Emergency response SLA tightened following March breach.

---

## Uptime SLA
- 99.5% monthly uptime on procurement API
- Maximum **2-hour** outage response time (reduced from 4h after breach)

## Delivery windows
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
    trashed_at: null,
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
2. 🟡 Menu board update pending (Till-E — thread-summer-menu card)
3. 🟢 Lavender syrup: contracted and on order`,
    created_at: new Date(now - 5 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-flavor-ai",
    provenance: { sources: ["actor_statement:evt-menu-001"] },
    trashed_at: null,
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
    trashed_at: null,
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
      `${new Date(now - 2 * 24 * 60 * 60 * 1000 + 15 * 60 * 1000).toISOString()} [OpsAI] Maintenance card created. Ordering part #TL-3000-L from RoboSupply Inc.`,
      `${new Date(now - 2 * 24 * 60 * 60 * 1000 + 18 * 60 * 1000).toISOString()} [RoboSupply Inc.] Order confirmed. Order ID: RS-20260305-4421. Estimated delivery: +24h.`,
      `${new Date(now - 2 * 24 * 60 * 60 * 1000 + 19 * 60 * 1000).toISOString()} [SqueezeBot 3000] Running in degraded mode. Left arm at 80% duty cycle. Throughput -20%.`,
    ].join("\n"),
    created_at: new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-squeeze-bot",
    provenance: { sources: ["actor_statement:evt-maint-001"] },
    trashed_at: null,
  },
  {
    id: "artifact-receipt-lavender-sourcing",
    kind: "receipt",
    thread_id: "thread-summer-menu",
    summary: "Receipt: Lavender syrup sourced — BotBotanicals API, 2L ordered",
    refs: ["thread:thread-summer-menu", "card:thread-summer-menu"],
    created_at: new Date(
      now - 2 * 24 * 60 * 60 * 1000 + 2 * 60 * 60 * 1000,
    ).toISOString(),
    created_by: "actor-flavor-ai",
    provenance: { sources: ["actor_statement:evt-menu-003"] },
    packet: {
      receipt_id: "artifact-receipt-lavender-sourcing",
      subject_ref: "card:thread-summer-menu",
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
    trashed_at: null,
  },
  {
    id: "artifact-review-lavender-sourcing",
    kind: "review",
    thread_id: "thread-summer-menu",
    summary: "Review: Lavender sourcing receipt — accepted with minor note",
    refs: [
      "thread:thread-summer-menu",
      "card:thread-summer-menu",
      "artifact:artifact-receipt-lavender-sourcing",
    ],
    created_at: new Date(
      now - 2 * 24 * 60 * 60 * 1000 + 3 * 60 * 60 * 1000,
    ).toISOString(),
    created_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-menu-003"] },
    packet: {
      review_id: "artifact-review-lavender-sourcing",
      subject_ref: "card:thread-summer-menu",
      receipt_ref: "artifact:artifact-receipt-lavender-sourcing",
      receipt_id: "artifact-receipt-lavender-sourcing",
      outcome: "accept",
      notes:
        "BotBotanicals pricing checks out — margin target preserved at 81%. " +
        "Two suppliers evaluated as required. Manual reorder gap is acceptable for now; " +
        "flag for Q3 automation sprint. Sourcing work can close once delivery is confirmed " +
        "by SupplyRover and inventory is updated.",
      evidence_refs: ["artifact:artifact-summer-menu-draft"],
    },
    trashed_at: null,
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
    trashed_at: null,
  },
  {
    id: "artifact-receipt-pricing-v1",
    kind: "receipt",
    thread_id: "thread-pricing-glitch",
    summary:
      "Receipt v1: root cause identified — awaiting refund decision before closing",
    refs: ["thread:thread-pricing-glitch", "card:thread-pricing-glitch"],
    created_at: new Date(now - 9 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-cashier-bot",
    provenance: { sources: ["actor_statement:evt-price-006"] },
    trashed_at: null,
    packet: {
      receipt_id: "artifact-receipt-pricing-v1",
      subject_ref: "card:thread-pricing-glitch",
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
      "Review v1 (escalate): refund decision required before receipt can be accepted",
    refs: [
      "thread:thread-pricing-glitch",
      "card:thread-pricing-glitch",
      "artifact:artifact-receipt-pricing-v1",
    ],
    created_at: new Date(
      now - 9 * 24 * 60 * 60 * 1000 + 1 * 60 * 60 * 1000,
    ).toISOString(),
    created_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-price-007"] },
    trashed_at: null,
    packet: {
      review_id: "artifact-review-pricing-escalate",
      subject_ref: "card:thread-pricing-glitch",
      receipt_ref: "artifact:artifact-receipt-pricing-v1",
      receipt_id: "artifact-receipt-pricing-v1",
      outcome: "escalate",
      notes:
        "Root cause analysis is solid and the fix approach looks correct. However, the receipt " +
        "cannot be accepted while the refund decision is unresolved — closure requires confirmed " +
        "customer refunds per the incident criteria. " +
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
    refs: ["thread:thread-pricing-glitch", "card:thread-pricing-glitch"],
    created_at: new Date(now - 8 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-cashier-bot",
    provenance: { sources: ["actor_statement:evt-price-009"] },
    trashed_at: null,
    packet: {
      receipt_id: "artifact-receipt-pricing-v2",
      subject_ref: "card:thread-pricing-glitch",
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
      "card:thread-pricing-glitch",
      "artifact:artifact-receipt-pricing-v2",
    ],
    created_at: new Date(
      now - 8 * 24 * 60 * 60 * 1000 + 1 * 60 * 60 * 1000,
    ).toISOString(),
    created_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:evt-price-010"] },
    packet: {
      review_id: "artifact-review-pricing-accept",
      subject_ref: "card:thread-pricing-glitch",
      receipt_ref: "artifact:artifact-receipt-pricing-v2",
      receipt_id: "artifact-receipt-pricing-v2",
      outcome: "accept",
      notes:
        "All acceptance criteria met: root cause documented, fix deployed and validated on " +
        "10 test transactions, all 3 customer refunds confirmed. The cache TTL reduction from " +
        "7 days to 1 hour is a good systemic improvement — this won't recur on future config pushes. " +
        "Open work can be marked done. Thread ready to close.",
      evidence_refs: [
        "artifact:artifact-pricing-evidence",
        "artifact:artifact-receipt-pricing-v2",
      ],
    },
    trashed_at: null,
  },
  // Trashed after seed create (see seed-core-from-mock.mjs) for Trash / permanent delete in local dev.
  {
    id: "artifact-dev-trash-onboarding-draft",
    kind: "evidence",
    thread_id: "thread-onboarding",
    summary: "Obsolete onboarding checklist (dev trash sample)",
    refs: ["thread:thread-onboarding"],
    content_type: "text/plain",
    content_text:
      "Dev seed: superseded onboarding notes. Eligible for permanent delete — not linked to any document revision.",
    created_at: new Date(now - 3 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    provenance: { sources: ["actor_statement:dev-trash-seed"] },
    trashed_at: new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString(),
    trashed_by: "actor-ops-ai",
    trash_reason:
      "Dev seed: removed from active use so operators can exercise Trash and permanent delete locally.",
  },
  {
    id: "artifact-dev-trash-ops-scratch",
    kind: "evidence",
    thread_id: "thread-onboarding",
    summary: "Scratch export — dev trash sample",
    refs: ["thread:thread-onboarding"],
    content_type: "text/plain",
    content_text:
      "Dev seed: ephemeral export blob. Delete permanently from Trash to verify removal.",
    created_at: new Date(now - 4 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-flavor-ai",
    provenance: { sources: ["actor_statement:dev-trash-seed"] },
    trashed_at: new Date(now - 1 * 24 * 60 * 60 * 1000).toISOString(),
    trashed_by: "actor-ops-ai",
    trash_reason:
      "Dev seed: trashed for local permanent-delete workflow testing.",
  },
  {
    id: "artifact-trashed-doc",
    kind: "doc",
    thread_id: "thread-pricing-glitch",
    summary: "Superseded draft — replaced by final evidence artifact",
    refs: ["thread:thread-pricing-glitch"],
    content_type: "text/plain",
    content_text: "This artifact was superseded and moved to trash.",
    created_at: new Date(now - 11 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-cashier-bot",
    provenance: { sources: ["actor_statement:evt-price-001"] },
    trashed_at: new Date(now - 10 * 24 * 60 * 60 * 1000).toISOString(),
    trashed_by: "actor-ops-ai",
    trash_reason:
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
    thread_id: "thread-q2-initiative",
    created_at: "2026-02-15T10:00:00Z",
    created_by: "actor-ops-ai",
    updated_at: "2026-03-08T14:30:00Z",
    updated_by: "actor-ops-ai",
    trashed_at: null,
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
    created_by: "actor-ops-ai",
    updated_at: "2026-03-05T11:00:00Z",
    updated_by: "actor-ops-ai",
    trashed_at: null,
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
    thread_id: "thread-onboarding",
    created_at: "2026-01-10T08:00:00Z",
    created_by: "actor-ops-ai",
    updated_at: "2026-01-10T08:00:00Z",
    updated_by: "actor-ops-ai",
    trashed_at: null,
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
    created_by: "actor-ops-ai",
    updated_at: "2026-03-01T10:00:00Z",
    updated_by: "actor-ops-ai",
    trashed_at: "2026-03-01T10:00:00Z",
    trashed_by: "actor-ops-ai",
    trash_reason: "Superseded by updated pricing model",
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
      created_by: "actor-ops-ai",
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
      created_by: "actor-ops-ai",
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
      created_by: "actor-ops-ai",
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
      created_by: "actor-ops-ai",
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
      created_by: "actor-ops-ai",
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
      created_by: "actor-ops-ai",
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
      created_by: "actor-ops-ai",
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

function topicStatusFromThreadStatus(status) {
  switch (String(status ?? "").trim()) {
    case "active":
      return "active";
    case "paused":
    case "blocked":
      return "paused";
    case "closed":
    case "archived":
      return "closed";
    default:
      return "active";
  }
}

function topicTypeFromThreadType(type) {
  switch (String(type ?? "").trim()) {
    case "incident":
      return "incident";
    case "initiative":
      return "initiative";
    case "case":
      return "decision";
    case "process":
      return "objective";
    case "note":
      return "note";
    case "request":
      return "request";
    case "risk":
      return "risk";
    default:
      return "other";
  }
}

function cardRiskFromThreadPriority(priority) {
  switch (String(priority ?? "").trim()) {
    case "p0":
      return "critical";
    case "p1":
      return "high";
    case "p2":
      return "medium";
    case "p3":
      return "low";
    default:
      return "medium";
  }
}

function cardResolutionFromRow(card) {
  const explicit = String(card?.resolution ?? "").trim();
  if (explicit === "done" || explicit === "canceled") {
    return explicit;
  }
  if (explicit === "completed") {
    return "done";
  }
  if (explicit === "unresolved" || explicit === "superseded" || !explicit) {
    // Open card — canonical contract uses null
    return null;
  }

  const status = String(card?.status ?? "").trim();
  if (status === "done") {
    return "done";
  }
  if (status === "cancelled" || status === "archived") {
    return "canceled";
  }

  return null;
}

/**
 * Mock topic typed refs use a short id (strip leading `thread-`) so `topic:` refs do not
 * look like thread ids. Canonical topic rows still use `thread-*` ids for URL/API parity.
 */
export function mockTopicRefSuffixFromThreadId(threadId) {
  const tid = String(threadId ?? "").trim();
  if (!tid) return "";
  return tid.startsWith("thread-") ? tid.slice("thread-".length) : tid;
}

export function mockTopicRefFromThreadId(threadId) {
  const suffix = mockTopicRefSuffixFromThreadId(threadId);
  return suffix ? `topic:${suffix}` : "";
}

function buildCanonicalTopicSeed(thread) {
  const threadId = String(thread?.id ?? "").trim();
  const boardRefs = boards
    .filter((board) => String(board.thread_id ?? "") === threadId)
    .map((board) => `board:${board.id}`);
  const documentRefs = listMockDocuments({ thread_id: threadId }).map(
    (document) => `document:${document.id}`,
  );
  const relatedRefs = [
    `thread:${threadId}`,
    ...(Array.isArray(thread?.key_artifacts)
      ? thread.key_artifacts.map(normalizeMockThreadKeyArtifactToTypedRef)
      : []),
    ...(Array.isArray(thread?.open_cards)
      ? thread.open_cards.map((id) => `card:${id}`)
      : []),
  ].filter(Boolean);

  return {
    id: threadId,
    thread_id: threadId,
    type: topicTypeFromThreadType(thread?.type),
    status: topicStatusFromThreadStatus(thread?.status),
    title: String(thread?.title ?? "").trim(),
    summary: String(thread?.current_summary ?? "").trim(),
    owner_refs: thread?.created_by ? [`actor:${thread.created_by}`] : [],
    board_refs: boardRefs,
    document_refs: documentRefs,
    related_refs: [...new Set(relatedRefs)],
    created_at: thread?.created_at ?? null,
    created_by: thread?.created_by ?? thread?.updated_by ?? "unknown",
    updated_at: thread?.updated_at ?? thread?.created_at ?? null,
    updated_by: thread?.updated_by ?? thread?.created_by ?? "unknown",
    provenance: deepClone(thread?.provenance ?? { sources: [] }),
  };
}

function buildCanonicalCardSeed(card) {
  const threadId = String(card?.thread_id ?? "").trim();
  const boardId = String(card?.board_id ?? "").trim();
  const thread = threadId
    ? threads.find((entry) => entry.id === threadId)
    : null;
  const topicRef = threadId ? mockTopicRefFromThreadId(threadId) : null;
  const threadTypedRef = threadId ? `thread:${threadId}` : null;
  const boardRef = boardId ? `board:${boardId}` : null;
  const documentId = String(card?.document_ref ?? "")
    .replace(/^document:/, "")
    .trim();
  const documentRef = documentId ? `document:${documentId}` : null;
  const summary =
    String(card?.summary ?? "").trim() ||
    String(card?.body ?? "").trim() ||
    String(thread?.current_summary ?? "").trim() ||
    String(thread?.title ?? "").trim() ||
    String(card?.title ?? "").trim();

  const assigneeRefs = normalizeCardRefList(card?.assignee_refs ?? []);
  const resolvedAssigneeRefs =
    assigneeRefs.length > 0
      ? assigneeRefs
      : mockBoardCardAssigneeRefsFromPayload({ assignee: card?.assignee });

  return {
    id: String(card?.id ?? threadId ?? boardId ?? "").trim() || null,
    board_id: boardId || null,
    thread_id: threadId || null,
    board_ref: boardRef,
    topic_ref: topicRef,
    document_ref: documentRef,
    title:
      String(card?.title ?? thread?.title ?? summary ?? "").trim() || summary,
    summary,
    column_key: String(card?.column_key ?? "backlog").trim() || "backlog",
    rank: String(card?.rank ?? "0000").trim() || "0000",
    assignee_refs: deepClone(resolvedAssigneeRefs),
    risk: cardRiskFromThreadPriority(thread?.priority),
    resolution: cardResolutionFromRow(card),
    resolution_refs: Array.isArray(card?.resolution_refs)
      ? deepClone(card.resolution_refs)
      : [],
    related_refs: [
      boardRef,
      topicRef,
      threadTypedRef,
      documentRef,
      ...(Array.isArray(card?.related_refs) ? card.related_refs : []),
    ].filter(Boolean),
    created_at: card?.created_at ?? null,
    created_by: card?.created_by ?? thread?.created_by ?? "unknown",
    updated_at: card?.updated_at ?? card?.created_at ?? null,
    updated_by: card?.updated_by ?? card?.created_by ?? "unknown",
    provenance: deepClone(card?.provenance ?? { sources: [] }),
  };
}

function buildCanonicalBoardSeed(board) {
  const boardId = String(board?.id ?? "").trim();
  const backingThreadId = String(board?.thread_id ?? "").trim();
  const cardRefs = boardCards
    .filter((card) => String(card?.board_id ?? "") === boardId)
    .map((card) => `card:${String(card?.id ?? card?.thread_id ?? "").trim()}`)
    .filter(Boolean);
  const rawRefs = Array.isArray(board?.refs) ? board.refs : [];
  const documentRefs = [
    ...rawRefs.filter((r) => String(r).startsWith("document:")),
    ...listMockDocuments({ thread_id: backingThreadId }).map(
      (document) => `document:${document.id}`,
    ),
  ].filter(Boolean);

  return {
    ...deepClone(board),
    document_refs: [...new Set(documentRefs)],
    card_refs: [...new Set(cardRefs)],
  };
}

function buildCanonicalPacketSeed(artifact) {
  const packet = artifact?.packet;
  if (!packet || typeof packet !== "object") {
    return null;
  }

  const subjectRef = String(packet.subject_ref ?? "").trim() || null;
  const packetId = String(
    packet.receipt_id ?? packet.review_id ?? artifact?.id ?? "",
  ).trim();

  return {
    id: packetId || String(artifact?.id ?? "").trim() || null,
    kind: String(artifact?.kind ?? "").trim(),
    subject_ref: subjectRef,
    artifact: deepClone(artifact),
    packet: deepClone(packet),
  };
}

export function getMockSeedData() {
  return {
    actors: deepClone(actors),
    topics: deepClone(threads.map(buildCanonicalTopicSeed)),
    boards: deepClone(boards.map(buildCanonicalBoardSeed)),
    cards: deepClone(boardCards.map(buildCanonicalCardSeed)),
    packets: deepClone(artifacts.map(buildCanonicalPacketSeed).filter(Boolean)),
    threads: deepClone(threads),
    documents: deepClone(MOCK_DOCUMENTS),
    documentRevisions: deepClone(MOCK_DOCUMENT_REVISIONS),
    artifacts: deepClone(artifacts),
    boardCards: deepClone(boardCards),
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

function splitLegacyTypedRef(refValue) {
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

function normalizeMockInboxCategory(category) {
  const normalized = String(category ?? "").trim();
  return normalized;
}

function deriveMockInboxSubjectRef(item) {
  const explicit = String(item?.subject_ref ?? "").trim();
  if (explicit) {
    return explicit;
  }

  const topicId = String(item?.topic_id ?? "").trim();
  if (topicId) {
    return mockTopicRefFromThreadId(topicId);
  }

  const cardId = String(item?.card_id ?? item?.work_item_id ?? "").trim();
  if (cardId) {
    return `card:${cardId}`;
  }

  const boardId = String(item?.board_id ?? "").trim();
  if (boardId) {
    return `board:${boardId}`;
  }

  const documentId = String(item?.document_id ?? "").trim();
  if (documentId) {
    return `document:${documentId}`;
  }

  const threadId = String(item?.thread_id ?? "").trim();
  if (threadId) {
    return `thread:${threadId}`;
  }

  return "";
}

function normalizeMockInboxItem(item) {
  const rest = { ...item };
  delete rest.refs;
  const subjectRef = deriveMockInboxSubjectRef(rest);
  const relatedRefs = Array.isArray(rest?.related_refs)
    ? rest.related_refs
    : [];

  return {
    ...rest,
    category: normalizeMockInboxCategory(rest?.category),
    subject_ref: subjectRef,
    related_refs:
      relatedRefs.length > 0
        ? relatedRefs
        : rest?.thread_id
          ? [`thread:${rest.thread_id}`]
          : [],
    subject_kind: splitLegacyTypedRef(subjectRef).prefix,
    subject_id: splitLegacyTypedRef(subjectRef).id,
  };
}

export function listMockInboxItems() {
  return inboxItems
    .filter((item) => !item.acknowledged_at)
    .map((item) => normalizeMockInboxItem(item));
}

export function ackMockInboxItem({ subject_ref, inbox_item_id }) {
  const correlation = resolveMockSubjectBackingThreadId(subject_ref);
  if (!correlation) {
    return null;
  }
  const item = inboxItems.find(
    (item) =>
      item.id === inbox_item_id &&
      String(item.thread_id) === String(correlation),
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

  const cardRow = boardCards.find((card) => {
    const normalized = normalizeMockBoardCard(card);
    return normalized && String(normalized.id) === String(snapshotId);
  });
  if (cardRow) {
    return {
      ...normalizeMockBoardCard(cardRow),
      kind: "card",
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

/** Bare artifact ids in thread.key_artifacts → `artifact:<id>` for topic related_refs and workspace. */
function normalizeMockThreadKeyArtifactToTypedRef(refValue) {
  const trimmed = String(refValue ?? "").trim();
  if (!trimmed) {
    return trimmed;
  }
  if (isTypedRef(trimmed)) {
    return trimmed;
  }
  return `artifact:${trimmed}`;
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
    open_cards: Array.isArray(thread.open_cards) ? thread.open_cards : [],
  };

  threads.unshift(created);
  return created;
}

export function getMockThread(threadId) {
  return threads.find((thread) => thread.id === threadId) ?? null;
}

export function listMockTopics(filters = {}) {
  return listMockThreads(filters).map((thread) => ({
    id: thread.id,
    type: thread.type ?? "other",
    status: topicStatusFromThreadStatus(thread.status),
    title: thread.title,
    summary: thread.current_summary ?? "",
    owner_refs: thread.created_by ? [`actor:${thread.created_by}`] : [],
    thread_id: thread.id,
    document_refs: listMockDocuments({ thread_id: thread.id }).map(
      (doc) => `document:${doc.id}`,
    ),
    board_refs: boards
      .filter((board) => board.thread_id === thread.id)
      .map((board) => `board:${board.id}`),
    related_refs: [
      ...new Set([
        ...(thread.key_artifacts ?? []).map(
          normalizeMockThreadKeyArtifactToTypedRef,
        ),
        ...(thread.open_cards ?? []).map((id) => `card:${id}`),
        `thread:${thread.id}`,
      ]),
    ].filter(Boolean),
    created_at: thread.created_at,
    created_by: thread.created_by,
    updated_at: thread.updated_at,
    updated_by: thread.updated_by,
    provenance: thread.provenance,
  }));
}

export function getMockTopic(topicId) {
  const id = String(topicId ?? "").trim();
  if (!id) return null;
  const topics = listMockTopics();
  const direct = topics.find((topic) => topic.id === id);
  if (direct) return direct;
  if (!id.startsWith("thread-")) {
    const candidate = `thread-${id}`;
    return topics.find((topic) => topic.id === candidate) ?? null;
  }
  return null;
}

function listMockOpenCardsForThread(threadId) {
  const tid = String(threadId ?? "").trim();
  if (!tid) {
    return [];
  }

  const threadRefs = new Set(
    [`thread:${tid}`, `topic:${tid}`, mockTopicRefFromThreadId(tid)].filter(
      Boolean,
    ),
  );

  return boardCards
    .filter((card) => {
      if (!isVisibleBoardCard(card)) {
        return false;
      }
      const cid = String(card.thread_id ?? "").trim();
      if (cid === tid) {
        return true;
      }
      const refs = normalizeRefList(card.related_refs);
      return refs.some((ref) => threadRefs.has(ref));
    })
    .map((card) => normalizeMockBoardCard(card))
    .filter((row) => {
      if (!row) return false;
      const r = String(row.resolution ?? "").trim();
      return !r || r === "unresolved";
    });
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
  openCards,
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
    open_cards: openCards,
    recommendation_count: recommendations.length,
    decision_request_count: decisionRequests.length,
    decision_count: decisions.length,
    artifact_count: keyArtifacts.length,
    open_card_count: openCards.length,
  };
}

function mockThreadWorkspaceRefreshCommand(threadId, thread) {
  const ref = String(thread?.topic_ref ?? thread?.subject_ref ?? "").trim();
  const m = ref.match(/^topic:(.+)$/);
  if (m && String(m[1] ?? "").trim()) {
    const refId = String(m[1]).trim();
    const topicRow = getMockTopic(refId);
    const topicId = String(topicRow?.id ?? refId).trim();
    return `oar topics workspace --topic-id ${topicId} --include-artifact-content --full-id --json`;
  }
  return `oar threads workspace --thread-id ${threadId} --include-artifact-content --full-id --json`;
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
  const openCards = listMockOpenCardsForThread(threadId);
  const documents = listMockDocuments({ thread_id: threadId });
  const keyArtifacts = normalizeRefList(thread.key_artifacts).map((ref) => {
    const normalizedRef = normalizeMockThreadKeyArtifactToTypedRef(ref);
    const { prefix, id } = splitTypedRef(normalizedRef);
    const artifact = prefix === "artifact" ? getMockArtifact(id) : null;
    const item = { ref: normalizedRef, artifact };
    if (include_artifact_content && artifact?.content_text) {
      item.content_preview = artifactContentPreview(artifact.content_text);
    }
    return item;
  });
  const collaboration = buildMockWorkspaceCollaboration(
    recentEvents,
    keyArtifacts,
    openCards,
  );

  const boardMemberships = boardCards
    .filter(
      (c) =>
        String(c.thread_id ?? "").trim() === String(threadId).trim() &&
        isVisibleBoardCard(c),
    )
    .map((card) => {
      const board = boards.find((b) => b.id === card.board_id);
      if (!board) return null;
      return {
        board: {
          id: board.id,
          title: board.title,
          status: board.status,
        },
        card: {
          board_id: card.board_id,
          ...(card.id ? { id: card.id } : {}),
          thread_id: card.thread_id,
          column_key: card.column_key,
          document_ref: card.document_ref ?? null,
        },
      };
    })
    .filter(Boolean);

  const ownedBoards = boards
    .filter((b) => b.thread_id === threadId)
    .map((b) => ({
      id: b.id,
      title: b.title,
      status: b.status,
      card_count: boardCards.filter(
        (c) => c.board_id === b.id && isVisibleBoardCard(c),
      ).length,
      updated_at: b.updated_at,
    }));

  return {
    thread_id: threadId,
    thread,
    context: {
      recent_events: recentEvents,
      key_artifacts: keyArtifacts,
      open_cards: openCards,
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
    owned_boards: {
      items: ownedBoards,
      count: ownedBoards.length,
    },
    board_memberships: {
      items: boardMemberships,
      count: boardMemberships.length,
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
      workspace_refresh_command: mockThreadWorkspaceRefreshCommand(
        threadId,
        thread,
      ),
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
    if (field === "open_cards") {
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

function resolveMockSubjectBackingThreadId(subjectRef) {
  const normalizedRef = String(subjectRef ?? "").trim();
  if (!normalizedRef) {
    return "";
  }
  if (normalizedRef.startsWith("thread:")) {
    return normalizedRef.slice("thread:".length).trim();
  }
  if (normalizedRef.startsWith("topic:")) {
    const topicId = normalizedRef.slice("topic:".length).trim();
    return String(getMockTopic(topicId)?.thread_id ?? "").trim();
  }
  if (normalizedRef.startsWith("document:")) {
    const documentId = normalizedRef.slice("document:".length).trim();
    return String(getMockDocument(documentId)?.thread_id ?? "").trim();
  }
  if (normalizedRef.startsWith("board:")) {
    const boardId = normalizedRef.slice("board:".length).trim();
    return String(getMockBoard(boardId)?.thread_id ?? "").trim();
  }
  if (normalizedRef.startsWith("card:")) {
    const cardId = normalizedRef.slice("card:".length).trim();
    const card = getMockCard(cardId);
    const cardThreadId = String(card?.thread_id ?? "").trim();
    let firstThreadId = "";
    for (const ref of card?.related_refs ?? []) {
      const { prefix, id } = splitTypedRef(String(ref ?? "").trim());
      if (prefix !== "thread" || !id) {
        continue;
      }
      if (!firstThreadId) {
        firstThreadId = id;
      }
      if (id !== cardThreadId) {
        return id;
      }
    }
    return firstThreadId || cardThreadId;
  }
  return "";
}

export function listMockArtifacts(filters = {}) {
  const trashedOnly =
    filters.trashed_only === true || String(filters.trashed_only) === "true";
  const includeTrashed =
    trashedOnly ||
    filters.include_trashed === true ||
    String(filters.include_trashed) === "true";

  return artifacts.filter((artifact) => {
    const isTrashed =
      artifact.trashed_at != null && String(artifact.trashed_at).trim() !== "";

    if (trashedOnly) {
      if (!isTrashed) {
        return false;
      }
    } else if (!includeTrashed && isTrashed) {
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

export function normalizePacketShapeForClient(packet, kind) {
  if (!packet || typeof packet !== "object") return packet;
  const p = { ...packet };
  delete p.thread_id;
  const k = String(kind ?? "");
  if (k === "review") {
    const rid = String(p.receipt_id ?? "").trim();
    if (rid && !String(p.receipt_ref ?? "").trim()) {
      p.receipt_ref = `artifact:${rid}`;
    }
  }
  return p;
}

export function artifactForApiResponse(artifact) {
  if (!artifact) return null;
  const base = { ...artifact };
  const k = String(base.kind ?? "");
  if (base.packet && (k === "receipt" || k === "review")) {
    base.packet = normalizePacketShapeForClient(base.packet, k);
  }
  return base;
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
      content: normalizePacketShapeForClient(artifact.packet, artifact.kind),
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
  if (!filters.include_trashed) {
    docs = docs.filter((d) => !d.trashed_at);
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
    trashed_at: null,
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
  if (Object.prototype.hasOwnProperty.call(docPatch, "thread_id")) {
    updatedDoc.thread_id = docPatch.thread_id || null;
  }

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
  const subjectRef = String(packet.subject_ref ?? "").trim();

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

  if (!subjectRef) {
    return { error: "validation", message: "packet.subject_ref is required." };
  }

  if (!subjectRef.startsWith("card:")) {
    return {
      error: "validation",
      message: "packet.subject_ref must be a card ref (card:...).",
    };
  }

  const backingThreadId = resolveMockSubjectBackingThreadId(subjectRef);
  if (!backingThreadId) {
    return {
      error: "validation",
      message: "packet.subject_ref must resolve to a backing thread.",
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

  const artifactRefs = normalizeRefList(artifact.refs);

  if (!artifactRefs.includes(subjectRef)) {
    return {
      error: "validation",
      message: "artifact.refs must include packet.subject_ref.",
    };
  }

  const summaryFallback = changesSummary.slice(0, 120);

  const createdArtifact = {
    id: artifactId,
    kind: "receipt",
    thread_id: backingThreadId,
    summary: String(artifact.summary ?? summaryFallback).trim(),
    refs: artifactRefs,
    created_at: new Date().toISOString(),
    created_by: actor_id,
    provenance: {
      sources: ["actor_statement:ui"],
    },
    packet: {
      receipt_id: packetId,
      subject_ref: subjectRef,
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
    thread_id: backingThreadId,
    refs: [`artifact:${artifactId}`, subjectRef],
    summary: `Receipt added: ${createdArtifact.summary}`,
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

export function createMockReview({ actor_id, artifact = {}, packet = {} }) {
  const artifactId = String(artifact.id ?? "").trim();
  const packetId = String(packet.review_id ?? "").trim();
  const receiptRef =
    String(packet.receipt_ref ?? "").trim() ||
    (String(packet.receipt_id ?? "").trim()
      ? `artifact:${String(packet.receipt_id).trim()}`
      : "");
  const receiptId = receiptRef.startsWith("artifact:")
    ? receiptRef.slice("artifact:".length).trim()
    : String(packet.receipt_id ?? "").trim();
  const subjectRef = String(packet.subject_ref ?? "").trim();

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

  if (!subjectRef) {
    return { error: "validation", message: "packet.subject_ref is required." };
  }

  if (!subjectRef.startsWith("card:")) {
    return {
      error: "validation",
      message: "packet.subject_ref must be a card ref (card:...).",
    };
  }

  const backingThreadId = resolveMockSubjectBackingThreadId(subjectRef);
  if (!backingThreadId) {
    return {
      error: "validation",
      message: "packet.subject_ref must resolve to a backing thread.",
    };
  }

  if (!receiptId || !receiptRef) {
    return { error: "validation", message: "packet.receipt_ref is required." };
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

  if (evidenceRefs.length === 0) {
    return {
      error: "validation",
      message: "packet.evidence_refs must include at least one typed ref.",
    };
  }

  if (evidenceRefs.some((refValue) => !isTypedRef(refValue))) {
    return {
      error: "validation",
      message: "packet.evidence_refs contains invalid typed refs.",
    };
  }

  const artifactRefs = normalizeRefList(artifact.refs);

  if (
    !artifactRefs.includes(subjectRef) ||
    !artifactRefs.includes(receiptRef)
  ) {
    return {
      error: "validation",
      message: "artifact.refs must include subject_ref and receipt_ref.",
    };
  }

  const createdArtifact = {
    id: artifactId,
    kind: "review",
    thread_id: backingThreadId,
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
      subject_ref: subjectRef,
      receipt_ref: receiptRef,
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
    thread_id: backingThreadId,
    refs: [`artifact:${artifactId}`, receiptRef, subjectRef],
    summary: `Review completed (${outcome})`,
    payload: {
      artifact_id: artifactId,
      receipt_id: receiptId,
      outcome,
    },
    provenance: {
      sources: ["actor_statement:ui"],
    },
  };

  events.push(createdEvent);

  return { artifact: createdArtifact, event: createdEvent };
}

// ─── Board fixtures ─────────────────────────────────────────────────────────────

const canonicalColumnSchema = [
  { key: "backlog", title: "Backlog", wip_limit: null },
  { key: "ready", title: "Ready", wip_limit: null },
  { key: "in_progress", title: "In Progress", wip_limit: 3 },
  { key: "blocked", title: "Blocked", wip_limit: null },
  { key: "review", title: "Review", wip_limit: 2 },
  { key: "done", title: "Done", wip_limit: null },
];
const canonicalBoardColumnKeys = new Set(
  canonicalColumnSchema.map((column) => column.key),
);

const boards = [
  {
    id: "board-product-launch",
    title: "Q2 Product Launch",
    status: "active",
    labels: ["product", "launch", "q2"],
    owners: ["actor-ops-ai"],
    thread_id: "thread-q2-initiative",
    refs: [
      "document:product-constitution",
      "thread:thread-q2-initiative",
      mockTopicRefFromThreadId("thread-q2-initiative"),
    ],
    column_schema: canonicalColumnSchema,
    pinned_refs: ["thread:thread-q2-initiative"],
    created_at: new Date(now - 14 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    updated_at: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
  },
  {
    id: "board-supply-crisis",
    title: "Supply Chain Crisis Response",
    status: "active",
    labels: ["supply-chain", "incident", "critical"],
    owners: ["actor-ops-ai", "actor-supply-rover"],
    thread_id: "thread-lemon-shortage",
    refs: [
      "artifact:artifact-supplier-sla",
      "document:incident-response-playbook",
      "thread:thread-lemon-shortage",
      mockTopicRefFromThreadId("thread-lemon-shortage"),
    ],
    column_schema: canonicalColumnSchema,
    pinned_refs: [
      "thread:thread-lemon-shortage",
      "artifact:artifact-supplier-sla",
    ],
    created_at: new Date(now - 18 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    updated_at: new Date(now - 1 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-supply-rover",
  },
  {
    id: "board-summer-menu",
    title: "Summer Menu Launch",
    status: "active",
    labels: ["product", "menu", "q2"],
    owners: ["actor-flavor-ai", "actor-cashier-bot"],
    thread_id: "thread-summer-menu",
    refs: [
      "document:onboarding-guide-v1",
      "thread:thread-summer-menu",
      mockTopicRefFromThreadId("thread-summer-menu"),
    ],
    column_schema: canonicalColumnSchema,
    pinned_refs: ["thread:thread-summer-menu"],
    created_at: new Date(now - 5 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-flavor-ai",
    updated_at: new Date(now - 3 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-flavor-ai",
  },
];

const boardCards = [
  {
    board_id: "board-product-launch",
    thread_id: "thread-summer-menu",
    column_key: "ready",
    rank: "0001",
    document_ref: "document:onboarding-guide-v1",
    created_at: new Date(now - 4 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-flavor-ai",
    updated_at: new Date(now - 4 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-flavor-ai",
  },
  {
    board_id: "board-product-launch",
    thread_id: "thread-daily-ops",
    column_key: "in_progress",
    rank: "0002",
    document_ref: null,
    created_at: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    updated_at: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
  },
  {
    board_id: "board-supply-crisis",
    thread_id: "thread-daily-ops",
    column_key: "ready",
    rank: "0001",
    document_ref: null,
    created_at: new Date(now - 18 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    updated_at: new Date(now - 18 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
  },
  {
    board_id: "board-supply-crisis",
    thread_id: "thread-squeezebot-maintenance",
    column_key: "blocked",
    rank: "0002",
    document_ref: "document:incident-response-playbook",
    created_at: new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-squeeze-bot",
    updated_at: new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-squeeze-bot",
  },
  {
    board_id: "board-summer-menu",
    thread_id: "thread-onboarding",
    column_key: "backlog",
    rank: "0001",
    document_ref: "document:onboarding-guide-v1",
    created_at: new Date(now - 5 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-flavor-ai",
    updated_at: new Date(now - 5 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-flavor-ai",
  },
  {
    board_id: "board-summer-menu",
    thread_id: "thread-pricing-glitch",
    column_key: "done",
    rank: "0001",
    document_ref: null,
    created_at: new Date(now - 10 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    updated_at: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
  },
  {
    id: "card-pricing-audit",
    board_id: "board-summer-menu",
    column_key: "done",
    rank: "0002",
    status: "cancelled",
    summary: "Full historical pricing audit for March (canceled)",
    related_refs: ["thread:thread-pricing-glitch"],
    created_at: new Date(now - 10 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    updated_at: new Date(
      now - 7 * 24 * 60 * 60 * 1000 + 30 * 60 * 1000,
    ).toISOString(),
    updated_by: "actor-ops-ai",
  },
  {
    id: "card-emergency-restock",
    board_id: "board-supply-crisis",
    column_key: "in_progress",
    rank: "0003",
    summary:
      "Place emergency lemon restock order with approved backup supplier",
    related_refs: [
      "thread:thread-lemon-shortage",
      "artifact:artifact-supplier-sla",
    ],
    assignee_refs: ["actor:actor-supply-rover"],
    due_at: new Date(now + 2 * 60 * 60 * 1000).toISOString(),
    created_at: new Date(now - 18 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-supply-rover",
    updated_at: new Date(now - 1 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-supply-rover",
  },
  {
    id: "card-sla-review",
    board_id: "board-supply-crisis",
    column_key: "ready",
    rank: "0004",
    summary: "File SLA breach report with CitrusBot Farm for today's outage",
    related_refs: [
      "thread:thread-lemon-shortage",
      "artifact:artifact-supplier-sla",
    ],
    assignee_refs: ["actor:actor-ops-ai"],
    due_at: new Date(now + 5 * 24 * 60 * 60 * 1000).toISOString(),
    created_at: new Date(now - 14 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    updated_at: new Date(now - 14 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
  },
  {
    id: "card-q2-permit",
    board_id: "board-product-launch",
    column_key: "ready",
    rank: "0003",
    summary: "Confirm city permit approval for Riverside Park Stand #2",
    related_refs: ["thread:thread-q2-initiative"],
    created_at: new Date(now - 14 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    updated_at: new Date(now - 14 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
  },
  {
    id: "card-q2-menu",
    board_id: "board-product-launch",
    column_key: "backlog",
    rank: "0004",
    summary: "FlavorMind to draft Riverside Park seasonal menu by April 1",
    related_refs: ["thread:thread-q2-initiative"],
    created_at: new Date(now - 14 * 24 * 60 * 60 * 1000).toISOString(),
    created_by: "actor-ops-ai",
    updated_at: new Date(now - 14 * 24 * 60 * 60 * 1000).toISOString(),
    updated_by: "actor-ops-ai",
  },
];

function cloneBoard(board) {
  if (!board) return null;

  return {
    ...board,
    labels: [...(board.labels ?? [])],
    owners: [...(board.owners ?? [])],
    refs: [...(board.refs ?? [])],
    pinned_refs: [...(board.pinned_refs ?? [])],
    column_schema: (board.column_schema ?? canonicalColumnSchema).map(
      (column) => ({
        ...column,
      }),
    ),
  };
}

function boardColumnOrder(columnKey) {
  const index = canonicalColumnSchema.findIndex(
    (column) => column.key === columnKey,
  );
  return index >= 0 ? index : canonicalColumnSchema.length;
}

function sortBoardCardsForBoard(cards) {
  return [...cards].sort((left, right) => {
    const columnDelta =
      boardColumnOrder(left.column_key) - boardColumnOrder(right.column_key);
    if (columnDelta !== 0) return columnDelta;

    const rankDelta =
      Number.parseInt(left.rank ?? "0", 10) -
      Number.parseInt(right.rank ?? "0", 10);
    if (Number.isFinite(rankDelta) && rankDelta !== 0) return rankDelta;

    return String(left.thread_id ?? left.id ?? "").localeCompare(
      String(right.thread_id ?? right.id ?? ""),
    );
  });
}

function normalizeCardRef(prefix, value) {
  const raw = String(value ?? "").trim();
  if (!raw) return null;
  if (raw.includes(":")) return raw;
  return `${prefix}:${raw}`;
}

function normalizeCardRefList(value) {
  if (!Array.isArray(value)) {
    return [];
  }

  const seen = new Set();
  const refs = [];

  for (const item of value) {
    const normalized = String(item ?? "").trim();
    if (!normalized || seen.has(normalized)) {
      continue;
    }
    seen.add(normalized);
    refs.push(normalized);
  }

  return refs;
}

/** Canonical assignee_refs from create payload; accepts legacy scalar assignee. */
function mockBoardCardAssigneeRefsFromPayload(payload) {
  const fromRefs = normalizeCardRefList(payload.assignee_refs ?? []);
  if (fromRefs.length > 0) {
    return fromRefs;
  }
  const raw = payload.assignee;
  if (raw == null || raw === "") {
    return [];
  }
  const s = String(raw).trim();
  if (!s) {
    return [];
  }
  return s.includes(":") ? [s] : [`actor:${s}`];
}

function mockBoardCardLegacyAssigneeFromRefs(refs) {
  const first = normalizeCardRefList(refs ?? [])[0];
  if (!first) {
    return null;
  }
  const { prefix, id } = splitTypedRef(first);
  if (prefix === "actor" && id) {
    return id;
  }
  return first;
}

function isArchivedBoardCard(card) {
  return (
    Boolean(card?.archived_at) ||
    String(card?.status ?? "").trim() === "archived"
  );
}

function isTrashedBoardCard(card) {
  return Boolean(card?.trashed_at);
}

function isVisibleBoardCard(card) {
  return !isArchivedBoardCard(card) && !isTrashedBoardCard(card);
}

function clearBoardCardLifecycle(card) {
  card.archived_at = null;
  card.archived_by = null;
  card.trashed_at = null;
  card.trashed_by = null;
  card.trash_reason = null;
}

function archiveBoardCard(card, actorId, reason = "") {
  const nowIso = new Date().toISOString();
  card.archived_at = nowIso;
  card.archived_by = actorId || card.archived_by || "unknown";
  card.trashed_at = null;
  card.trashed_by = null;
  card.trash_reason = reason || null;
  card.version = (Number(card.version) || 0) + 1;
  card.updated_at = nowIso;
  if (actorId) {
    card.updated_by = actorId;
  }
  return nowIso;
}

function normalizeMockBoardCard(card) {
  if (!card) return null;

  const rawThreadRef = String(card.thread_ref ?? "").trim();
  const parsedThreadRef = splitTypedRef(rawThreadRef);
  const threadId = String(
    card.thread_id ??
      (parsedThreadRef.prefix === "thread" ? parsedThreadRef.id : ""),
  ).trim();
  const thread = threadId ? getMockThread(threadId) : null;
  const documentId = String(card.document_ref ?? "")
    .replace(/^document:/, "")
    .trim();
  const cardId =
    String(card.id ?? "").trim() ||
    (threadId ? threadId : `card-${String(card.board_id ?? "board").trim()}`);
  const resolution = String(card.resolution ?? "").trim();
  const dueAt = String(card.due_at ?? "").trim();
  const definitionOfDone = Array.isArray(card.definition_of_done)
    ? [...card.definition_of_done]
    : [];
  const relatedRefs = normalizeCardRefList(card.related_refs);
  const resolutionRefs = normalizeCardRefList(card.resolution_refs);
  const assigneeRefs = Array.isArray(card.assignee_refs)
    ? [...card.assignee_refs]
    : Array.isArray(card.assignee)
      ? [...card.assignee]
      : card.assignee
        ? [String(card.assignee).trim()]
        : [];

  const rawTopicRef = String(card.topic_ref ?? "").trim();
  const topicRef = rawTopicRef
    ? rawTopicRef.includes(":")
      ? rawTopicRef
      : normalizeCardRef("topic", rawTopicRef)
    : String(thread?.topic_ref ?? "").trim() || "";
  const rawDocumentRef = String(card.document_ref ?? "").trim();
  const documentRef = rawDocumentRef
    ? rawDocumentRef.includes(":")
      ? rawDocumentRef
      : normalizeCardRef("document", rawDocumentRef)
    : normalizeCardRef("document", documentId);
  const boardRef = normalizeCardRef("board", card.board_id);
  const summary =
    String(card.summary ?? "").trim() ||
    String(card.body ?? "").trim() ||
    String(thread?.current_summary ?? "").trim() ||
    String(thread?.title ?? "").trim() ||
    String(card.title ?? "").trim();

  const threadRefForRelated = normalizeCardRef("thread", threadId);
  let nextResolution = null;
  if (resolution === "completed" || resolution === "done") {
    nextResolution = "done";
  } else if (resolution === "canceled" || resolution === "cancelled") {
    nextResolution = "canceled";
  } else if (
    !resolution ||
    resolution === "unresolved" ||
    resolution === "superseded"
  ) {
    const st = String(card.status ?? "").trim();
    if (st === "done") {
      nextResolution = "done";
    } else if (st === "cancelled" || st === "archived") {
      nextResolution = "canceled";
    } else if (String(card.column_key ?? "").trim() === "done") {
      nextResolution = "done";
    }
  }

  const normalized = {
    ...card,
    id: cardId,
    board_ref: boardRef,
    topic_ref: topicRef || null,
    document_ref: documentRef || null,
    archived_at: card.archived_at ?? null,
    archived_by: card.archived_by ?? null,
    trashed_at: card.trashed_at ?? null,
    trashed_by: card.trashed_by ?? null,
    trash_reason: card.trash_reason ?? null,
    title: String(card.title ?? "").trim() || summary,
    summary,
    due_at: dueAt || null,
    definition_of_done: definitionOfDone,
    assignee_refs: assigneeRefs,
    risk: String(card.risk ?? "").trim() || "medium",
    resolution: nextResolution,
    resolution_refs: resolutionRefs,
    related_refs:
      relatedRefs.length > 0
        ? relatedRefs
        : [threadRefForRelated, topicRef || null, documentRef || null].filter(
            Boolean,
          ),
  };
  delete normalized.thread_ref;
  return normalized;
}

function getBoardColumnCards(boardId) {
  const columns = canonicalColumnSchema.reduce((acc, column) => {
    acc[column.key] = [];
    return acc;
  }, {});

  for (const card of sortBoardCardsForBoard(
    boardCards.filter(
      (candidate) =>
        candidate.board_id === boardId && isVisibleBoardCard(candidate),
    ),
  )) {
    if (!columns[card.column_key]) {
      columns[card.column_key] = [];
    }
    columns[card.column_key].push(card);
  }

  return columns;
}

function renormalizeColumnCards(cards) {
  cards.forEach((card, index) => {
    card.rank = String(index + 1).padStart(4, "0");
  });
}

function mockCardMatchesAnchor(card, anchor) {
  const a = String(anchor ?? "").trim();
  if (!a) return false;
  const { prefix, id: anchorId } = splitTypedRef(a);
  const cardId = String(card?.id ?? "").trim();
  const tid = String(card?.thread_id ?? "").trim();
  if (prefix === "card") {
    return cardId === anchorId || tid === anchorId;
  }
  if (prefix === "thread" || prefix === "topic") {
    return tid === anchorId;
  }
  return cardId === a || tid === a;
}

function mockBoardCardStableKey(card) {
  const id = String(card?.id ?? "").trim();
  if (id) return id;
  return String(card?.thread_id ?? "").trim();
}

function newStandaloneMockCardId(explicitId) {
  const ex = String(explicitId ?? "").trim();
  if (ex) return ex;
  const c = globalThis.crypto;
  if (c && typeof c.randomUUID === "function") {
    return c.randomUUID();
  }
  return `card-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

function resolveInsertIndex(cards, payload = {}) {
  const {
    before_card_ref,
    after_card_ref,
    before_card_id,
    after_card_id,
    before_thread_id,
    after_thread_id,
  } = payload;

  const beforeAnchor = before_card_ref ?? before_card_id;
  const afterAnchor = after_card_ref ?? after_card_id;

  if (beforeAnchor) {
    const beforeIndex = cards.findIndex((card) =>
      mockCardMatchesAnchor(card, beforeAnchor),
    );
    if (beforeIndex >= 0) return beforeIndex;
  }

  if (afterAnchor) {
    const afterIndex = cards.findIndex((card) =>
      mockCardMatchesAnchor(card, afterAnchor),
    );
    if (afterIndex >= 0) return afterIndex + 1;
  }

  if (before_thread_id) {
    const beforeIndex = cards.findIndex((card) =>
      mockCardMatchesAnchor(card, before_thread_id),
    );
    if (beforeIndex >= 0) return beforeIndex;
  }

  if (after_thread_id) {
    const afterIndex = cards.findIndex((card) =>
      mockCardMatchesAnchor(card, after_thread_id),
    );
    if (afterIndex >= 0) return afterIndex + 1;
  }

  return cards.length;
}

function buildBoardSummary(board) {
  const cards = boardCards.filter(
    (card) => card.board_id === board.id && isVisibleBoardCard(card),
  );
  const cardsByColumn = canonicalColumnSchema.reduce((counts, column) => {
    counts[column.key] = 0;
    return counts;
  }, {});

  let latestActivityAt = board.updated_at ?? null;
  let unresolvedCardCount = 0;
  let resolvedCardCount = 0;
  let documentCount = 0;
  let openCardCount = 0;
  const threadIds = collectMockBoardWorkspaceThreadIds(
    board.id,
    board.thread_id,
  );

  for (const card of cards) {
    cardsByColumn[card.column_key] = (cardsByColumn[card.column_key] ?? 0) + 1;
    const normalized = normalizeMockBoardCard(card);
    const res = String(normalized?.resolution ?? "").trim();
    if (!res || res === "unresolved") {
      unresolvedCardCount += 1;
    } else {
      resolvedCardCount += 1;
    }
  }

  for (const threadId of threadIds) {
    const thread = getMockThread(threadId);
    openCardCount += Array.isArray(thread?.open_cards)
      ? thread.open_cards.length
      : 0;
    if (thread?.updated_at) {
      if (
        !latestActivityAt ||
        Date.parse(thread.updated_at) > Date.parse(latestActivityAt)
      ) {
        latestActivityAt = thread.updated_at;
      }
    }
    documentCount += listMockDocuments({ thread_id: threadId }).length;
  }

  const inboxCount = listMockInboxItems().filter((item) => {
    const subjectRef = String(item?.subject_ref ?? "").trim();
    if (!subjectRef) {
      return false;
    }
    const { prefix, id } = splitLegacyTypedRef(subjectRef);
    if (prefix === "topic" || prefix === "thread") {
      return threadIds.includes(id);
    }
    if (prefix === "board") {
      return id === board.id;
    }
    return false;
  }).length;

  const relatedTopicCount = threadIds.length;
  const stale = threadIds.some((threadId) =>
    isThreadStale(getMockThread(threadId)),
  );

  return {
    card_count: cards.length,
    cards_by_column: cardsByColumn,
    related_topic_count: relatedTopicCount,
    open_card_count: openCardCount,
    unresolved_card_count: unresolvedCardCount,
    resolved_card_count: resolvedCardCount,
    document_count: documentCount,
    inbox_count: inboxCount,
    stale,
    latest_activity_at: latestActivityAt,
    has_document_ref: (board.refs ?? []).some((r) =>
      String(r).startsWith("document:"),
    ),
  };
}

function buildBoardWorkspaceCard(card) {
  const normalizedCard = normalizeMockBoardCard(card);
  const threadId = String(normalizedCard.thread_id ?? "").trim();
  const thread = threadId ? getMockThread(threadId) : null;

  const documentId = String(normalizedCard.document_ref ?? "")
    .replace(/^document:/, "")
    .trim();
  const pinnedDocument = documentId
    ? (getMockDocument(documentId)?.document ?? null)
    : null;
  const relatedTopicCount = new Set(
    normalizeCardRefList(normalizedCard.related_refs)
      .map((ref) => splitTypedRef(ref))
      .filter((ref) => ["topic", "thread", "card"].includes(ref.prefix))
      .map((ref) => `${ref.prefix}:${ref.id}`),
  ).size;
  const documentCount = new Set(
    [
      normalizedCard.document_ref,
      ...normalizeCardRefList(normalizedCard.related_refs).filter(
        (ref) => splitTypedRef(ref).prefix === "document",
      ),
    ].filter(Boolean),
  ).size;

  if (!thread) {
    return {
      membership: { ...normalizedCard },
      backing: {
        thread_id: threadId || null,
        thread: null,
        pinned_document_ref: documentId ? `document:${documentId}` : null,
        pinned_document: pinnedDocument ? { ...pinnedDocument } : null,
      },
      derived: {
        summary: {
          related_topic_count: relatedTopicCount,
          open_card_count: 0,
          document_count: documentCount,
          inbox_count: 0,
          latest_activity_at: normalizedCard.updated_at ?? null,
          stale: false,
        },
        freshness: {
          thread_id: null,
          status: "current",
          generated_at: normalizedCard.updated_at ?? null,
          queued_at: null,
          started_at: null,
          completed_at: normalizedCard.updated_at ?? null,
          last_error_at: null,
          last_error: null,
          materialized: true,
          refresh_in_flight: false,
        },
      },
    };
  }

  const documents = listMockDocuments({ thread_id: threadId });
  const recentEvents = listMockTimelineEvents(threadId);
  const keyArtifacts = normalizeRefList(thread.key_artifacts).map((ref) => ({
    ref: normalizeMockThreadKeyArtifactToTypedRef(ref),
    artifact: null,
  }));
  const openCards = listMockOpenCardsForThread(threadId);
  const collaboration = buildMockWorkspaceCollaboration(
    recentEvents,
    keyArtifacts,
    openCards,
  );
  const inboxCount = listMockInboxItems().filter(
    (item) => String(item.thread_id ?? "") === String(threadId),
  ).length;
  return {
    membership: { ...normalizedCard },
    backing: {
      thread_id: threadId,
      thread: { ...thread },
      pinned_document_ref: documentId ? `document:${documentId}` : null,
      pinned_document: pinnedDocument ? { ...pinnedDocument } : null,
    },
    derived: {
      summary: {
        decision_request_count: collaboration.decision_request_count,
        decision_count: collaboration.decision_count,
        recommendation_count: collaboration.recommendation_count,
        related_topic_count: relatedTopicCount || (thread ? 1 : 0),
        open_card_count: openCards.length,
        document_count: documentCount || documents.length,
        inbox_count: inboxCount,
        latest_activity_at:
          normalizedCard.updated_at ?? thread.updated_at ?? null,
        stale: ["stale", "very-stale"].includes(String(thread.staleness ?? "")),
      },
      freshness: {
        thread_id: threadId,
        status: "current",
        generated_at: normalizedCard.updated_at ?? thread.updated_at ?? null,
        queued_at: null,
        started_at: null,
        completed_at: normalizedCard.updated_at ?? thread.updated_at ?? null,
        last_error_at: null,
        last_error: null,
        materialized: true,
        refresh_in_flight: false,
      },
    },
  };
}

function boardMutationConflict(board, expectedUpdatedAt) {
  if (
    expectedUpdatedAt &&
    String(expectedUpdatedAt) !== String(board.updated_at ?? "")
  ) {
    return {
      error: "conflict",
      message: "Board has been updated by another actor.",
      current: cloneBoard(board),
    };
  }

  return null;
}

function touchBoard(board, actorId) {
  board.updated_at = new Date().toISOString();
  board.updated_by = actorId || board.updated_by || "unknown";
}

function cloneBoardCard(card) {
  return card ? normalizeMockBoardCard(card) : null;
}

export function listMockBoards(filters = {}) {
  let result = [...boards];

  if (filters.status) {
    result = result.filter((board) => board.status === filters.status);
  }
  if (filters.label) {
    const labels = Array.isArray(filters.label)
      ? filters.label
      : [filters.label];
    result = result.filter((board) =>
      labels.some((label) => board.labels.includes(label)),
    );
  }
  if (filters.owner) {
    const owners = Array.isArray(filters.owner)
      ? filters.owner
      : [filters.owner];
    result = result.filter((board) =>
      owners.some((owner) => board.owners.includes(owner)),
    );
  }

  return result.map((board) => ({
    board: cloneBoard(board),
    summary: buildBoardSummary(board),
  }));
}

export function getMockBoard(boardId) {
  const board = boards.find((candidate) => candidate.id === boardId);
  return cloneBoard(board);
}

const MOCK_TOPIC_WORKSPACE_THREAD_TYPES = new Set([
  "initiative",
  "objective",
  "decision",
  "incident",
  "risk",
  "request",
  "note",
  "other",
]);

function mockThreadTypeToTopicWorkspaceType(type) {
  const t = String(type ?? "").trim();
  if (MOCK_TOPIC_WORKSPACE_THREAD_TYPES.has(t)) return t;
  return "other";
}

/** Map thread-shaped status strings to topic workspace `active | paused | closed`. */
function mockThreadStatusToTopicWorkspaceStatus(status) {
  const s = String(status ?? "").trim();
  if (s === "blocked") return "paused";
  if (s === "resolved") return "closed";
  if (s === "active" || s === "paused" || s === "closed") return s;
  return "active";
}

/**
 * Builds a native topic-workspace projection from a threads.workspace-shaped payload.
 * Mock route handlers use this instead of a separate adapter layer.
 */
export function buildMockTopicWorkspaceFromThreadWorkspace(
  ws,
  topicIdOverride,
) {
  if (!ws || typeof ws !== "object") {
    return {
      topic: {},
      cards: [],
      boards: [],
      documents: [],
      threads: [],
      inbox: [],
      projection_freshness: {},
      generated_at: new Date().toISOString(),
    };
  }

  const thread = ws.thread && typeof ws.thread === "object" ? ws.thread : null;
  const context =
    ws.context && typeof ws.context === "object" ? ws.context : {};
  const documents = Array.isArray(context.documents) ? context.documents : [];
  const boardMemberships = Array.isArray(ws.board_memberships?.items)
    ? ws.board_memberships.items
    : [];
  const ownedItems = Array.isArray(ws.owned_boards?.items)
    ? ws.owned_boards.items
    : [];

  const boardsOut = [];
  const boardIds = new Set();

  const threadId = thread ? String(thread.id ?? "").trim() : "";
  const topicId = String(topicIdOverride ?? "").trim() || threadId;
  const topicRefStr = threadId ? mockTopicRefFromThreadId(threadId) : "";

  for (const ob of ownedItems) {
    const bid = String(ob?.id ?? "").trim();
    if (!bid || boardIds.has(bid)) continue;
    boardIds.add(bid);
    const canonicalBoard = getMockBoard(bid);
    const refs = Array.isArray(canonicalBoard?.refs)
      ? [...canonicalBoard.refs]
      : [];
    boardsOut.push({
      id: bid,
      title: ob.title ?? canonicalBoard?.title,
      status: ob.status ?? canonicalBoard?.status,
      refs,
      primary_topic_ref:
        topicRefStr && !refs.some((r) => String(r).trim() === topicRefStr)
          ? topicRefStr
          : "",
      updated_at: ob.updated_at ?? canonicalBoard?.updated_at,
    });
  }

  for (const m of boardMemberships) {
    const b = m?.board;
    const bid = String(b?.id ?? m?.board_id ?? "").trim();
    if (bid && !boardIds.has(bid)) {
      boardIds.add(bid);
      const canonicalBoard = getMockBoard(bid);
      const refs = Array.isArray(canonicalBoard?.refs)
        ? [...canonicalBoard.refs]
        : [];
      boardsOut.push({
        id: bid,
        title: b?.title ?? canonicalBoard?.title,
        status: b?.status ?? canonicalBoard?.status,
        ...(refs.length ? { refs } : {}),
      });
    }
  }

  const cards = [];
  for (const m of boardMemberships) {
    const c = m?.card;
    if (!c || typeof c !== "object") continue;
    const bid = String(c.board_id ?? m?.board?.id ?? "").trim();
    if (!bid) continue;
    cards.push({
      ...c,
      board_id: c.board_id || bid,
      thread_id: c.thread_id || thread?.id,
    });
  }

  const topic = thread
    ? {
        id: topicId,
        type: mockThreadTypeToTopicWorkspaceType(thread.type),
        status: mockThreadStatusToTopicWorkspaceStatus(thread.status),
        title: thread.title,
        summary: String(thread.current_summary ?? ""),
        owner_refs: Array.isArray(thread.owner_refs) ? thread.owner_refs : [],
        thread_id: threadId || null,
        document_refs: Array.isArray(thread.document_refs)
          ? thread.document_refs
          : [],
        board_refs: Array.isArray(thread.board_refs) ? thread.board_refs : [],
        related_refs: Array.isArray(thread.related_refs)
          ? thread.related_refs
          : [],
        created_at: thread.created_at ?? thread.updated_at,
        created_by: thread.created_by ?? thread.updated_by,
        updated_at: thread.updated_at,
        updated_by: thread.updated_by,
        provenance:
          thread.provenance && typeof thread.provenance === "object"
            ? thread.provenance
            : { sources: [] },
      }
    : {};

  const threadWithTopicRef = thread
    ? {
        ...thread,
        topic_ref: topicRefStr || thread.topic_ref,
      }
    : null;

  return {
    topic,
    cards,
    boards: boardsOut,
    documents,
    threads: threadWithTopicRef ? [threadWithTopicRef] : [],
    inbox: Array.isArray(ws.inbox?.items) ? ws.inbox.items : [],
    projection_freshness:
      ws.projection_freshness && typeof ws.projection_freshness === "object"
        ? ws.projection_freshness
        : { aggregate: "unknown" },
    generated_at:
      typeof ws.generated_at === "string"
        ? ws.generated_at
        : new Date().toISOString(),
  };
}

export function getMockTopicWorkspace(topicId, options = {}) {
  const topic = getMockTopic(topicId);
  if (!topic) return null;
  const threadId = String(topic.thread_id ?? topicId).trim();
  const ws = getMockThreadWorkspace(threadId, options);
  if (!ws) return null;
  return buildMockTopicWorkspaceFromThreadWorkspace(ws, topicId);
}

function collectMockBoardWorkspaceThreadIds(boardId, backingThreadId) {
  const seen = new Set();
  const threadIds = [];
  const pushThreadId = (threadId) => {
    const normalized = String(threadId ?? "").trim();
    if (!normalized || seen.has(normalized)) return;
    seen.add(normalized);
    threadIds.push(normalized);
  };

  pushThreadId(backingThreadId);
  for (const card of boardCards) {
    if (card.board_id === boardId && isVisibleBoardCard(card)) {
      pushThreadId(card.thread_id);
    }
  }

  return threadIds;
}

function listMockBoardWorkspaceDocuments(threadIds) {
  const documentsById = new Map();
  for (const threadId of threadIds) {
    for (const document of listMockDocuments({ thread_id: threadId })) {
      documentsById.set(document.id, document);
    }
  }

  return [...documentsById.values()].sort((left, right) => {
    const timeDelta =
      Date.parse(right.updated_at ?? 0) - Date.parse(left.updated_at ?? 0);
    if (timeDelta !== 0) return timeDelta;
    return String(left.id ?? "").localeCompare(String(right.id ?? ""));
  });
}

export function getMockBoardWorkspace(boardId) {
  const board = boards.find((candidate) => candidate.id === boardId);
  if (!board) return null;

  const cards = listMockBoardCards(boardId)
    .map((card) => buildBoardWorkspaceCard(card))
    .filter(Boolean);
  const threadIds = collectMockBoardWorkspaceThreadIds(
    boardId,
    board.thread_id,
  );
  const documents = listMockBoardWorkspaceDocuments(threadIds);
  const generatedAt = new Date().toISOString();
  const freshnessThreads = threadIds.map((threadId) => ({
    thread_id: threadId,
    status: "current",
    generated_at: generatedAt,
    queued_at: null,
    started_at: null,
    completed_at: generatedAt,
    last_error_at: null,
    last_error: null,
    materialized: true,
    refresh_in_flight: false,
  }));

  const backingThreadId = String(board.thread_id ?? "").trim();
  const backingThread = backingThreadId ? getMockThread(backingThreadId) : null;

  return {
    board_id: board.id,
    board: cloneBoard(board),
    backing_thread: backingThread ? { ...backingThread } : null,
    cards: {
      items: cards,
      count: cards.length,
    },
    documents: {
      items: documents,
      count: documents.length,
    },
    inbox: {
      items: [],
      count: 0,
      generated_at: generatedAt,
    },
    board_summary: buildBoardSummary(board),
    projection_freshness: {
      status: "current",
      thread_count: freshnessThreads.length,
      threads: freshnessThreads,
    },
    board_summary_freshness: {
      status: "current",
      thread_count: freshnessThreads.length,
      threads: freshnessThreads,
    },
    warnings: {
      items: [],
      count: 0,
    },
    section_kinds: {
      board: "canonical",
      cards: "convenience",
      documents: "derived",
      inbox: "derived",
      board_summary: "derived",
    },
    generated_at: generatedAt,
  };
}

export function listMockBoardCards(boardId) {
  return sortBoardCardsForBoard(
    boardCards.filter(
      (card) => card.board_id === boardId && isVisibleBoardCard(card),
    ),
  ).map((card) => cloneBoardCard(card));
}

export function listMockCards(filters = {}) {
  const boardFilter = String(filters.board_id ?? "").trim();
  const topicFilter = String(
    filters.topic_id ?? filters.thread_id ?? "",
  ).trim();
  const archivedOnly =
    filters.archived_only === true || String(filters.archived_only) === "true";
  const trashedOnly =
    filters.trashed_only === true || String(filters.trashed_only) === "true";

  return boardCards
    .filter((card) => {
      if (boardFilter && String(card.board_id ?? "") !== boardFilter) {
        return false;
      }
      if (topicFilter && String(card.thread_id ?? "") !== topicFilter) {
        return false;
      }
      if (archivedOnly && !card.archived_at) {
        return false;
      }
      if (trashedOnly && !card.trashed_at) {
        return false;
      }
      return true;
    })
    .map((card) => normalizeMockBoardCard(card));
}

export function getMockCard(cardId) {
  return (
    listMockCards().find((card) => {
      const stableId = String(card?.id ?? "").trim();
      if (stableId === String(cardId ?? "").trim()) {
        return true;
      }
      return (
        String(card?.thread_id ?? "").trim() === String(cardId ?? "").trim()
      );
    }) ?? null
  );
}

function soleThreadIdFromRelatedRefsForCreate(relatedRefs) {
  const ids = [];
  for (const r of relatedRefs ?? []) {
    const s = String(r ?? "").trim();
    if (!s) continue;
    const typed = splitTypedRef(s.includes(":") ? s : `thread:${s}`);
    if (typed.prefix === "thread" && typed.id) {
      ids.push(String(typed.id).trim());
    }
  }
  const uniq = [...new Set(ids)];
  if (uniq.length > 1) {
    return {
      ok: false,
      threadId: "",
      message:
        "related_refs must include at most one thread ref for this board card.",
    };
  }
  return { ok: true, threadId: uniq[0] || "", message: "" };
}

export function createMockBoardCard(boardId, payload) {
  const board = boards.find((candidate) => candidate.id === boardId);
  if (!board) {
    return { error: "not_found", message: `Board not found: ${boardId}` };
  }

  const boardConflict = boardMutationConflict(
    board,
    payload.if_board_updated_at,
  );
  if (boardConflict) {
    return boardConflict;
  }

  const relatedRefsList = Array.isArray(payload.related_refs)
    ? payload.related_refs
    : [];
  const soleRefs = soleThreadIdFromRelatedRefsForCreate(relatedRefsList);
  if (!soleRefs.ok) {
    return { error: "validation", message: soleRefs.message };
  }

  const rawThreadRef = String(payload.thread_ref ?? "").trim();
  const parsedThreadRef = splitTypedRef(rawThreadRef);
  const threadId = String(
    payload.thread_id ??
      (parsedThreadRef.prefix === "thread" ? parsedThreadRef.id : "") ??
      soleRefs.threadId,
  ).trim();
  const title = String(payload.title ?? "").trim();
  if (!title) {
    return { error: "validation", message: "title is required." };
  }

  const explicitSummary =
    String(payload.summary ?? "").trim() ||
    title ||
    String(payload.body ?? "").trim();

  const columnKey = String(payload.column_key || "backlog").trim();
  if (!canonicalBoardColumnKeys.has(columnKey)) {
    return {
      error: "validation",
      message:
        "column_key must be one of: backlog, ready, in_progress, blocked, review, done.",
    };
  }
  const rawDocRef = String(payload.document_ref ?? "").trim();
  const pinnedDocumentId = rawDocRef.replace(/^document:/, "").trim();
  if (pinnedDocumentId && !getMockDocument(pinnedDocumentId)) {
    return {
      error: "not_found",
      message: `Document not found: ${pinnedDocumentId}`,
    };
  }

  if (threadId) {
    if (!getMockThread(threadId)) {
      return { error: "not_found", message: `Thread not found: ${threadId}` };
    }
    if (threadId === board.thread_id) {
      return {
        error: "validation",
        message: "The board backing thread cannot be added as a card.",
      };
    }
    if (
      boardCards.some(
        (card) =>
          card.board_id === boardId &&
          isVisibleBoardCard(card) &&
          String(card.thread_id ?? "").trim() === threadId,
      )
    ) {
      return {
        error: "conflict",
        message: `Thread '${threadId}' is already on board '${boardId}'.`,
        current: cloneBoard(board),
      };
    }
  }

  const columns = getBoardColumnCards(boardId);
  const targetColumn = columns[columnKey] ?? (columns[columnKey] = []);
  const nowIso = new Date().toISOString();
  const cardId = threadId
    ? String(payload.card_id ?? "").trim() || threadId
    : newStandaloneMockCardId(payload.card_id);

  if (
    String(payload.card_id ?? "").trim() &&
    boardCards.some(
      (c) =>
        c.board_id === boardId &&
        isVisibleBoardCard(c) &&
        mockCardMatchesAnchor(c, cardId),
    )
  ) {
    return {
      error: "conflict",
      message: `Card id '${cardId}' is already on board '${boardId}'.`,
      current: cloneBoard(board),
    };
  }

  const assigneeRefs = mockBoardCardAssigneeRefsFromPayload(payload);
  const newCard = normalizeMockBoardCard({
    id: cardId,
    board_id: boardId,
    thread_id: threadId || null,
    topic_ref: payload.topic_ref ?? null,
    document_ref: payload.document_ref ?? null,
    title: String(payload.title ?? "").trim() || "",
    summary: explicitSummary,
    due_at: payload.due_at ?? null,
    definition_of_done: Array.isArray(payload.definition_of_done)
      ? payload.definition_of_done
      : [],
    body: String(payload.body ?? "").trim() || "",
    column_key: columnKey,
    rank: "0000",
    version: 1,
    assignee_refs: assigneeRefs,
    risk: String(payload.risk ?? "").trim() || "medium",
    resolution: (() => {
      const r = String(payload.resolution ?? "").trim();
      if (!r) return null;
      if (r === "completed") return "done";
      if (r === "done" || r === "canceled") return r;
      return null;
    })(),
    resolution_refs: Array.isArray(payload.resolution_refs)
      ? payload.resolution_refs
      : [],
    related_refs: Array.isArray(payload.related_refs)
      ? payload.related_refs
      : [],
    assignee:
      assigneeRefs.length > 0
        ? mockBoardCardLegacyAssigneeFromRefs(assigneeRefs)
        : payload.assignee == null || String(payload.assignee).trim() === ""
          ? null
          : String(payload.assignee).trim(),
    priority: payload.priority ?? null,
    status: payload.status ?? "todo",
    created_at: nowIso,
    created_by: payload.actor_id || "unknown",
    updated_at: nowIso,
    updated_by: payload.actor_id || "unknown",
    archived_at: null,
    archived_by: null,
    trashed_at: null,
    trashed_by: null,
    trash_reason: null,
  });

  targetColumn.splice(resolveInsertIndex(targetColumn, payload), 0, newCard);
  renormalizeColumnCards(targetColumn);
  boardCards.push(newCard);
  touchBoard(board, payload.actor_id);

  return {
    board: cloneBoard(board),
    card: cloneBoardCard(newCard),
  };
}

export function updateMockBoardCardByGlobalCardId(cardId, payload) {
  const row = boardCards.find((candidate) =>
    mockCardMatchesAnchor(candidate, cardId),
  );
  if (!row) {
    return {
      error: "not_found",
      message: `Card not found: ${cardId}`,
    };
  }
  return updateMockBoardCard(row.board_id, cardId, payload);
}

export function updateMockBoardCard(boardId, cardId, payload) {
  const board = boards.find((candidate) => candidate.id === boardId);
  if (!board) {
    return { error: "not_found", message: `Board not found: ${boardId}` };
  }

  const card = boardCards.find(
    (candidate) =>
      candidate.board_id === boardId &&
      mockCardMatchesAnchor(candidate, cardId),
  );
  if (!card) {
    return {
      error: "not_found",
      message: `Card not found: ${cardId} on board ${boardId}`,
    };
  }

  const boardConflict = boardMutationConflict(
    board,
    payload.if_board_updated_at,
  );
  if (boardConflict) {
    return boardConflict;
  }

  const patch = payload.patch ?? {};
  let mutated = false;

  if (Object.prototype.hasOwnProperty.call(patch, "title")) {
    const value = String(patch.title ?? "").trim();
    if (value === "") {
      return {
        error: "validation",
        message: "patch.title must not be empty",
      };
    }
    if (card.summary !== value) {
      card.summary = value;
      mutated = true;
    }
    if (card.title !== value) {
      card.title = value;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "body")) {
    const value = String(patch.body ?? "").trim();
    if (card.body !== value) {
      card.body = value;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "summary")) {
    const value = String(patch.summary ?? "").trim();
    if (card.summary !== value) {
      card.summary = value;
      mutated = true;
    }
    if (card.title !== value) {
      card.title = value;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "due_at")) {
    const value = String(patch.due_at ?? "").trim();
    const next = value || null;
    if (card.due_at !== next) {
      card.due_at = next;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "definition_of_done")) {
    const next = Array.isArray(patch.definition_of_done)
      ? [...patch.definition_of_done]
      : [];
    const current = Array.isArray(card.definition_of_done)
      ? card.definition_of_done
      : [];
    if (JSON.stringify(current) !== JSON.stringify(next)) {
      card.definition_of_done = next;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "assignee")) {
    const value = patch.assignee == null ? "" : String(patch.assignee).trim();
    const next = value || null;
    if (card.assignee !== next) {
      card.assignee = next;
      mutated = true;
    }
    const refsNext = next
      ? next.includes(":")
        ? normalizeCardRefList([next])
        : normalizeCardRefList([`actor:${next}`])
      : [];
    const current = normalizeCardRefList(card.assignee_refs);
    if (JSON.stringify(current) !== JSON.stringify(refsNext)) {
      card.assignee_refs = refsNext;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "priority")) {
    const value = patch.priority == null ? "" : String(patch.priority).trim();
    const next = value || null;
    if (card.priority !== next) {
      card.priority = next;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "assignee_refs")) {
    const next = normalizeCardRefList(patch.assignee_refs);
    const current = normalizeCardRefList(card.assignee_refs);
    if (JSON.stringify(current) !== JSON.stringify(next)) {
      card.assignee_refs = next;
      mutated = true;
    }
    const legacyNext = mockBoardCardLegacyAssigneeFromRefs(next);
    if (card.assignee !== legacyNext) {
      card.assignee = legacyNext;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "risk")) {
    const value = String(patch.risk ?? "").trim();
    const next = value || "medium";
    if (card.risk !== next) {
      card.risk = next;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "status")) {
    const value = String(patch.status ?? "").trim();
    if (!["todo", "in_progress", "done", "cancelled"].includes(value)) {
      return {
        error: "validation",
        message:
          "patch.status must be one of: todo, in_progress, done, cancelled",
      };
    }
    if (card.status !== value) {
      card.status = value;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "document_ref")) {
    const value = String(patch.document_ref ?? "").trim();
    const normalized = value ? value.replace(/^document:/, "") : "";
    if (normalized && !getMockDocument(normalized)) {
      return {
        error: "not_found",
        message: `Document not found: ${normalized}`,
      };
    }
    const next = normalized || null;
    const nextRef = next ? `document:${next}` : null;
    if (card.document_ref !== nextRef) {
      card.document_ref = nextRef;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "topic_ref")) {
    const value = String(patch.topic_ref ?? "").trim();
    const next = value || null;
    if (card.topic_ref !== next) {
      card.topic_ref = next;
      mutated = true;
    }
  }

  function peekThreadIdFromAnchorRaw(raw) {
    if (raw === null || raw === undefined || raw === "") {
      return { ok: true, id: "" };
    }
    const value = String(raw ?? "").trim();
    const typed = splitTypedRef(value);
    if (value && typed.prefix === "thread") {
      return { ok: true, id: typed.id };
    }
    if (value && !typed.prefix) {
      return { ok: true, id: value };
    }
    if (!value) {
      return { ok: true, id: "" };
    }
    return { ok: false, id: "" };
  }

  function validateCardThreadChange(nextThreadId) {
    const normalized = String(nextThreadId ?? "").trim();
    const current = String(card.thread_id ?? "").trim();
    if (normalized === current) return null;
    if (normalized) {
      if (!getMockThread(normalized)) {
        return {
          error: "not_found",
          message: `Thread not found: ${normalized}`,
        };
      }
      if (normalized === board.thread_id) {
        return {
          error: "validation",
          message: "board.thread_id cannot be added as a board card",
        };
      }
      const duplicate = boardCards.some(
        (c) =>
          c.board_id === boardId &&
          c !== card &&
          isVisibleBoardCard(c) &&
          String(c.thread_id ?? "").trim() === normalized,
      );
      if (duplicate) {
        return {
          error: "conflict",
          message: `Thread '${normalized}' is already on board '${boardId}'.`,
          current: cloneBoard(board),
        };
      }
    }
    return null;
  }

  function applyThreadAnchorFromPatch(raw) {
    if (raw === null || raw === undefined || raw === "") {
      card.thread_id = null;
      return true;
    }
    const value = String(raw ?? "").trim();
    const typed = splitTypedRef(value);
    if (value && typed.prefix === "thread") {
      card.thread_id = typed.id;
      return true;
    }
    if (value && !typed.prefix) {
      card.thread_id = value;
      return true;
    }
    if (!value) {
      card.thread_id = null;
      return true;
    }
    return false;
  }

  if (Object.prototype.hasOwnProperty.call(patch, "thread_id")) {
    const peek = peekThreadIdFromAnchorRaw(patch.thread_id);
    if (peek.ok) {
      const err = validateCardThreadChange(peek.id);
      if (err) return err;
    }
    const beforeThread = String(card.thread_id ?? "").trim();
    const applied = applyThreadAnchorFromPatch(patch.thread_id);
    delete card.thread_ref;
    if (applied && String(card.thread_id ?? "").trim() !== beforeThread) {
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "thread_ref")) {
    const peek = peekThreadIdFromAnchorRaw(patch.thread_ref);
    if (peek.ok) {
      const err = validateCardThreadChange(peek.id);
      if (err) return err;
    }
    const beforeThread = String(card.thread_id ?? "").trim();
    const applied = applyThreadAnchorFromPatch(patch.thread_ref);
    delete card.thread_ref;
    if (applied && String(card.thread_id ?? "").trim() !== beforeThread) {
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "resolution")) {
    const raw = patch.resolution;
    let next = null;
    if (raw !== null && raw !== undefined && String(raw).trim() !== "") {
      const value = String(raw).trim();
      if (value === "completed") next = "done";
      else if (value === "done" || value === "canceled") next = value;
    }
    if (card.resolution !== next) {
      card.resolution = next;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "resolution_refs")) {
    const next = normalizeCardRefList(patch.resolution_refs);
    const current = normalizeCardRefList(card.resolution_refs);
    if (JSON.stringify(current) !== JSON.stringify(next)) {
      card.resolution_refs = next;
      mutated = true;
    }
  }

  if (Object.prototype.hasOwnProperty.call(patch, "related_refs")) {
    const next = normalizeCardRefList(patch.related_refs);
    const current = normalizeCardRefList(card.related_refs);
    if (JSON.stringify(current) !== JSON.stringify(next)) {
      card.related_refs = next;
      mutated = true;
    }
    const sole = soleThreadIdFromRelatedRefsForCreate(card.related_refs);
    if (!sole.ok) {
      return { error: "validation", message: sole.message };
    }
    if (
      sole.threadId &&
      sole.threadId !== String(card.thread_id ?? "").trim()
    ) {
      const err = validateCardThreadChange(sole.threadId);
      if (err) return err;
      card.thread_id = sole.threadId;
      mutated = true;
    }
  }

  if (!mutated) {
    return {
      board: cloneBoard(board),
      card: cloneBoardCard(card),
    };
  }

  card.version = (Number(card.version) || 0) + 1;
  card.updated_at = new Date().toISOString();
  if (payload.actor_id) {
    card.updated_by = payload.actor_id;
  }
  touchBoard(board, payload.actor_id);

  return {
    board: cloneBoard(board),
    card: cloneBoardCard(card),
  };
}

export function moveMockBoardCard(boardId, cardId, payload) {
  const board = boards.find((candidate) => candidate.id === boardId);
  if (!board) {
    return { error: "not_found", message: `Board not found: ${boardId}` };
  }

  const card = boardCards.find(
    (candidate) =>
      candidate.board_id === boardId &&
      mockCardMatchesAnchor(candidate, cardId),
  );
  if (!card) {
    return {
      error: "not_found",
      message: `Card not found: ${cardId} on board ${boardId}`,
    };
  }

  const boardConflict = boardMutationConflict(
    board,
    payload.if_board_updated_at,
  );
  if (boardConflict) {
    return boardConflict;
  }

  const columnKey = String(payload.column_key || card.column_key).trim();
  if (!canonicalBoardColumnKeys.has(columnKey)) {
    return {
      error: "validation",
      message:
        "column_key must be one of: backlog, ready, in_progress, blocked, review, done.",
    };
  }
  const columns = getBoardColumnCards(boardId);
  const sourceColumn = columns[card.column_key] ?? [];
  const targetColumn = columns[columnKey] ?? (columns[columnKey] = []);
  const movingKey = mockBoardCardStableKey(card);
  const sourceIndex = sourceColumn.findIndex(
    (candidate) => mockBoardCardStableKey(candidate) === movingKey,
  );
  if (sourceIndex >= 0) {
    sourceColumn.splice(sourceIndex, 1);
  }

  card.column_key = columnKey;
  const insertIndex = resolveInsertIndex(targetColumn, payload);
  targetColumn.splice(insertIndex, 0, card);

  if (Object.prototype.hasOwnProperty.call(payload, "resolution")) {
    const moveResolution = String(payload.resolution ?? "").trim();
    if (moveResolution === "completed" || moveResolution === "done") {
      card.resolution = "done";
    } else if (moveResolution === "canceled") {
      card.resolution = "canceled";
    }
  } else if (
    columnKey === "done" &&
    (!card.resolution ||
      card.resolution === "unresolved" ||
      String(card.resolution).trim() === "")
  ) {
    card.resolution = "done";
  }

  if (Object.prototype.hasOwnProperty.call(payload, "resolution_refs")) {
    card.resolution_refs = normalizeCardRefList(payload.resolution_refs);
  }

  renormalizeColumnCards(sourceColumn);
  if (targetColumn !== sourceColumn) {
    renormalizeColumnCards(targetColumn);
  } else {
    renormalizeColumnCards(sourceColumn);
  }

  card.updated_at = new Date().toISOString();
  if (payload.actor_id) {
    card.updated_by = payload.actor_id;
  }
  touchBoard(board, payload.actor_id);

  return {
    board: cloneBoard(board),
    card: cloneBoardCard(card),
  };
}

export function removeMockBoardCard(boardId, cardId, payload = {}) {
  const board = boards.find((candidate) => candidate.id === boardId);
  if (!board) {
    return { error: "not_found", message: `Board not found: ${boardId}` };
  }

  const boardConflict = boardMutationConflict(
    board,
    payload.if_board_updated_at,
  );
  if (boardConflict) {
    return boardConflict;
  }

  const cardIndex = boardCards.findIndex(
    (candidate) =>
      candidate.board_id === boardId &&
      mockCardMatchesAnchor(candidate, cardId),
  );
  if (cardIndex === -1) {
    return {
      error: "not_found",
      message: `Card not found: ${cardId} on board ${boardId}`,
    };
  }

  const removedCard = boardCards[cardIndex];
  archiveBoardCard(
    removedCard,
    payload.actor_id,
    String(payload.reason ?? "").trim(),
  );
  const remainingInColumn = sortBoardCardsForBoard(
    boardCards.filter(
      (candidate) =>
        candidate.board_id === boardId &&
        candidate.column_key === removedCard.column_key &&
        isVisibleBoardCard(candidate),
    ),
  );
  renormalizeColumnCards(remainingInColumn);
  touchBoard(board, payload.actor_id);

  return {
    board: cloneBoard(board),
    card: cloneBoardCard(removedCard),
    removed_thread_id: removedCard.thread_id,
  };
}

/** Archive/remove by card id alone (matches POST /cards/{card_id}/archive). */
export function archiveMockBoardCardByCardId(cardId, payload = {}) {
  const cardIndex = boardCards.findIndex((candidate) =>
    mockCardMatchesAnchor(candidate, cardId),
  );
  if (cardIndex === -1) {
    return {
      error: "not_found",
      message: `Card not found: ${cardId}`,
    };
  }
  const cardRow = boardCards[cardIndex];
  return removeMockBoardCard(cardRow.board_id, cardId, payload);
}

export function restoreMockBoardCardByCardId(cardId, payload = {}) {
  const card = boardCards.find((candidate) =>
    mockCardMatchesAnchor(candidate, cardId),
  );
  if (!card) {
    return {
      error: "not_found",
      message: `Card not found: ${cardId}`,
    };
  }

  const board = boards.find((candidate) => candidate.id === card.board_id);
  if (!board) {
    return {
      error: "not_found",
      message: `Board not found: ${card.board_id}`,
    };
  }

  clearBoardCardLifecycle(card);
  const activeCardsInColumn = sortBoardCardsForBoard(
    boardCards.filter(
      (candidate) =>
        candidate.board_id === board.id &&
        candidate.column_key === card.column_key &&
        isVisibleBoardCard(candidate),
    ),
  );
  renormalizeColumnCards(activeCardsInColumn);
  card.version = (Number(card.version) || 0) + 1;
  card.updated_at = new Date().toISOString();
  if (payload.actor_id) {
    card.updated_by = payload.actor_id;
  }
  touchBoard(board, payload.actor_id);

  return {
    board: cloneBoard(board),
    card: cloneBoardCard(card),
  };
}

export function purgeMockBoardCardByCardId(cardId, payload = {}) {
  const cardIndex = boardCards.findIndex((candidate) =>
    mockCardMatchesAnchor(candidate, cardId),
  );
  if (cardIndex === -1) {
    return {
      error: "not_found",
      message: `Card not found: ${cardId}`,
    };
  }

  const removedCard = boardCards.splice(cardIndex, 1)[0];
  const board = boards.find(
    (candidate) => candidate.id === removedCard.board_id,
  );
  if (board) {
    const remainingInColumn = sortBoardCardsForBoard(
      boardCards.filter(
        (candidate) =>
          candidate.board_id === board.id &&
          candidate.column_key === removedCard.column_key &&
          isVisibleBoardCard(candidate),
      ),
    );
    renormalizeColumnCards(remainingInColumn);
    touchBoard(board, payload.actor_id);
  }

  return {
    board: board ? cloneBoard(board) : null,
    card: cloneBoardCard(removedCard),
  };
}

function normalizeBoardCreateDocumentRefs(raw) {
  if (!Array.isArray(raw)) {
    return [];
  }
  const out = [];
  for (const item of raw) {
    const s = String(item ?? "").trim();
    if (!s) continue;
    out.push(s.startsWith("document:") ? s : `document:${s}`);
  }
  return [...new Set(out)].sort();
}

export function createMockBoard(payload) {
  const requestedId = String(payload.board.id ?? "").trim();
  const boardId =
    requestedId || `board-${Math.random().toString(36).slice(2, 10)}`;

  if (boards.some((board) => board.id === boardId)) {
    return {
      error: "conflict",
      message: `Board with id '${boardId}' already exists.`,
    };
  }

  const threadId = String(payload.board.thread_id ?? "").trim();
  if (!threadId) {
    return {
      error: "validation",
      message: "board.thread_id is required",
    };
  }
  if (!getMockThread(threadId)) {
    return {
      error: "not_found",
      message: `Thread not found: ${threadId}`,
    };
  }
  const boardTitle = String(payload.board.title ?? "").trim();
  if (!boardTitle) {
    return {
      error: "validation",
      message: "board.title is required",
    };
  }
  const boardStatus = String(payload.board.status ?? "active").trim();
  if (!["active", "paused", "closed"].includes(boardStatus)) {
    return {
      error: "validation",
      message: "board.status must be one of: active, paused, closed",
    };
  }
  const documentRefStrings = normalizeBoardCreateDocumentRefs(
    payload.board.document_refs,
  );
  for (const ref of documentRefStrings) {
    const docId = ref.startsWith("document:")
      ? ref.slice("document:".length).trim()
      : ref;
    if (docId && !getMockDocument(docId)) {
      return {
        error: "not_found",
        message: `Document not found: ${docId}`,
      };
    }
  }

  const pinnedRefsRaw = Array.isArray(payload.board.pinned_refs)
    ? payload.board.pinned_refs
        .map((r) => String(r ?? "").trim())
        .filter(Boolean)
    : [];
  const refsSet = new Set([
    `thread:${threadId}`,
    mockTopicRefFromThreadId(threadId),
    ...documentRefStrings,
    ...pinnedRefsRaw,
  ]);

  const nowIso = new Date().toISOString();
  const newBoard = {
    id: boardId,
    title: boardTitle,
    status: boardStatus,
    labels: payload.board.labels || [],
    owners: payload.board.owners || [],
    thread_id: threadId,
    refs: [...refsSet].sort(),
    column_schema: (payload.board.column_schema || canonicalColumnSchema).map(
      (column) => ({ ...column }),
    ),
    pinned_refs: pinnedRefsRaw,
    created_at: nowIso,
    created_by: payload.actor_id || "unknown",
    updated_at: nowIso,
    updated_by: payload.actor_id || "unknown",
  };

  boards.push(newBoard);

  return { board: cloneBoard(newBoard) };
}

export function updateMockBoard(boardId, payload) {
  const board = boards.find((candidate) => candidate.id === boardId);
  if (!board) {
    return { error: "not_found", message: `Board not found: ${boardId}` };
  }

  const boardConflict = boardMutationConflict(board, payload.if_updated_at);
  if (boardConflict) {
    return boardConflict;
  }

  const patch = payload.patch ?? {};

  if (patch.status !== undefined) {
    const nextStatus = String(patch.status ?? "").trim();
    if (!["active", "paused", "closed"].includes(nextStatus)) {
      return {
        error: "validation",
        message: "board.status must be one of: active, paused, closed",
      };
    }
  }
  if (patch.title !== undefined && !String(patch.title ?? "").trim()) {
    return {
      error: "validation",
      message: "board.title is required",
    };
  }
  if (patch.document_refs !== undefined) {
    const next = normalizeBoardCreateDocumentRefs(patch.document_refs);
    for (const ref of next) {
      const docId = ref.startsWith("document:")
        ? ref.slice("document:".length).trim()
        : ref;
      if (docId && !getMockDocument(docId)) {
        return {
          error: "not_found",
          message: `Document not found: ${docId}`,
        };
      }
    }
  }

  if (patch.title !== undefined) board.title = String(patch.title).trim();
  if (patch.status !== undefined) board.status = patch.status;
  if (patch.labels !== undefined) board.labels = patch.labels;
  if (patch.owners !== undefined) board.owners = patch.owners;
  if (patch.document_refs !== undefined) {
    const next = normalizeBoardCreateDocumentRefs(patch.document_refs);
    const base = (board.refs ?? []).filter(
      (r) => !String(r).trim().startsWith("document:"),
    );
    board.refs = [...new Set([...base, ...next])].sort();
  }
  if (patch.pinned_refs !== undefined) {
    board.pinned_refs = patch.pinned_refs;
  }
  if (patch.column_schema !== undefined) {
    board.column_schema = patch.column_schema;
  }

  touchBoard(board, payload.actor_id);

  return { board: cloneBoard(board) };
}
