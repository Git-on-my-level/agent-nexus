const now = Date.now();

function iso(offsetMs) {
  return new Date(now + offsetMs).toISOString();
}

export function getPilotRescueSeedData() {
  return {
    actors: [
      {
        id: "actor-product-lead",
        display_name: "Avery Product",
        tags: ["product", "strategy"],
        created_at: iso(-30 * 24 * 60 * 60 * 1000),
      },
      {
        id: "actor-support-lead",
        display_name: "Jordan Support",
        tags: ["support", "customer-success"],
        created_at: iso(-29 * 24 * 60 * 60 * 1000),
      },
      {
        id: "actor-project-manager",
        display_name: "Morgan PM",
        tags: ["delivery", "coordination"],
        created_at: iso(-28 * 24 * 60 * 60 * 1000),
      },
      {
        id: "actor-delivery-engineer",
        display_name: "Riley Delivery",
        tags: ["engineering", "implementation"],
        created_at: iso(-27 * 24 * 60 * 60 * 1000),
      },
      {
        id: "actor-growth-ops",
        display_name: "Casey Growth",
        tags: ["gtm", "launch"],
        created_at: iso(-26 * 24 * 60 * 60 * 1000),
      },
    ],
    threads: [
      {
        id: "thread-pilot-rescue-main",
        type: "initiative",
        title: "Pilot Rescue Sprint: NorthWave Launch Readiness",
        status: "active",
        priority: "p0",
        tags: ["pilot", "launch", "customer-feedback", "cross-functional"],
        key_artifacts: [
          "artifact-feedback-matrix",
          "artifact-launch-checklist",
          "artifact-risk-register",
        ],
        cadence: "daily",
        current_summary:
          "NorthWave's pilot launch is at risk. Customer feedback says daily digests hide card owners and due dates, escalation inboxes duplicate follow-up topics after updates, and artifact visibility in topic workspaces is inconsistent. Product wants a limited pilot rescue plan by Friday without promising a platform rewrite.",
        next_actions: [
          "Support synthesizes customer pain and closure requirements",
          "Delivery scopes the minimum safe fix set",
          "Project manager sequences launch gating and ownership",
          "Product manager publishes final rescue and GTM recommendation",
        ],
        next_check_in_at: iso(6 * 60 * 60 * 1000),
        updated_at: iso(-35 * 60 * 1000),
        updated_by: "actor-product-lead",
        provenance: {
          sources: ["actor_statement:evt-pilot-main-001"],
        },
      },
      {
        id: "thread-pilot-feedback",
        type: "case",
        title: "Customer Escalation: NorthWave Pilot Feedback",
        status: "active",
        priority: "p1",
        tags: ["support", "feedback", "northwave"],
        key_artifacts: ["artifact-feedback-quotes", "artifact-feedback-matrix"],
        cadence: "daily",
        current_summary:
          "NorthWave and BriskPay both reported pilot pain. NorthWave's daily digest omits card owners and due dates. BriskPay cannot see supporting artifacts in topic workspaces. Support is manually stitching context together for every follow-up.",
        next_actions: [
          "Summarize customer pain and retention risk",
          "Turn recurring complaints into clear launch acceptance criteria",
          "Draft closure note after fix verification",
        ],
        next_check_in_at: iso(4 * 60 * 60 * 1000),
        updated_at: iso(-50 * 60 * 1000),
        updated_by: "actor-support-lead",
        provenance: {
          sources: ["actor_statement:evt-feedback-001"],
        },
      },
      {
        id: "thread-pilot-delivery",
        type: "process",
        title: "Delivery Plan: Pilot Fix + Rollout Sequencing",
        status: "active",
        priority: "p1",
        tags: ["delivery", "bugs", "rollout"],
        key_artifacts: ["artifact-risk-register", "artifact-launch-checklist"],
        cadence: "daily",
        current_summary:
          "Engineering believes the digest-field omission is a small patch. Duplicate escalation threading is moderate risk but can be narrowed to status update webhook handling. Artifact timeline visibility probably needs a follow-on note in the GTM brief so customers understand the staged rollout.",
        next_actions: [
          "Confirm minimum patch scope for Friday",
          "Sequence launch gating and customer validation",
          "Escalate anything that would force a one-week slip",
        ],
        next_check_in_at: iso(5 * 60 * 60 * 1000),
        updated_at: iso(-42 * 60 * 1000),
        updated_by: "actor-project-manager",
        provenance: {
          sources: ["actor_statement:evt-delivery-001"],
        },
      },
    ],
    artifacts: [
      {
        id: "artifact-feedback-matrix",
        kind: "doc",
        thread_id: "thread-pilot-feedback",
        summary: "Support feedback matrix for NorthWave and BriskPay pilot complaints",
        refs: [
          "topic:thread-pilot-feedback",
          "topic:thread-pilot-rescue-main",
        ],
        provenance: {
          sources: ["actor_statement:evt-feedback-001"],
        },
        created_at: iso(-55 * 60 * 1000),
        created_by: "actor-support-lead",
        content_text: `# Feedback Matrix\n\n- NorthWave: daily digest omits card owner and due date; CSM is manually annotating updates.\n- NorthWave: executive sponsor expects a Friday rescue plan, not a vague roadmap promise.\n- BriskPay: artifacts are not visible in topic workspaces, so escalation follow-up lacks evidence.\n- BriskPay: duplicate escalation follow-up after status updates makes support triage noisy.\n\nSeverity:\n- Digest omission: critical for NorthWave launch confidence\n- Duplicate escalation follow-up: high operational cost\n- Artifact visibility: medium, acceptable as a staged follow-up if clearly documented\n`,
      },
      {
        id: "artifact-feedback-quotes",
        kind: "doc",
        thread_id: "thread-pilot-feedback",
        summary: "Direct customer quotes from NorthWave and BriskPay pilot feedback",
        refs: ["topic:thread-pilot-feedback"],
        provenance: {
          sources: ["actor_statement:evt-feedback-001"],
        },
        created_at: iso(-54 * 60 * 1000),
        created_by: "actor-support-lead",
        content_text: `# Customer Quotes\n\nNorthWave sponsor:\n"If the digest cannot tell my team who owns a card or when it is due, this is not launch-ready for our Friday pilot review."\n\nNorthWave CSM:\n"We are manually copying due dates into Slack because the digest leaves them out."\n\nBriskPay ops lead:\n"We can live with a staged artifact workspace improvement, but we cannot keep triaging duplicate escalation follow-up."\n`,
      },
      {
        id: "artifact-launch-checklist",
        kind: "doc",
        thread_id: "thread-pilot-delivery",
        summary: "Friday pilot launch checklist and validation gates",
        refs: [
          "topic:thread-pilot-delivery",
          "topic:thread-pilot-rescue-main",
        ],
        provenance: {
          sources: ["actor_statement:evt-delivery-001"],
        },
        created_at: iso(-48 * 60 * 1000),
        created_by: "actor-project-manager",
        content_text: `# Launch Checklist\n\nFriday launch gate:\n1. Digest owner/due-date patch merged and validated with NorthWave sample data\n2. Duplicate escalation follow-up reproduction removed in pilot path\n3. Support closure draft reviewed by Product\n4. Product rescue brief updated with exact scope and follow-up deliverables\n5. If any item misses 11:00 local time Friday, slip the pilot by one week\n`,
      },
      {
        id: "artifact-risk-register",
        kind: "doc",
        thread_id: "thread-pilot-delivery",
        summary: "Delivery and rollout risk register for NorthWave pilot rescue",
        refs: [
          "topic:thread-pilot-delivery",
          "topic:thread-pilot-rescue-main",
        ],
        provenance: {
          sources: ["actor_statement:evt-delivery-001"],
        },
        created_at: iso(-47 * 60 * 1000),
        created_by: "actor-delivery-engineer",
        content_text: `# Risk Register\n\n- Digest owner/due-date omission: low implementation risk, high customer impact\n- Duplicate escalation follow-up: medium implementation risk, high support cost\n- Artifact workspace visibility: medium implementation risk, can be follow-up if clearly documented\n- Launch promise risk: high if product implies full platform fix instead of scoped pilot rescue\n`,
      },
      {
        id: "artifact-pilot-metrics",
        kind: "doc",
        thread_id: "thread-pilot-rescue-main",
        summary: "Pilot metrics and renewal risk snapshot",
        refs: ["topic:thread-pilot-rescue-main"],
        provenance: {
          sources: ["actor_statement:evt-pilot-main-001"],
        },
        created_at: iso(-46 * 60 * 1000),
        created_by: "actor-growth-ops",
        content_text: `# Pilot Metrics\n\n- NorthWave weekly active evaluators: 18\n- NorthWave Friday launch review: locked for 2026-03-07 16:00 local time\n- BriskPay renewal conversation: in 10 days\n- Support hours spent manually reconciling duplicate escalation follow-up this week: 6.5\n- Product budget assumption: only a scoped pilot rescue fits this week\n`,
      },
    ],
    documents: [
      {
        id: "northwave-pilot-rescue-brief",
        document: {
          id: "northwave-pilot-rescue-brief",
          title: "NorthWave Pilot Rescue Brief",
          kind: "gtm-brief",
          owner: "actor-product-lead",
          status: "draft",
        },
        refs: [
          "topic:thread-pilot-rescue-main",
          "topic:thread-pilot-feedback",
          "topic:thread-pilot-delivery",
          "artifact:artifact-feedback-matrix",
          "artifact:artifact-launch-checklist",
        ],
        content_type: "text",
        content: `# NorthWave Pilot Rescue Brief\n\nStatus: draft\n\nOpen questions:\n- Which fixes are in Friday scope?\n- What do we tell NorthWave and BriskPay about the artifact timeline gap?\n- What exact go/no-go gate should the Friday pilot use?\n`,
        actor_id: "actor-product-lead",
      },
    ],
    events: [
      {
        id: "evt-pilot-main-001",
        actor_id: "actor-product-lead",
        type: "actor_statement",
        thread_id: "thread-pilot-rescue-main",
        refs: [
          "topic:thread-pilot-rescue-main",
          "artifact:artifact-pilot-metrics",
          "document:northwave-pilot-rescue-brief",
        ],
        summary: "Product kickoff: ship a Friday rescue plan without promising a platform rewrite",
        payload: {
          ask: "Need support, delivery, and project recommendations today so Product can publish a limited pilot rescue brief.",
          constraint: "Only a scoped launch rescue fits this week.",
        },
        provenance: {
          sources: ["inferred"],
        },
        ts: iso(-45 * 60 * 1000),
      },
      {
        id: "evt-feedback-001",
        actor_id: "actor-support-lead",
        type: "actor_statement",
        thread_id: "thread-pilot-feedback",
        refs: [
          "topic:thread-pilot-feedback",
          "artifact:artifact-feedback-matrix",
          "artifact:artifact-feedback-quotes",
        ],
        summary: "Support escalation: NorthWave will not call Friday pilot launch-ready without digest ownership and due dates",
        payload: {
          severity: "high",
          customer_risk: "NorthWave sponsor expects a concrete rescue plan by Friday review; BriskPay can tolerate staged artifact visibility if duplicate follow-up is removed.",
        },
        provenance: {
          sources: ["artifact:artifact-feedback-quotes"],
        },
        ts: iso(-44 * 60 * 1000),
      },
      {
        id: "evt-delivery-001",
        actor_id: "actor-delivery-engineer",
        type: "actor_statement",
        thread_id: "thread-pilot-delivery",
        refs: [
          "topic:thread-pilot-delivery",
          "artifact:artifact-risk-register",
          "artifact:artifact-launch-checklist",
        ],
        summary: "Delivery assessment: two fixes fit Friday scope, artifact timeline visibility should be documented as follow-up",
        payload: {
          digest_fix_risk: "low",
          dedupe_fix_risk: "medium",
          artifact_timeline_follow_up: true,
        },
        provenance: {
          sources: ["artifact:artifact-risk-register"],
        },
        ts: iso(-43 * 60 * 1000),
      },
      {
        id: "evt-main-decision-needed",
        actor_id: "actor-project-manager",
        type: "decision_needed",
        thread_id: "thread-pilot-rescue-main",
        refs: [
          "topic:thread-pilot-rescue-main",
          "topic:thread-pilot-feedback",
          "topic:thread-pilot-delivery",
          ],
        summary: "Need cross-functional rescue recommendation before Friday pilot gate",
        payload: {
          ask: "Support, Delivery, and PM should post role-specific recommendations. Product will publish final rescue brief after reviewing them.",
          deadline: iso(8 * 60 * 60 * 1000),
        },
        provenance: {
          sources: ["inferred"],
        },
        ts: iso(-41 * 60 * 1000),
      },
    ],
  };
}
