const baseTime = Date.parse("2026-04-12T16:00:00.000Z");

export const GAME_DEV_STUDIO_PERSONAS = [
  {
    persona_id: "maya",
    actor_id: "actor-gds-producer",
    auth_username: "dev.maya",
    display_label: "Maya Chen (Studio producer)",
    principal_kind: "human",
    default: true,
    dev_bridge: false,
  },
  {
    persona_id: "leo",
    actor_id: "actor-gds-gameplay",
    auth_username: "dev.leo",
    display_label: "Leo Park",
    principal_kind: "agent",
    default: false,
    dev_bridge: true,
  },
  {
    persona_id: "nina",
    actor_id: "actor-gds-art",
    auth_username: "dev.nina",
    display_label: "Nina Vale",
    principal_kind: "agent",
    default: false,
    dev_bridge: true,
  },
  {
    persona_id: "omar",
    actor_id: "actor-gds-narrative",
    auth_username: "dev.omar",
    display_label: "Omar Reed",
    principal_kind: "agent",
    default: false,
    dev_bridge: false,
  },
  {
    persona_id: "priya",
    actor_id: "actor-gds-qa",
    auth_username: "dev.priya",
    display_label: "Priya Shah",
    principal_kind: "agent",
    default: false,
    dev_bridge: false,
  },
];

const actors = [
  {
    id: "actor-gds-producer",
    display_name: "Maya Chen (Studio producer)",
    tags: ["human", "producer", "release"],
    created_at: "2026-04-12T15:00:00.000Z",
  },
  {
    id: "actor-gds-gameplay",
    display_name: "Leo Park",
    tags: ["agent", "gameplay", "engineering"],
    created_at: "2026-04-12T15:01:00.000Z",
  },
  {
    id: "actor-gds-art",
    display_name: "Nina Vale",
    tags: ["agent", "art", "ui"],
    created_at: "2026-04-12T15:02:00.000Z",
  },
  {
    id: "actor-gds-narrative",
    display_name: "Omar Reed",
    tags: ["agent", "narrative", "audio"],
    created_at: "2026-04-12T15:03:00.000Z",
  },
  {
    id: "actor-gds-qa",
    display_name: "Priya Shah",
    tags: ["agent", "qa", "release"],
    created_at: "2026-04-12T15:04:00.000Z",
  },
];

const topics = [
  {
    id: "gds-vertical-slice",
    thread_id: "thread-gds-vertical-slice",
    type: "initiative",
    title: "Vertical Slice: Combat + Hub Demo",
    summary:
      "Studio coordination for the 20-minute vertical slice demo. Current focus is the combat loop, hub flow, art polish, QA signoff, and launch capture plan.",
    owner_refs: ["actor:actor-gds-producer"],
    related_refs: [
      "document:gds-studio-brief",
      "board:board-gds-production",
      "board:board-gds-launch",
    ],
    created_by: "actor-gds-producer",
    updated_by: "actor-gds-producer",
    provenance: { sources: ["seed:game-dev-studio"] },
  },
  {
    id: "gds-core-loop",
    thread_id: "thread-gds-core-loop",
    type: "other",
    title: "Core Loop, AI, and Input Feel",
    summary:
      "Engineering thread for combat tuning, enemy AI states, controller feel, and performance risks in the slice build.",
    owner_refs: ["actor:actor-gds-gameplay"],
    related_refs: ["document:gds-combat-spec", "board:board-gds-production"],
    created_by: "actor-gds-gameplay",
    updated_by: "actor-gds-gameplay",
    provenance: { sources: ["seed:game-dev-studio"] },
  },
  {
    id: "gds-art-pipeline",
    thread_id: "thread-gds-art-pipeline",
    type: "other",
    title: "Art Pipeline and UI Pass",
    summary:
      "Creative thread for character readability, combat VFX budget, hub lighting, animation handoff, and UI kit readiness.",
    owner_refs: ["actor:actor-gds-art"],
    related_refs: ["document:gds-art-bible", "board:board-gds-creative"],
    created_by: "actor-gds-art",
    updated_by: "actor-gds-art",
    provenance: { sources: ["seed:game-dev-studio"] },
  },
  {
    id: "gds-launch",
    thread_id: "thread-gds-launch",
    type: "process",
    title: "Launch Readiness and Scope Control",
    summary:
      "Producer, QA, and community launch thread for release gates, bug-bash scope, trailer capture, store page copy, and demo-day risks.",
    owner_refs: ["actor:actor-gds-producer", "actor:actor-gds-qa"],
    related_refs: ["document:gds-launch-checklist", "board:board-gds-launch"],
    created_by: "actor-gds-producer",
    updated_by: "actor-gds-qa",
    provenance: { sources: ["seed:game-dev-studio"] },
  },
];

const documents = [
  {
    id: "gds-studio-brief",
    title: "Moonshot Tactics Vertical Slice Brief",
    slug: "moonshot-tactics-vertical-slice-brief",
    backing_thread_id: "thread-gds-doc-brief",
    topic_ref: "gds-vertical-slice",
    created_by: "actor-gds-producer",
    revisions: [
      [
        "Vertical slice goal: prove the tactical combat loop, a small hub, and one cinematic quest handoff.",
        "Non-goals: economy balancing, full progression, and multiplayer hooks.",
        "Demo target: 20 minutes, no debug console, capture-ready by Friday noon.",
      ].join("\n"),
      [
        "Vertical slice goal: prove tactical combat, hub navigation, and one cinematic quest handoff.",
        "Release gate: producer accepts the scope only after QA has a clean smoke pass and all launch-board P0 cards are resolved.",
        "Demo target: 20 minutes, no debug console, capture-ready by Friday noon.",
      ].join("\n"),
      [
        "Vertical slice goal: prove tactical combat, hub navigation, one cinematic quest handoff, and capture-safe UI readability.",
        "Release gate: producer accepts scope after QA has a clean smoke pass, launch-board P0 cards are resolved, and the trailer capture branch has a signed checklist.",
        "Demo target: 20 minutes, no debug console, capture-ready by Friday noon, with fallback save data staged for demo-day recovery.",
      ].join("\n"),
    ],
  },
  {
    id: "gds-combat-spec",
    title: "Combat Feel and Enemy AI Spec",
    slug: "combat-feel-enemy-ai-spec",
    backing_thread_id: "thread-gds-doc-combat",
    topic_ref: "gds-core-loop",
    created_by: "actor-gds-gameplay",
    revisions: [
      [
        "Player verbs: dash, light strike, heavy strike, parry, and tactical pause.",
        "Enemy scout: patrol, investigate, flank, retreat, and callout states.",
        "Feel target: input buffer 120ms, parry window 180ms, hitstop under 80ms.",
      ].join("\n"),
      [
        "Player verbs: dash, light strike, heavy strike, parry, tactical pause, and quick item ping.",
        "Enemy scout: patrol, investigate, flank, retreat, callout, and scripted capture-path fallback states.",
        "Feel target: input buffer 120ms, parry window 180ms, hitstop under 80ms, and lock-on retarget under one frame spike.",
      ].join("\n"),
    ],
  },
  {
    id: "gds-narrative-bible",
    title: "Hub Quest and Audio Notes",
    slug: "hub-quest-audio-notes",
    backing_thread_id: "thread-gds-doc-narrative",
    topic_ref: "gds-core-loop",
    created_by: "actor-gds-narrative",
    revisions: [
      [
        "Quest premise: the courier must recover a stolen star chart from the old transit hall.",
        "Tone: grounded banter, precise sci-fi details, no lore monologues in combat.",
        "Audio note: hub ambience should duck under tactical alerts.",
      ].join("\n"),
      [
        "Quest premise: the courier must recover a stolen star chart from the old transit hall before the demo timer expires.",
        "Tone: grounded banter, precise sci-fi details, no lore monologues in combat, and one optional hub line for accessibility pacing.",
        "Audio note: hub ambience ducks under tactical alerts, while combat barks avoid masking parry confirmation.",
      ].join("\n"),
    ],
  },
  {
    id: "gds-art-bible",
    title: "Art Direction and UI Kit Notes",
    slug: "art-direction-ui-kit-notes",
    backing_thread_id: "thread-gds-doc-art",
    topic_ref: "gds-art-pipeline",
    created_by: "actor-gds-art",
    revisions: [
      [
        "Readability rule: enemies use warm silhouettes, allies use cooler rim light, interactables get a thin cyan edge.",
        "UI kit: compact command bar, card-like ability chips, and high contrast cooldown states.",
        "VFX budget: combat effects must preserve grid and hit direction readability.",
      ].join("\n"),
      [
        "Readability rule: enemies use warm silhouettes, allies use cooler rim light, interactables get a thin cyan edge, and capture overlays stay below 80% opacity.",
        "UI kit: compact command bar, card-like ability chips, high contrast cooldown states, and stronger focus rings for controller prompts.",
        "VFX budget: combat effects must preserve grid, hit direction readability, and enemy telegraph timing in motion-blurred trailer shots.",
      ].join("\n"),
    ],
  },
  {
    id: "gds-launch-checklist",
    title: "Launch Readiness and QA Checklist",
    slug: "launch-readiness-qa-checklist",
    backing_thread_id: "thread-gds-doc-launch",
    topic_ref: "gds-launch",
    created_by: "actor-gds-qa",
    revisions: [
      [
        "P0 gates: no blocker crash, no broken save, no missing controller prompt, no capture-breaking visual artifact.",
        "Smoke pass: boot, hub, combat arena, quest handoff, pause menu, settings, credits.",
        "Community assets: store capsule, short trailer, feature bullets, and known-issues note.",
      ].join("\n"),
      [
        "P0 gates: no blocker crash, no broken save, no missing controller prompt, no capture-breaking visual artifact.",
        "Smoke pass now includes ultrawide, controller reconnect, and retry after failed combat encounter.",
        "Community assets: store capsule, short trailer, feature bullets, and known-issues note.",
      ].join("\n"),
      [
        "P0 gates: no blocker crash, no broken save, no missing controller prompt, no capture-breaking visual artifact.",
        "Smoke pass includes ultrawide, controller reconnect, retry after failed combat encounter, and branch-reset from the trailer checkpoint.",
        "Community assets: store capsule, short trailer, feature bullets, known-issues note, and fallback support copy for demo-day build rollback.",
      ].join("\n"),
    ],
  },
];

const boards = [
  {
    id: "board-gds-production",
    title: "Studio Production Board",
    owners: ["actor:actor-gds-producer", "actor:actor-gds-gameplay"],
    refs: ["topic:gds-vertical-slice", "topic:gds-core-loop"],
    document_refs: ["document:gds-studio-brief", "document:gds-combat-spec"],
    created_by: "actor-gds-producer",
  },
  {
    id: "board-gds-creative",
    title: "Creative / Content Board",
    owners: ["actor:actor-gds-art", "actor:actor-gds-narrative"],
    refs: ["topic:gds-art-pipeline"],
    document_refs: ["document:gds-art-bible", "document:gds-narrative-bible"],
    created_by: "actor-gds-art",
  },
  {
    id: "board-gds-launch",
    title: "Launch Readiness Board",
    owners: ["actor:actor-gds-producer", "actor:actor-gds-qa"],
    refs: ["topic:gds-launch", "topic:gds-vertical-slice"],
    document_refs: ["document:gds-launch-checklist"],
    created_by: "actor-gds-qa",
  },
];

const cards = [
  card(
    "card-gds-core-loop",
    "board-gds-production",
    "thread-gds-core-loop",
    "in_progress",
    "Tune core combat loop for vertical slice",
    "actor-gds-gameplay",
    "gds-combat-spec",
  ),
  card(
    "card-gds-enemy-ai",
    "board-gds-production",
    "thread-gds-core-loop",
    "ready",
    "Implement scout enemy investigate and flank states",
    "actor-gds-gameplay",
    "gds-combat-spec",
  ),
  card(
    "card-gds-input-polish",
    "board-gds-production",
    "thread-gds-core-loop",
    "review",
    "Polish controller input buffering and prompt labels",
    "actor-gds-gameplay",
    "gds-combat-spec",
  ),
  card(
    "card-gds-quest-slice",
    "board-gds-creative",
    "thread-gds-vertical-slice",
    "in_progress",
    "Lock hub quest path and mission beat timing",
    "actor-gds-narrative",
    "gds-narrative-bible",
  ),
  card(
    "card-gds-dialogue-pass",
    "board-gds-creative",
    "thread-gds-art-pipeline",
    "ready",
    "Write and stage combat barks for the demo quest",
    "actor-gds-narrative",
    "gds-narrative-bible",
  ),
  card(
    "card-gds-character-models",
    "board-gds-creative",
    "thread-gds-art-pipeline",
    "in_progress",
    "Finish courier and scout enemy model readability pass",
    "actor-gds-art",
    "gds-art-bible",
  ),
  card(
    "card-gds-ui-kit",
    "board-gds-creative",
    "thread-gds-art-pipeline",
    "review",
    "Finalize tactical command UI kit for capture build",
    "actor-gds-art",
    "gds-art-bible",
  ),
  card(
    "card-gds-animation-pass",
    "board-gds-production",
    "thread-gds-art-pipeline",
    "backlog",
    "Clean dash, parry, and stagger animation handoff",
    "actor-gds-art",
    "gds-art-bible",
  ),
  card(
    "card-gds-bug-bash",
    "board-gds-launch",
    "thread-gds-launch",
    "in_progress",
    "Run QA bug bash and triage release blockers",
    "actor-gds-qa",
    "gds-launch-checklist",
  ),
  card(
    "card-gds-vertical-slice",
    "board-gds-launch",
    "thread-gds-vertical-slice",
    "blocked",
    "Prepare vertical slice capture candidate build",
    "actor-gds-producer",
    "gds-studio-brief",
  ),
  card(
    "card-gds-store-page",
    "board-gds-launch",
    "thread-gds-launch",
    "ready",
    "Finalize store page copy and community post",
    "actor-gds-producer",
    "gds-launch-checklist",
  ),
  card(
    "card-gds-audio-mix",
    "board-gds-creative",
    "thread-gds-art-pipeline",
    "backlog",
    "Balance hub ambience against tactical alerts",
    "actor-gds-narrative",
    "gds-narrative-bible",
  ),
  card(
    "card-gds-photo-mode-backlog",
    "board-gds-launch",
    "thread-gds-launch",
    "backlog",
    "Evaluate photo-mode toggle for post-demo wishlist",
    "actor-gds-producer",
    "gds-launch-checklist",
  ),
  card(
    "card-gds-accessibility-backlog",
    "board-gds-production",
    "thread-gds-core-loop",
    "backlog",
    "Scope subtitle size and input-remap accessibility pass",
    "actor-gds-qa",
    "gds-studio-brief",
  ),
  card(
    "card-gds-capsule-done",
    "board-gds-launch",
    "thread-gds-launch",
    "done",
    "Approve store capsule art for demo announcement",
    "actor-gds-art",
    "gds-launch-checklist",
    {
      resolutionRefs: ["event:evt-gds-launch-001"],
    },
  ),
  card(
    "card-gds-checkpoint-done",
    "board-gds-production",
    "thread-gds-vertical-slice",
    "done",
    "Stage demo checkpoint save for recovery testing",
    "actor-gds-gameplay",
    "gds-studio-brief",
    {
      resolutionRefs: ["event:evt-gds-vertical-slice-001"],
    },
  ),
];

export function getGameDevStudioSeedData() {
  return {
    actors,
    topics,
    documents: documents.map((document) => ({
      id: document.id,
      title: document.title,
      slug: document.slug,
      backing_thread_id: document.backing_thread_id,
      topic_ref: document.topic_ref,
      created_by: document.created_by,
    })),
    documentRevisions: Object.fromEntries(
      documents.map((document) => [
        document.id,
        document.revisions.map((content, index) => ({
          revision_number: index + 1,
          created_by: index === 0 ? document.created_by : "actor-gds-producer",
          content_type: "text",
          content,
        })),
      ]),
    ),
    boards,
    cards,
    events: buildEvents(),
  };
}

function card(
  id,
  boardID,
  topicThreadID,
  columnKey,
  summary,
  assignee,
  pinnedDocumentID,
  options = {},
) {
  return {
    id,
    board_id: boardID,
    message_thread_id: id,
    topic_ref: topicThreadID.replace(/^thread-/, ""),
    column_key: columnKey,
    summary,
    title: summary,
    assignee,
    pinned_document_id: pinnedDocumentID,
    related_refs: [`document:${pinnedDocumentID}`],
    resolution_refs: options.resolutionRefs ?? [],
    created_by: assignee,
    updated_by: assignee,
  };
}

function buildEvents() {
  const events = [];
  let minute = 0;
  for (const topic of topics) {
    addConversation(events, {
      idPrefix: `evt-${topic.id}`,
      threadId: topic.thread_id,
      subjectKind: "topic",
      subjectId: topic.id,
      subjectRef: `topic:${topic.id}`,
      count: 12,
      actors: [
        "actor-gds-producer",
        topic.owner_refs[0]?.replace("actor:", "") ?? "actor-gds-producer",
        "actor-gds-qa",
        "actor-gds-art",
        "actor-gds-narrative",
        "actor-gds-gameplay",
      ],
      summaryNoun: topic.title,
      minuteStart: minute,
    });
    minute += 20;
  }
  for (const document of documents) {
    addConversation(events, {
      idPrefix: `evt-gds-doc-${document.id.replace(/^gds-/, "")}`,
      threadId: document.backing_thread_id,
      subjectKind: "document",
      subjectId: document.id,
      subjectRef: `document:${document.id}`,
      count: 10,
      actors: [
        document.created_by,
        "actor-gds-producer",
        "actor-gds-qa",
        "actor-gds-gameplay",
        "actor-gds-art",
        "actor-gds-narrative",
      ],
      summaryNoun: document.title,
      minuteStart: minute,
    });
    minute += 12;
  }
  for (const item of cards) {
    addConversation(events, {
      idPrefix: `evt-${item.id}`,
      threadId: item.id,
      subjectKind: "card",
      subjectId: item.id,
      subjectRef: `card:${item.id}`,
      count: 3,
      actors: [
        String(item.assignee ?? "actor-gds-producer"),
        "actor-gds-producer",
        "actor-gds-qa",
      ],
      summaryNoun: item.summary,
      minuteStart: minute,
    });
    minute += 5;
  }
  return events;
}

function addConversation(
  events,
  {
    idPrefix,
    threadId,
    subjectKind,
    subjectId,
    subjectRef,
    count,
    actors,
    summaryNoun,
    minuteStart,
  },
) {
  let parentId = "";
  for (let index = 1; index <= count; index += 1) {
    const id = `${idPrefix}-${String(index).padStart(3, "0")}`;
    const isReply = index % 3 !== 1 && parentId !== "";
    const actorId = actors[(index - 1) % actors.length];
    const refs = [`thread:${threadId}`, subjectRef];
    if (isReply) {
      refs.push(`event:${parentId}`);
    } else {
      parentId = id;
    }
    const payload = {
      kind: `${subjectKind}_message`,
      text: messageText(subjectKind, summaryNoun, actorId, index, isReply),
      subject_kind: subjectKind,
      subject_id: subjectId,
      subject_ref: subjectRef,
      ...(isReply ? { reply_to_event_id: parentId } : {}),
    };
    events.push({
      id,
      ts: new Date(baseTime + (minuteStart + index) * 60 * 1000).toISOString(),
      type: "message_posted",
      actor_id: actorId,
      thread_id: threadId,
      refs,
      summary: `${isReply ? "Reply" : "Update"} on ${summaryNoun}`,
      payload,
      provenance: { sources: ["seed:game-dev-studio"] },
    });
  }
}

function messageText(subjectKind, summaryNoun, actorId, index, isReply) {
  const role = actorId.replace("actor-gds-", "");
  const opener = isReply
    ? "Replying with the current read:"
    : "Posting the current status:";
  if (subjectKind === "card") {
    return `${opener} ${summaryNoun}. ${role} sees checkpoint ${index} as ${
      index % 2 === 0
        ? "ready for verification"
        : "still needing one owner decision"
    }. Next handoff is explicit so another agent can continue without local context.`;
  }
  if (subjectKind === "document") {
    return `${opener} ${summaryNoun}. ${role} marked the paragraph that affects demo readiness, named the blocking assumption, and left a concrete edit for the next revision.`;
  }
  return `${opener} ${summaryNoun}. ${role} recorded scope, risk, and next action ${index}; the note references the shared slice plan so the studio can coordinate across boards and docs.`;
}
