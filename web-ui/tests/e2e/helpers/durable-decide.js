export const ACTOR = "actor-durable-e2e";
export const ITEM = {
  id: "inbox-durable",
  ref: "inbox:inbox-durable",
};
export const LIST = "/o/local/w/local/decide";
export const URL = `${LIST}/${ITEM.id}`;
export const ROW = `[data-decision-ref="${ITEM.ref}"]`;

/** One ask in a brief.v1 document, enough for Decide to render a row. */
export const BRIEF = {
  schema: "brief.v1",
  generated_at: new Date().toISOString(),
  principal: { id: "human:operator", display: "Operator", kind: "human" },
  decisions: [
    {
      ref: ITEM.ref,
      kind: "ask",
      inbox_item_id: ITEM.id,
      title: "Choose the rollout window",
      body: "Which window should we use?",
      requested_by: { id: "agent:codex", display: "codex" },
      created_at: new Date().toISOString(),
      decide_by: null,
      overdue_for_seconds: null,
      blocks: [],
      subject: null,
      proposals: ["Use the Monday window.", "Wait until Thursday."],
      blocks_count: 0,
      age_seconds: 60,
    },
  ],
  initiatives: [],
  your_asks: [],
  answered_for_you: [],
  truncated: {
    decisions: false,
    initiatives: false,
    your_asks: false,
    answered_for_you: false,
  },
};

const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

/** Stub the brief and `POST /inbox/{id}/respond`; `send` answers the latter. */
export async function setup(context, send) {
  await context.addInitScript((actor) => {
    localStorage.setItem("anx_ui_actor_id:local", actor);
    localStorage.setItem("workspaceTourSeen.local", "1");
  }, ACTOR);
  await context.route(/\/actors(?:\?.*)?$/, (route) =>
    json(route, {
      actors: [{ id: ACTOR, display_name: "Reader", tags: ["human"] }],
    }),
  );
  await context.route(
    /\/(?:stream\/(?:events|inbox)|inbox\/stream)(?:\?.*)?$/,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: "",
      }),
  );
  await context.route(
    (url) => url.pathname === "/brief",
    (route) =>
      json(route, { ...BRIEF, generated_at: new Date().toISOString() }),
  );
  await context.route(
    (url) => url.pathname === `/inbox/${ITEM.id}/respond`,
    send,
  );
}
