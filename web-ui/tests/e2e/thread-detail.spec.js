import { expect, test } from "@playwright/test";
import { webcrypto } from "node:crypto";

const bridgeProofKeyPromise = (async () => {
  const keyPair = await webcrypto.subtle.generateKey(
    { name: "ECDSA", namedCurve: "P-256" },
    true,
    ["sign", "verify"],
  );
  const publicKey = await webcrypto.subtle.exportKey("spki", keyPair.publicKey);
  return {
    privateKey: keyPair.privateKey,
    publicKeyB64: Buffer.from(publicKey).toString("base64"),
  };
})();

function stableJsonValue(value) {
  if (Array.isArray(value)) {
    return value.map((item) => stableJsonValue(item));
  }
  if (value && typeof value === "object") {
    return Object.keys(value)
      .sort()
      .reduce((normalized, key) => {
        normalized[key] = stableJsonValue(value[key]);
        return normalized;
      }, {});
  }
  return value;
}

async function signCheckinPayload(content) {
  const { privateKey } = await bridgeProofKeyPromise;
  const signature = await webcrypto.subtle.sign(
    { name: "ECDSA", hash: "SHA-256" },
    privateKey,
    Buffer.from(
      JSON.stringify(
        stableJsonValue({
          v: "agent-bridge-checkin-proof/v1",
          handle: String(content.handle ?? "").trim(),
          actor_id: String(content.actor_id ?? "").trim(),
          workspace_id: String(content.workspace_id ?? "").trim(),
          bridge_instance_id: String(content.bridge_instance_id ?? "").trim(),
          checked_in_at: String(content.checked_in_at ?? "").trim(),
          expires_at: String(content.expires_at ?? "").trim(),
        }),
      ),
      "utf8",
    ),
  );
  return Buffer.from(signature).toString("base64");
}

test("thread detail separates messages from timeline and nests replies", async ({
  page,
}) => {
  const actorId = "actor-thread-detail-e2e";
  const { publicKeyB64 } = await bridgeProofKeyPromise;
  let postedEvents = 0;
  let streamLastEventId = "";
  let timelineRequests = 0;
  let allowFirstTimelineResponse;
  let allowSecondTimelineResponse;
  const firstTimelineResponseGate = new Promise((resolve) => {
    allowFirstTimelineResponse = resolve;
  });
  const secondTimelineResponseGate = new Promise((resolve) => {
    allowSecondTimelineResponse = resolve;
  });
  let recentEvents = [
    {
      id: "evt-1002",
      ts: "2026-03-03T09:00:00.000Z",
      type: "message_posted",
      actor_id: actorId,
      thread_id: "thread-onboarding",
      refs: ["thread:thread-onboarding"],
      summary: "Latest workspace message",
      payload: { text: "Latest workspace message" },
      provenance: { sources: ["actor_statement:event-1002"] },
    },
  ];
  let timeline = [
    {
      id: "evt-0999",
      ts: "2026-03-03T08:00:00.000Z",
      type: "message_posted",
      actor_id: actorId,
      thread_id: "thread-onboarding",
      refs: ["thread:thread-onboarding"],
      summary: "Earlier timeline-only message",
      payload: { text: "Earlier timeline-only message" },
      provenance: { sources: ["actor_statement:event-0999"] },
    },
    {
      id: "evt-1002",
      ts: "2026-03-03T09:00:00.000Z",
      type: "message_posted",
      actor_id: actorId,
      thread_id: "thread-onboarding",
      refs: ["thread:thread-onboarding"],
      summary: "Latest workspace message",
      payload: { text: "Latest workspace message" },
      provenance: { sources: ["actor_statement:event-1002"] },
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);
  await page.context().addCookies([
    {
      name: "oar_ui_session_local",
      value: "test-refresh-token",
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
    },
  ]);

  await page.route("**/auth/session", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        authenticated: true,
        agent: {
          agent_id: "agent-ops-ai",
          actor_id: actorId,
          username: "ops-ai",
        },
      }),
    });
  });

  await page.route("**/auth/principals?**", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        principals: [
          {
            agent_id: "agent-m4-hermes",
            actor_id: "actor-m4-hermes",
            username: "m4-hermes",
            principal_kind: "agent",
            auth_method: "public_key",
            revoked: false,
          },
          {
            agent_id: "agent-jarvis",
            actor_id: "actor-jarvis",
            username: "jarvis",
            principal_kind: "agent",
            auth_method: "public_key",
            revoked: false,
          },
          {
            agent_id: "agent-clawd",
            actor_id: "actor-clawd",
            username: "clawd",
            principal_kind: "agent",
            auth_method: "public_key",
            revoked: false,
          },
        ],
      }),
    });
  });

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Thread Detail Tester" }],
      }),
    });
  });

  await page.route(/\/threads\/thread-onboarding$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        thread: {
          id: "thread-onboarding",
          type: "process",
          title: "Customer Onboarding Workflow",
          status: "active",
          priority: "p1",
          cadence: "weekly",
          tags: ["ops", "customer"],
          current_summary: "Thread detail summary.",
          next_actions: ["Collect legal signoff"],
          open_commitments: ["commitment-onboard-1"],
          next_check_in_at: "2026-03-05T00:00:00.000Z",
          updated_at: "2026-03-04T00:00:00.000Z",
          updated_by: actorId,
          provenance: { sources: ["actor_statement:event-1001"] },
        },
      }),
    });
  });

  await page.route("**/docs/agentreg.m4-hermes", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        document: {
          id: "agentreg.m4-hermes",
          status: "active",
        },
        revision: {
          content: {
            handle: "m4-hermes",
            actor_id: "actor-m4-hermes",
            status: "active",
            bridge_signing_public_key_spki_b64: publicKeyB64,
            bridge_checkin_event_id: "event-bridge-checkin-m4-hermes",
            workspace_bindings: [{ workspace_id: "local", enabled: true }],
          },
        },
      }),
    });
  });

  await page.route("**/docs/agentreg.jarvis", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        document: {
          id: "agentreg.jarvis",
          status: "active",
        },
        revision: {
          content: {
            handle: "jarvis",
            actor_id: "actor-jarvis",
            status: "pending",
            workspace_bindings: [{ workspace_id: "local", enabled: true }],
          },
        },
      }),
    });
  });

  await page.route("**/docs/agentreg.clawd", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        document: {
          id: "agentreg.clawd",
          status: "active",
        },
        revision: {
          content: {
            handle: "clawd",
            actor_id: "actor-clawd",
            status: "active",
            bridge_signing_public_key_spki_b64: publicKeyB64,
            bridge_checkin_event_id: "event-bridge-checkin-clawd",
            workspace_bindings: [{ workspace_id: "local", enabled: true }],
          },
        },
      }),
    });
  });

  await page.route(
    "**/events/event-bridge-checkin-m4-hermes",
    async (route) => {
      const payload = {
        handle: "m4-hermes",
        actor_id: "actor-m4-hermes",
        workspace_id: "local",
        bridge_instance_id: "bridge-hermes-1",
        checked_in_at: "2099-03-20T12:00:00Z",
        expires_at: "2099-03-20T12:05:00Z",
      };
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          event: {
            id: "event-bridge-checkin-m4-hermes",
            type: "agent_bridge_checked_in",
            payload: {
              ...payload,
              proof_signature_b64: await signCheckinPayload(payload),
            },
          },
        }),
      });
    },
  );

  await page.route("**/events/event-bridge-checkin-clawd", async (route) => {
    const payload = {
      handle: "clawd",
      actor_id: "actor-clawd",
      workspace_id: "local",
      bridge_instance_id: "bridge-clawd-1",
      checked_in_at: "2026-03-20T12:00:00Z",
      expires_at: "2026-03-20T12:05:00Z",
    };
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        event: {
          id: "event-bridge-checkin-clawd",
          type: "agent_bridge_checked_in",
          payload: {
            ...payload,
            proof_signature_b64: await signCheckinPayload(payload),
          },
        },
      }),
    });
  });

  await page.route(
    /\/threads\/thread-onboarding\/workspace(\?.*)?$/,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          thread_id: "thread-onboarding",
          thread: {
            id: "thread-onboarding",
            type: "process",
            title: "Customer Onboarding Workflow",
            status: "active",
            priority: "p1",
            cadence: "weekly",
            tags: ["ops", "customer"],
            current_summary: "Thread detail summary.",
            next_actions: ["Collect legal signoff"],
            open_commitments: ["commitment-onboard-1"],
            next_check_in_at: "2026-03-05T00:00:00.000Z",
            updated_at: "2026-03-04T00:00:00.000Z",
            updated_by: actorId,
            provenance: { sources: ["actor_statement:event-1001"] },
          },
          context: {
            recent_events: recentEvents,
            key_artifacts: [],
            open_commitments: [],
            documents: [
              {
                id: "doc-onboarding-runbook",
                title: "Onboarding Runbook",
                status: "active",
                updated_at: "2026-03-04T00:30:00.000Z",
                updated_by: actorId,
                labels: ["ops"],
                head_revision_id: "rev-onboarding-runbook-2",
                head_revision_number: 2,
                head_revision: {
                  revision_id: "rev-onboarding-runbook-2",
                  revision_number: 2,
                  content_type: "text",
                  created_at: "2026-03-04T00:30:00.000Z",
                },
              },
            ],
          },
          board_memberships: {
            items: [
              {
                board: {
                  id: "board-q2-launch",
                  title: "Q2 Launch Board",
                  status: "active",
                },
                card: {
                  board_id: "board-q2-launch",
                  thread_id: "thread-onboarding",
                  column_key: "backlog",
                  pinned_document_id: "doc-onboarding-runbook",
                },
              },
            ],
            count: 1,
          },
        }),
      });
    },
  );

  await page.route(/\/threads\/thread-onboarding\/timeline$/, async (route) => {
    timelineRequests += 1;
    if (timelineRequests === 1) {
      await firstTimelineResponseGate;
    }
    if (timelineRequests === 2) {
      await secondTimelineResponseGate;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ events: timeline }),
    });
  });

  await page.route(/\/events\/stream(\?.*)?$/, async (route) => {
    streamLastEventId =
      new URL(route.request().url()).searchParams.get("last_event_id") ?? "";
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: ": keepalive\n\n",
    });
  });

  await page.route(/\/docs\?thread_id=thread-onboarding$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        documents: [
          {
            id: "doc-onboarding-runbook",
            title: "Onboarding Runbook",
            status: "active",
            updated_at: "2026-03-04T00:30:00.000Z",
            updated_by: actorId,
            labels: ["ops"],
            head_revision_id: "rev-onboarding-runbook-2",
            head_revision_number: 2,
            head_revision: {
              revision_id: "rev-onboarding-runbook-2",
              revision_number: 2,
              content_type: "text",
              created_at: "2026-03-04T00:30:00.000Z",
            },
          },
        ],
      }),
    });
  });

  await page.route(/\/events$/, async (route) => {
    const payload = JSON.parse(route.request().postData() ?? "{}");
    postedEvents += 1;

    const created = {
      id: `event-new-${postedEvents}`,
      ts: "2026-03-04T01:00:00.000Z",
      actor_id: payload.actor_id,
      ...payload.event,
    };
    recentEvents = [created, ...recentEvents];
    timeline = [...timeline, created];

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ event: created }),
    });
  });

  await page.goto("/threads/thread-onboarding");
  await expect.poll(() => streamLastEventId).toBe("evt-1002");

  await expect(
    page.getByText("Thread-linked docs and current head revisions."),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: /Q2 Launch Board/ }),
  ).toHaveAttribute("href", /\/boards\/board-q2-launch$/);
  await expect(
    page.getByRole("link", {
      name: "Pinned doc: doc-onboarding-runbook",
    }),
  ).toHaveAttribute("href", /\/docs\/doc-onboarding-runbook$/);
  const docLink = page.getByRole("link", { name: /Onboarding Runbook/ });
  await expect(docLink).toBeVisible();
  await expect(docLink).toHaveAttribute(
    "href",
    /\/docs\/doc-onboarding-runbook\?revision=rev-onboarding-runbook-2$/,
  );
  await page.getByRole("tab", { name: "Messages" }).click();
  await expect(page).toHaveURL(
    /\/local\/threads\/thread-onboarding\?tab=messages$/,
  );
  await expect(
    page.getByText("Latest workspace message", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Loading messages...", { exact: true }),
  ).toHaveCount(0);
  allowFirstTimelineResponse();

  await expect(
    page.getByRole("heading", { name: "Customer Onboarding Workflow" }),
  ).toBeVisible();
  await expect(
    page.getByText(
      "Mention @handle to wake a registered agent in this workspace.",
      { exact: false },
    ),
  ).toBeVisible();
  await expect(
    page.locator('[role="tabpanel"]').getByRole("link", { name: "Access" }),
  ).toHaveAttribute("href", /\/local\/access$/);
  await expect(
    page.getByText("Earlier timeline-only message", { exact: true }),
  ).toBeVisible();
  await page.locator("#message-text").fill("@");
  await expect(page.locator("#message-mention-list")).toContainText(
    "@m4-hermes",
  );
  await expect(
    page.locator("#message-evt-1002").getByRole("button", { name: "Reply" }),
  ).toBeVisible();
  await page
    .locator("#message-evt-1002")
    .getByRole("button", { name: "Reply" })
    .click();
  await page.locator("#message-text").fill("Reply message from e2e");
  await page.getByRole("button", { name: "Post message" }).click();

  await expect.poll(() => postedEvents).toBe(1);
  await expect(
    page.getByText("Earlier timeline-only message", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Syncing…", { exact: true })).toBeVisible();
  await expect(
    page.getByText("Loading messages...", { exact: true }),
  ).toHaveCount(0);
  allowSecondTimelineResponse();

  await expect(
    page
      .locator("#message-evt-1002")
      .locator("#message-event-new-1")
      .getByText("Reply message from e2e", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("tab", { name: "Timeline" })).toBeVisible();

  await page.getByRole("tab", { name: "Timeline" }).click();
  await expect(page).toHaveURL(
    /\/local\/threads\/thread-onboarding\?tab=timeline$/,
  );
  await expect(page.locator("#message-text")).toHaveCount(0);
  await expect(
    page.getByText("Message: Reply message from e2e", { exact: true }),
  ).toBeVisible();

  await page.reload();

  await expect(page).toHaveURL(
    /\/local\/threads\/thread-onboarding\?tab=timeline$/,
  );
  await expect(page.locator("#message-text")).toHaveCount(0);
  await expect(
    page.getByRole("tab", { name: "Timeline", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await expect(
    page.getByText("Message: Reply message from e2e", { exact: true }),
  ).toBeVisible();
});

test("thread detail handles snapshot update conflict and retries after reload", async ({
  page,
}) => {
  const actorId = "actor-thread-edit-e2e";
  const patchRequests = [];
  let patchAttempt = 0;
  let threadSnapshot = {
    id: "thread-onboarding",
    type: "process",
    title: "Customer Onboarding Workflow",
    status: "active",
    priority: "p1",
    cadence: "weekly",
    tags: ["ops", "customer"],
    key_artifacts: ["artifact-policy-draft"],
    current_summary: "Thread detail summary.",
    next_actions: ["Collect legal signoff"],
    open_commitments: ["commitment-onboard-1"],
    next_check_in_at: "2026-03-05T00:00:00.000Z",
    updated_at: "2026-03-04T00:00:00.000Z",
    updated_by: actorId,
    provenance: { sources: ["actor_statement:event-1001"] },
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Thread Edit Tester" }],
      }),
    });
  });

  await page.route(/\/threads\/thread-onboarding$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    if (request.method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ thread: threadSnapshot }),
      });
      return;
    }

    if (request.method() === "PATCH") {
      const payload = JSON.parse(request.postData() ?? "{}");
      patchRequests.push(payload);
      patchAttempt += 1;

      if (patchAttempt === 1) {
        threadSnapshot = {
          ...threadSnapshot,
          title: "Server updated title",
          updated_at: "2026-03-04T02:00:00.000Z",
        };
        await route.fulfill({
          status: 409,
          contentType: "application/json",
          body: JSON.stringify({
            error: "Thread has been updated by another actor.",
            current: threadSnapshot,
          }),
        });
        return;
      }

      threadSnapshot = {
        ...threadSnapshot,
        ...payload.patch,
        updated_at: "2026-03-04T03:00:00.000Z",
        updated_by: payload.actor_id,
      };
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ thread: threadSnapshot }),
      });
      return;
    }

    await route.continue();
  });

  await page.route(
    /\/threads\/thread-onboarding\/workspace(\?.*)?$/,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          thread_id: "thread-onboarding",
          thread: threadSnapshot,
          context: {
            recent_events: [],
            key_artifacts: [],
            open_commitments: [],
            documents: [],
          },
        }),
      });
    },
  );

  await page.route(/\/threads\/thread-onboarding\/timeline$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ events: [] }),
    });
  });

  await page.route(/\/events\/stream(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: ": keepalive\n\n",
    });
  });

  await page.route(/\/docs\?thread_id=thread-onboarding$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ documents: [] }),
    });
  });

  await page.goto("/threads/thread-onboarding");

  await expect(
    page.getByRole("heading", { name: "Customer Onboarding Workflow" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Edit" }).click();
  await page.getByLabel("Title", { exact: true }).fill("Edited after conflict");
  await page.getByRole("button", { name: "Save" }).click();

  await expect(
    page.getByText("Thread was updated elsewhere.", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Server updated title" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Edit" }).click();
  await page.getByLabel("Title", { exact: true }).fill("Final merged title");
  await page.getByRole("button", { name: "Save" }).click();

  await expect(page.getByText("Changes saved.", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Final merged title" }),
  ).toBeVisible();

  expect(patchRequests).toHaveLength(2);
  expect(patchRequests[0]).toEqual({
    actor_id: actorId,
    patch: {
      cadence: "0 9 * * 1",
      title: "Edited after conflict",
    },
    if_updated_at: "2026-03-04T00:00:00.000Z",
  });
  expect(patchRequests[1]).toEqual({
    actor_id: actorId,
    patch: {
      cadence: "0 9 * * 1",
      title: "Final merged title",
    },
    if_updated_at: "2026-03-04T02:00:00.000Z",
  });
});

test("thread detail updates workspace panels from another actor via event stream", async ({
  page,
}) => {
  const actorId = "actor-live-thread-e2e";
  let timeline = [
    {
      id: "evt-live-1",
      ts: "2026-03-04T00:00:00.000Z",
      type: "message_posted",
      actor_id: actorId,
      thread_id: "thread-onboarding",
      refs: ["thread:thread-onboarding"],
      summary: "Initial activity",
      payload: { text: "Initial activity" },
    },
  ];
  let workOrders = [
    {
      id: "artifact-work-order-1",
      kind: "work_order",
      thread_id: "thread-onboarding",
      summary: "Initial work order",
      refs: ["thread:thread-onboarding"],
    },
  ];
  let threadSnapshot = {
    id: "thread-onboarding",
    type: "process",
    title: "Customer Onboarding Workflow",
    status: "active",
    priority: "p1",
    cadence: "weekly",
    tags: ["ops", "customer"],
    current_summary: "Initial thread summary.",
    next_actions: ["Collect legal signoff"],
    open_commitments: ["commitment-open-1"],
    next_check_in_at: "2026-03-05T00:00:00.000Z",
    updated_at: "2026-03-04T00:00:00.000Z",
    updated_by: actorId,
  };
  let contextDocuments = [
    {
      id: "doc-onboarding-runbook",
      title: "Onboarding Runbook",
      status: "active",
      updated_at: "2026-03-04T00:30:00.000Z",
      head_revision: {
        revision_id: "rev-onboarding-runbook-2",
        revision_number: 2,
        content_type: "text",
        created_at: "2026-03-04T00:30:00.000Z",
      },
    },
  ];
  let contextCommitments = [
    {
      id: "commitment-open-1",
      title: "Collect onboarding requirements",
      owner: actorId,
      due_at: "2026-03-07T00:00:00.000Z",
      status: "open",
      definition_of_done: [],
      links: ["thread:thread-onboarding"],
    },
  ];

  let releaseRemoteUpdate;
  const remoteUpdateReady = new Promise((resolve) => {
    releaseRemoteUpdate = resolve;
  });

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Live Thread Tester" }],
      }),
    });
  });

  await page.route(/\/threads\/thread-onboarding$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ thread: threadSnapshot }),
    });
  });

  await page.route(
    /\/threads\/thread-onboarding\/workspace(\?.*)?$/,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          thread_id: "thread-onboarding",
          thread: threadSnapshot,
          context: {
            recent_events: timeline,
            key_artifacts: [],
            open_commitments: contextCommitments,
            documents: contextDocuments,
          },
        }),
      });
    },
  );

  await page.route(/\/threads\/thread-onboarding\/timeline$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ events: timeline }),
    });
  });

  await page.route(/\/artifacts(\?.*)?$/, async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get("kind") === "work_order") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ artifacts: workOrders }),
      });
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ artifacts: [] }),
    });
  });

  await page.route(/\/events\/stream(\?.*)?$/, async (route) => {
    await remoteUpdateReady;
    await route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: `id: evt-live-remote\nevent: event\ndata: ${JSON.stringify({
        event: timeline[0],
      })}\n\n`,
    });
  });

  await page.goto("/threads/thread-onboarding");

  await expect(
    page.getByText("Collect onboarding requirements", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Onboarding Runbook", { exact: true }),
  ).toBeVisible();

  threadSnapshot = {
    ...threadSnapshot,
    current_summary: "Updated remotely by another actor.",
    updated_at: "2026-03-04T02:00:00.000Z",
    updated_by: "actor-remote",
  };
  contextDocuments = [
    ...contextDocuments,
    {
      id: "doc-remote-checklist",
      title: "Remote Coordination Checklist",
      status: "active",
      updated_at: "2026-03-04T02:00:00.000Z",
      head_revision: {
        revision_id: "rev-remote-checklist-1",
        revision_number: 1,
        content_type: "text",
        created_at: "2026-03-04T02:00:00.000Z",
      },
    },
  ];
  contextCommitments = [
    {
      id: "commitment-blocked-1",
      title: "Wait for legal approval",
      owner: actorId,
      due_at: "2026-03-04T01:00:00.000Z",
      status: "blocked",
      definition_of_done: [],
      links: ["thread:thread-onboarding"],
    },
    ...contextCommitments,
  ];
  workOrders = [
    ...workOrders,
    {
      id: "artifact-work-order-2",
      kind: "work_order",
      thread_id: "thread-onboarding",
      summary: "Remote follow-up work order",
      refs: ["thread:thread-onboarding"],
    },
  ];
  timeline = [
    {
      id: "evt-live-remote",
      ts: "2026-03-04T02:00:00.000Z",
      type: "message_posted",
      actor_id: "actor-remote",
      thread_id: "thread-onboarding",
      refs: ["thread:thread-onboarding"],
      summary: "Remote actor updated coordination context",
      payload: { text: "Remote actor updated coordination context" },
    },
    ...timeline,
  ];
  releaseRemoteUpdate();

  await expect(
    page.getByText("Updated remotely by another actor.", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Wait for legal approval", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Blocked", { exact: true })).toBeVisible();
  await expect(
    page.getByText("Remote Coordination Checklist", { exact: true }),
  ).toBeVisible();

  await page.getByRole("tab", { name: "Work" }).click();
  await expect(
    page.getByRole("combobox", { name: "Work order" }),
  ).toContainText("Remote follow-up work order");

  await page.getByRole("tab", { name: "Timeline" }).click();
  await expect(
    page.getByText("Remote actor updated coordination context", {
      exact: true,
    }),
  ).toBeVisible();
});
