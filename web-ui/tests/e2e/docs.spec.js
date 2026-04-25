import { expect, test } from "@playwright/test";

test("create document flow — POST /docs and navigate to new document", async ({
  page,
}) => {
  const actorId = "actor-docs-create-e2e";
  let createCount = 0;
  let listCount = 0;
  let createPayload = null;
  const threads = [
    {
      id: "thread-docs",
      title: "Operations Thread",
      status: "active",
      priority: "p1",
      type: "process",
      tags: ["ops"],
    },
  ];
  const createdDoc = {
    id: "new-test-doc",
    title: "New Test Document",
    status: "draft",
    labels: ["ops"],
    thread_id: "thread-docs",
    head_revision_id: "rev-new-1",
    head_revision_number: 1,
    updated_at: new Date().toISOString(),
    updated_by: actorId,
  };
  const createdRevision = {
    revision_id: "rev-new-1",
    revision_number: 1,
    created_at: new Date().toISOString(),
    created_by: actorId,
    content_type: "text",
    content_hash: "content-hash-new",
    revision_hash: "revision-hash-new",
    content: "# New Test Document\n\nThis is created from the E2E test.",
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Doc Creator", tags: ["human"] }],
      }),
    });
  });

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ threads }),
    });
  });

  await page.route(/\/docs(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    if (request.method() === "GET") {
      listCount += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          documents: listCount > 1 ? [createdDoc] : [],
        }),
      });
      return;
    }

    if (request.method() === "POST") {
      createCount += 1;
      createPayload = JSON.parse(request.postData() ?? "{}");
      await route.fulfill({
        status: 201,
        contentType: "application/json",
        body: JSON.stringify({
          document: createdDoc,
          revision: createdRevision,
        }),
      });
      return;
    }

    await route.continue();
  });

  await page.route(/\/docs\/new-test-doc$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ document: createdDoc, revision: createdRevision }),
    });
  });

  await page.goto("/o/local/w/local/docs");
  await expect(page).toHaveURL(/\/o\/local\/w\/local\/docs$/);
  // Wait for network idle so the page is fully hydrated and client-side
  // effects have completed before interacting with buttons.
  await page.waitForLoadState("networkidle");
  await expect(
    page.getByRole("heading", { name: "Docs", exact: true }),
  ).toBeVisible();

  await page.getByRole("button", { name: "New doc" }).click();
  // The form appears inside {#if createOpen}; wait for textarea to confirm
  await expect(page.locator("textarea")).toBeVisible();

  // Use exact placeholder match to distinguish the title input from the textarea
  // (whose placeholder also starts with "# Document title").
  await page
    .getByPlaceholder("Document title", { exact: true })
    .fill("New Test Document");
  await page.getByPlaceholder("e.g. ops, runbook").fill("ops");
  await page.getByLabel("Thread linkage search").fill("Operations Thread");
  await page.getByRole("button", { name: /Operations Thread/ }).click();
  await page
    .locator("textarea")
    .fill("# New Test Document\n\nThis is created from the E2E test.");

  await page.getByRole("button", { name: "Create doc" }).click();

  await expect.poll(() => createCount).toBe(1);
  expect(createPayload).toMatchObject({
    actor_id: actorId,
    document: {
      title: "New Test Document",
      status: "draft",
      labels: ["ops"],
      thread_id: "thread-docs",
    },
  });
  await expect(page).toHaveURL(/\/o\/local\/w\/local\/docs\/new-test-doc$/);
  await expect(
    page.locator("section").getByRole("heading", { name: "New Test Document" }),
  ).toBeVisible();
});

test("update document flow — PATCH /docs/:id creates a new revision", async ({
  page,
}) => {
  const actorId = "actor-docs-update-e2e";
  let updateCount = 0;
  let updatePayload = null;
  const baseRevisionId = "rev-update-1";
  const newRevisionId = "rev-update-2";
  const threads = [
    {
      id: "thread-ops",
      title: "Operations Thread",
      status: "active",
      priority: "p1",
      type: "process",
      tags: ["ops"],
    },
    {
      id: "thread-policy",
      title: "Policy Thread",
      status: "active",
      priority: "p2",
      type: "process",
      tags: ["policy"],
    },
  ];

  const initialDoc = {
    id: "updatable-doc",
    title: "Updatable Document",
    status: "active",
    labels: ["ops"],
    thread_id: "thread-ops",
    head_revision_id: baseRevisionId,
    head_revision_number: 1,
    updated_at: "2026-03-08T10:00:00Z",
    updated_by: actorId,
  };

  const initialRevision = {
    revision_id: baseRevisionId,
    revision_number: 1,
    created_at: "2026-03-08T10:00:00Z",
    created_by: actorId,
    content_type: "text",
    content_hash: "hash-v1",
    revision_hash: "rhash-v1",
    content: "# Updatable Document\n\nOriginal content.",
  };

  const updatedDoc = {
    ...initialDoc,
    thread_id: "thread-policy",
    head_revision_id: newRevisionId,
    head_revision_number: 2,
    updated_at: new Date().toISOString(),
  };

  const updatedRevision = {
    revision_id: newRevisionId,
    revision_number: 2,
    prev_revision_id: baseRevisionId,
    created_at: new Date().toISOString(),
    created_by: actorId,
    content_type: "text",
    content_hash: "hash-v2",
    revision_hash: "rhash-v2",
    content: "# Updatable Document\n\nRevised content from E2E test.",
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Doc Editor", tags: ["human"] }],
      }),
    });
  });

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ threads }),
    });
  });

  await page.route(/\/docs\/updatable-doc$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    if (request.method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          document: updateCount === 0 ? initialDoc : updatedDoc,
          revision: updateCount === 0 ? initialRevision : updatedRevision,
        }),
      });
      return;
    }

    if (request.method() === "PATCH") {
      const payload = JSON.parse(request.postData() ?? "{}");
      updatePayload = payload;

      if (payload.if_base_revision !== baseRevisionId) {
        await route.fulfill({
          status: 409,
          contentType: "application/json",
          body: JSON.stringify({
            error: { code: "conflict", message: "Base revision mismatch." },
          }),
        });
        return;
      }

      updateCount += 1;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          document: updatedDoc,
          revision: updatedRevision,
        }),
      });
      return;
    }

    await route.continue();
  });

  await page.goto("/o/local/w/local/docs/updatable-doc");
  await expect(page).toHaveURL(/\/o\/local\/w\/local\/docs\/updatable-doc$/);
  await expect(
    page
      .locator("section")
      .getByRole("heading", { name: "Updatable Document" }),
  ).toBeVisible();
  await expect(page.getByText("Original content.")).toBeVisible();

  await page.getByRole("button", { name: "New revision" }).click();
  await expect(
    page.getByRole("button", { name: "Save revision" }),
  ).toBeVisible();

  await page.getByLabel("Thread linkage search").fill("Policy Thread");
  await page.getByRole("button", { name: /Policy Thread/ }).click();
  // The single textarea in the revision form (pre-filled with head content).
  await page.locator("textarea").fill("Revised content from E2E test.");

  await page.getByRole("button", { name: "Save revision" }).click();

  await expect.poll(() => updateCount).toBe(1);
  expect(updatePayload).toMatchObject({
    actor_id: actorId,
    if_base_revision: baseRevisionId,
    document: {
      thread_id: "thread-policy",
    },
  });

  await expect(page.getByText("Revised content from E2E test.")).toBeVisible();
  // Check that revision number v2 is shown in the metadata span (exact match
  // to avoid matching hash fields like "hash-v2" or "rhash-v2").
  await expect(
    page.locator("section").getByText("v2", { exact: true }),
  ).toBeVisible();
});

test("structured/binary content type — New revision button is hidden, CLI hint shown", async ({
  page,
}) => {
  const actorId = "actor-docs-structured-e2e";

  const doc = {
    id: "structured-doc",
    title: "Structured Document",
    status: "active",
    labels: [],
    head_revision_id: "rev-struct-1",
    head_revision_number: 1,
    updated_at: "2026-03-08T10:00:00Z",
    updated_by: actorId,
  };

  const revision = {
    revision_id: "rev-struct-1",
    revision_number: 1,
    created_at: "2026-03-08T10:00:00Z",
    created_by: actorId,
    content_type: "structured",
    content_hash: "hash-s1",
    revision_hash: "rhash-s1",
    content: '{"key":"value"}',
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [
          { id: actorId, display_name: "Structured Tester", tags: ["human"] },
        ],
      }),
    });
  });

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ threads: [] }),
    });
  });

  await page.route(/\/docs\/structured-doc$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }
    if (request.method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ document: doc, revision }),
      });
      return;
    }
    await route.continue();
  });

  await page.goto("/o/local/w/local/docs/structured-doc");
  await expect(
    page
      .locator("section")
      .getByRole("heading", { name: "Structured Document" }),
  ).toBeVisible();

  // "New revision" button must not appear for structured content
  await expect(page.getByRole("button", { name: "New revision" })).toHaveCount(
    0,
  );
  // CLI hint badge must appear instead
  await expect(page.getByText("structured — edit via CLI")).toBeVisible();
});

test("update document conflict — 409 response shows error", async ({
  page,
}) => {
  const actorId = "actor-docs-conflict-e2e";

  const doc = {
    id: "conflict-doc",
    title: "Conflict Document",
    status: "active",
    labels: [],
    head_revision_id: "rev-conflict-1",
    head_revision_number: 1,
    updated_at: "2026-03-08T10:00:00Z",
    updated_by: actorId,
  };

  const revision = {
    revision_id: "rev-conflict-1",
    revision_number: 1,
    created_at: "2026-03-08T10:00:00Z",
    created_by: actorId,
    content_type: "text",
    content_hash: "hash-c1",
    revision_hash: "rhash-c1",
    content: "# Conflict Document\n\nOriginal.",
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [
          { id: actorId, display_name: "Conflict Tester", tags: ["human"] },
        ],
      }),
    });
  });

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ threads: [] }),
    });
  });

  await page.route(/\/docs\/conflict-doc$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    if (request.method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ document: doc, revision }),
      });
      return;
    }

    if (request.method() === "PATCH") {
      await route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({
          error: {
            code: "conflict",
            message:
              "Base revision mismatch. Document was updated by another actor.",
          },
        }),
      });
      return;
    }

    await route.continue();
  });

  await page.goto("/o/local/w/local/docs/conflict-doc");
  await expect(
    page.locator("section").getByRole("heading", { name: "Conflict Document" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "New revision" }).click();
  await expect(
    page.getByRole("button", { name: "Save revision" }),
  ).toBeVisible();
  await page.locator("textarea").fill("Some conflicting changes.");
  await page.getByRole("button", { name: "Save revision" }).click();

  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.getByRole("alert")).toContainText(
    "Failed to save revision",
  );
});

test("documents list redirects through the default workspace and loads revision history", async ({
  page,
}) => {
  const actorId = "actor-docs-e2e";
  const threads = [
    {
      id: "thread-governance",
      title: "Governance Thread",
      status: "active",
      priority: "p1",
      type: "initiative",
      tags: ["governance"],
    },
  ];
  const documents = [
    {
      id: "product-constitution",
      title: "Product Constitution",
      status: "active",
      labels: ["governance", "product"],
      head_revision_id: "rev-pc-3",
      head_revision_number: 3,
      updated_at: "2026-03-08T14:30:00Z",
      updated_by: actorId,
    },
    {
      id: "incident-response-playbook",
      title: "Incident Response Playbook",
      status: "active",
      labels: ["ops", "runbook"],
      head_revision_id: "rev-irp-2",
      head_revision_number: 2,
      updated_at: "2026-03-05T11:00:00Z",
      updated_by: actorId,
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/threads(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ threads }),
    });
  });

  await page.route(/\/docs(\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ documents }),
    });
  });

  await page.route(/\/docs\/product-constitution$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        document: documents[0],
        revision: {
          revision_id: "rev-pc-3",
          revision_number: 3,
          created_at: "2026-03-08T14:30:00Z",
          created_by: actorId,
          content_type: "text",
          content_hash: "content-hash-3",
          revision_hash: "revision-hash-3",
          content: "# Product Constitution v3\n\nCurrent ratified version.",
        },
      }),
    });
  });

  await page.route(/\/docs\/product-constitution\/history$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        document_id: "product-constitution",
        revisions: [
          {
            revision_id: "rev-pc-1",
            revision_number: 1,
            created_at: "2026-02-15T10:00:00Z",
            created_by: actorId,
          },
          {
            revision_id: "rev-pc-2",
            revision_number: 2,
            created_at: "2026-02-28T16:00:00Z",
            created_by: actorId,
          },
          {
            revision_id: "rev-pc-3",
            revision_number: 3,
            created_at: "2026-03-08T14:30:00Z",
            created_by: actorId,
          },
        ],
      }),
    });
  });

  await page.route(
    /\/docs\/product-constitution\/revisions\/rev-pc-2$/,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          revision: {
            revision_id: "rev-pc-2",
            revision_number: 2,
            created_at: "2026-02-28T16:00:00Z",
            created_by: actorId,
            content_type: "text",
            content_hash: "content-hash-2",
            revision_hash: "revision-hash-2",
            content: "# Product Constitution v2\n\nPrior version.",
          },
        }),
      });
    },
  );

  await page.route(/\/docs\/incident-response-playbook$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        document: documents[1],
        revision: {
          revision_id: "rev-irp-2",
          revision_number: 2,
          created_at: "2026-03-05T11:00:00Z",
          created_by: actorId,
          content_type: "text",
          content_hash: "content-hash-irp-2",
          revision_hash: "revision-hash-irp-2",
          content: "# Incident Response Playbook v2\n\nCurrent response steps.",
        },
      }),
    });
  });

  await page.route(
    /\/docs\/incident-response-playbook\/history$/,
    async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          document_id: "incident-response-playbook",
          revisions: [
            {
              revision_id: "rev-irp-1",
              revision_number: 1,
              created_at: "2026-02-20T09:00:00Z",
              created_by: actorId,
            },
            {
              revision_id: "rev-irp-2",
              revision_number: 2,
              created_at: "2026-03-05T11:00:00Z",
              created_by: actorId,
            },
          ],
        }),
      });
    },
  );

  await page.goto("/o/local/w/local/docs");
  await expect(page).toHaveURL(/\/o\/local\/w\/local\/docs$/);
  await expect(
    page.getByRole("heading", { name: "Docs", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: /Product Constitution/ }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: /Incident Response Playbook/ }),
  ).toBeVisible();

  await page.getByRole("link", { name: /Product Constitution/ }).click();
  await expect(
    page.getByRole("heading", { name: "Product Constitution", exact: true }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Revision history" }).click();
  await expect(
    page.getByText("Current version", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: /Version 2/ }).click();
  await expect(
    page.getByText("Viewing revision 2", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByText("Prior version.", { exact: false }),
  ).toBeVisible();

  await page
    .locator('nav[aria-label="Breadcrumb"]')
    .getByRole("link", { name: "Docs", exact: true })
    .click();
  await expect(page).toHaveURL(/\/o\/local\/w\/local\/docs$/);
  await page.getByRole("link", { name: /Incident Response Playbook/ }).click();
  await expect(
    page.getByRole("heading", {
      name: "Incident Response Playbook",
      exact: true,
    }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Revision history" }).click();
  await expect(page.getByText("Version 3", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Version 1", { exact: true })).toBeVisible();
});
