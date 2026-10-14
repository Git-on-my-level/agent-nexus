import { buildMockTopicWorkspaceFromThreadWorkspace } from "../../../src/lib/devSeedData.js";
const THREAD = "thread-durable-comment";
const json = (route, body, status = 200) =>
  route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
export async function setup(context, send) {
  await context.addInitScript(() => {
    localStorage.setItem("anx_ui_actor_id:local", "actor-comment");
    localStorage.setItem("workspaceTourSeen.local", "1");
  });
  await context.addCookies([
    {
      name: "anx_ui_session_local",
      value: "refresh-comment",
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
    },
  ]);
  await context.route("**/auth/session", (route) =>
    json(route, {
      authenticated: true,
      agent: {
        agent_id: "reader-comment",
        actor_id: "actor-comment",
        username: "reader",
      },
    }),
  );
  await context.route(/\/auth\/principals(?:\?.*)?$/, (route) =>
    json(route, { principals: [] }),
  );
  await context.route(/\/actors(?:\?.*)?$/, (route) =>
    json(route, {
      actors: [
        { id: "actor-comment", display_name: "Reader", tags: ["human"] },
      ],
    }),
  );
  const thread = {
    id: THREAD,
    type: "process",
    title: "Durable discussion",
    status: "active",
    current_summary: "A discussion",
    updated_at: "2026-10-13T00:00:00Z",
    provenance: { sources: ["inferred"] },
  };
  const events = [
    {
      id: "original-message",
      actor_id: "actor-comment",
      type: "message_posted",
      thread_id: THREAD,
      refs: [`thread:${THREAD}`],
      summary: "Original message",
      payload: { text: "Original message" },
      ts: "2026-10-13T00:00:00Z",
      provenance: { sources: ["inferred"] },
    },
  ];
  await context.route(
    (url) => url.pathname.endsWith(`/${THREAD}/workspace`),
    (route) => {
      const ws = {
        thread_id: THREAD,
        thread,
        events,
        artifacts: [],
        topics: [],
        cards: [],
        documents: [],
        threads: [thread],
        notification_receipts: {},
      };
      return json(
        route,
        route.request().url().includes("/topics/")
          ? buildMockTopicWorkspaceFromThreadWorkspace(ws, THREAD)
          : ws,
      );
    },
  );
  await context.route(
    (url) => url.pathname.endsWith(`/${THREAD}/timeline`),
    (route) => json(route, { events }),
  );
  await context.route(
    (url) => url.pathname === `/threads/${THREAD}`,
    (route) => json(route, { thread }),
  );
  await context.route(
    (url) => url.pathname === `/topics/${THREAD}`,
    (route) =>
      json(route, {
        topic: buildMockTopicWorkspaceFromThreadWorkspace(
          { thread, thread_id: THREAD, events: [] },
          THREAD,
        ).topic,
      }),
  );
  await context.route(/\/stream\/(?:events|inbox)(?:\?.*)?$/, (route) =>
    route.fulfill({ status: 200, contentType: "text/event-stream", body: "" }),
  );
  await context.route(/\/events(?:\?.*)?$/, (route) =>
    route.request().method() === "POST"
      ? send(route, events)
      : json(route, { events }),
  );
  for (const [pattern, body] of [
    [/\/docs(?:\?.*)?$/, { documents: [] }],
    [/\/inbox(?:\?.*)?$/, { items: [] }],
    [/\/pm\/decisions(?:\?.*)?$/, { items: [] }],
    [/\/pm\/actions(?:\?.*)?$/, { items: [] }],
    [/\/work(?:\?.*)?$/, { work: [] }],
    [/\/home\/unread(?:\?.*)?$/, { groups: [] }],
  ])
    await context.route(pattern, (route) => json(route, body));
}
