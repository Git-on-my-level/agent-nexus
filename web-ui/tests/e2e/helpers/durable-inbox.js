export const ACTOR = "actor-durable-e2e";
export const ITEM = {
  id: "inbox-durable",
  kind: "ask",
  status: "open",
  title: "Choose the rollout window",
  body: "Which window should we use?",
  thread_id: "thread-durable",
  subject_ref: "thread:thread-durable",
  source_event_id: "ask-durable",
  request_event_ref: "event:ask-durable",
  related_refs: ["thread:thread-durable"],
  response_proposals: ["Use the Monday window.", "Wait until Thursday."],
  notification_target_status: { resolvable: false },
  source_event_time: new Date().toISOString(),
};
const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
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
  for (const [pattern, body] of [
    [/\/pm\/decisions(?:\?.*)?$/, { items: [] }],
    [/\/pm\/actions(?:\?.*)?$/, { items: [] }],
    [/\/work(?:\?.*)?$/, { work: [] }],
    [/\/home\/unread(?:\?.*)?$/, { groups: [] }],
    [/\/events(?:\?.*)?$/, { events: [] }],
  ])
    await context.route(pattern, (route) => json(route, body));
  await context.route(
    /\/(?:stream\/(?:events|inbox)|inbox\/stream)(?:\?.*)?$/,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: "",
      }),
  );
  await context.route(/\/asks\/[^/?]+(?:\?.*)?$/, (route) =>
    json(route, { ask_id: "event:ask-durable", status: "open", delivery: [] }),
  );
  await context.route(/\/events\/ask-durable(?:\?.*)?$/, (route) =>
    json(route, {
      event: {
        id: "ask-durable",
        type: "human_attention_requested",
        thread_id: ITEM.thread_id,
        payload: { kind: "ask" },
      },
    }),
  );
  await context.route(
    (url) => url.pathname === "/inbox",
    (route) => json(route, { status: "open", items: [ITEM] }),
  );
  await context.route(
    (url) => url.pathname === `/inbox/${ITEM.id}`,
    (route) => json(route, { item: ITEM }),
  );
  await context.route(
    (url) => url.pathname === `/inbox/${ITEM.id}/respond`,
    send,
  );
}
