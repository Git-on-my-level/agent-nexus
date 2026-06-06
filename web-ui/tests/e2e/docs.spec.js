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
      type: "process",
    },
  ];
  const createdDoc = {
    id: "new-test-doc",
    title: "New Test Document",
    status: "draft",
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
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
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
  await expect(
    page.getByRole("textbox", { name: "Head content (Markdown) *" }),
  ).toBeVisible();

  // Use exact placeholder match to distinguish the title input from the textarea
  // (whose placeholder also starts with "# Document title").
  await page
    .getByPlaceholder("Document title", { exact: true })
    .fill("New Test Document");
  await page
    .getByRole("textbox", { name: "Head content (Markdown) *" })
    .fill("# New Test Document\n\nThis is created from the E2E test.");

  await page.getByRole("button", { name: "Create doc" }).click();

  await expect.poll(() => createCount).toBe(1);
  expect(createPayload).toMatchObject({
    actor_id: actorId,
    document: {
      title: "New Test Document",
    },
  });
  await expect(page).toHaveURL(/\/o\/local\/w\/local\/docs\/new-test-doc$/);
  await expect(
    page.getByRole("heading", { name: "New Test Document" }).first(),
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
      type: "process",
    },
    {
      id: "thread-policy",
      title: "Policy Thread",
      status: "active",
      type: "process",
    },
  ];

  const initialDoc = {
    id: "updatable-doc",
    title: "Updatable Document",
    status: "active",
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
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
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

  await page.route(/\/docs\/updatable-doc\/revisions$/, async (route) => {
    const request = route.request();
    if (request.method() !== "POST") {
      await route.continue();
      return;
    }

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
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({
        document: updatedDoc,
        revision: updatedRevision,
      }),
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

    await route.continue();
  });

  await page.goto("/o/local/w/local/docs/updatable-doc");
  await expect(page).toHaveURL(/\/o\/local\/w\/local\/docs\/updatable-doc$/);
  await expect(
    page.getByRole("heading", { name: "Updatable Document" }).first(),
  ).toBeVisible();
  await expect(page.getByText("Original content.")).toBeVisible();

  await page.getByRole("button", { name: "Edit" }).click();
  await expect(
    page.getByRole("button", { name: "Save revision" }),
  ).toBeVisible();

  // The single textarea in the revision form (pre-filled with head content).
  await page
    .getByRole("textbox", { name: /Content \(Markdown\)/ })
    .fill("Revised content from E2E test.");

  await page.getByRole("button", { name: "Save revision" }).click();

  await expect.poll(() => updateCount).toBe(1);
  expect(updatePayload).toMatchObject({
    actor_id: actorId,
    if_base_revision: baseRevisionId,
  });

  await expect(page.getByText("Revised content from E2E test.")).toBeVisible();
  // Check that revision number v2 is shown in the metadata span (exact match
  // to avoid matching hash fields like "hash-v2" or "rhash-v2").
  await expect(page.getByText("v2", { exact: true })).toBeVisible();
});

test("structured/binary content type — Edit button is hidden, CLI hint shown", async ({
  page,
}) => {
  const actorId = "actor-docs-structured-e2e";

  const doc = {
    id: "structured-doc",
    title: "Structured Document",
    status: "active",
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
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
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
    page.getByRole("heading", { name: "Structured Document" }).first(),
  ).toBeVisible();

  // In-app revision editor must not appear for structured content
  await expect(page.getByRole("button", { name: "Edit" })).toHaveCount(0);
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
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
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

  await page.route(/\/docs\/conflict-doc\/revisions$/, async (route) => {
    if (route.request().method() !== "POST") {
      await route.continue();
      return;
    }

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

    await route.continue();
  });

  await page.goto("/o/local/w/local/docs/conflict-doc");
  await expect(
    page.getByRole("heading", { name: "Conflict Document" }).first(),
  ).toBeVisible();

  await page.getByRole("button", { name: "Edit" }).click();
  await expect(
    page.getByRole("button", { name: "Save revision" }),
  ).toBeVisible();
  await page
    .getByRole("textbox", { name: /Content \(Markdown\)/ })
    .fill("Some conflicting changes.");
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
      type: "initiative",
    },
  ];
  const documents = [
    {
      id: "product-constitution",
      title: "Product Constitution",
      status: "active",
      head_revision_id: "rev-pc-3",
      head_revision_number: 3,
      updated_at: "2026-03-08T14:30:00Z",
      updated_by: actorId,
    },
    {
      id: "incident-response-playbook",
      title: "Incident Response Playbook",
      status: "active",
      thread_id: "thread-governance",
      head_revision_id: "rev-irp-2",
      head_revision_number: 2,
      updated_at: "2026-03-05T11:00:00Z",
      updated_by: actorId,
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Docs Revision Tester" }],
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

  await page.route(
    /\/docs\/product-constitution\/revisions$/,
    async (route) => {
      const request = route.request();
      if (request.method() !== "GET") {
        await route.continue();
        return;
      }
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
    },
  );

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

  await page.route(
    /\/threads\/thread-governance\/timeline(\?.*)?$/,
    async (route) => {
      const request = route.request();
      if (request.method() !== "GET") {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ events: [] }),
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
    /\/docs\/incident-response-playbook\/revisions$/,
    async (route) => {
      const request = route.request();
      if (request.method() !== "GET") {
        await route.continue();
        return;
      }
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

  await page.getByRole("button", { name: "More actions" }).click();
  await page.getByRole("menuitem", { name: "Revision history" }).click();
  await expect(page.getByText("Current version")).toBeVisible();
  await page.getByRole("button", { name: /Version 2/ }).click();
  await expect(
    page.getByText("Viewing revision 2", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByText("Prior version.", { exact: false }),
  ).toBeVisible();

  await page
    .getByRole("navigation", { name: /Breadcrumb and document status/ })
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
  await page.getByRole("button", { name: "More actions" }).click();
  await expect(page.getByRole("menuitem", { name: "Settings" })).toBeVisible();
  await expect(
    page.getByRole("menuitem", { name: "Revision history" }),
  ).toHaveCount(0);
  await expect(page.getByText("Version 3", { exact: true })).toHaveCount(0);
});

test("inline title rename — commits a new revision carrying the title patch", async ({
  page,
}) => {
  const actorId = "actor-docs-rename-e2e";
  const baseRevisionId = "rev-rename-1";
  let renamePayload = null;
  let renameCount = 0;

  const initialDoc = {
    id: "renamable-doc",
    title: "Old Title",
    status: "active",
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
    content_hash: "hash-r1",
    revision_hash: "rhash-r1",
    content: "# Old Title\n\nBody stays the same.",
  };
  const renamedDoc = {
    ...initialDoc,
    title: "Fresh Title",
    head_revision_id: "rev-rename-2",
    head_revision_number: 2,
    updated_at: new Date().toISOString(),
  };
  const renamedRevision = {
    ...initialRevision,
    revision_id: "rev-rename-2",
    revision_number: 2,
    prev_revision_id: baseRevisionId,
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Doc Renamer", tags: ["human"] }],
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

  await page.route(/\/docs\/renamable-doc\/revisions$/, async (route) => {
    const request = route.request();
    if (request.method() !== "POST") {
      await route.continue();
      return;
    }
    renamePayload = JSON.parse(request.postData() ?? "{}");
    renameCount += 1;
    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({ document: renamedDoc, revision: renamedRevision }),
    });
  });

  await page.route(/\/docs\/renamable-doc$/, async (route) => {
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
          document: renameCount === 0 ? initialDoc : renamedDoc,
          revision: renameCount === 0 ? initialRevision : renamedRevision,
        }),
      });
      return;
    }
    await route.continue();
  });

  await page.goto("/o/local/w/local/docs/renamable-doc");
  await expect(
    page.getByRole("heading", { name: "Old Title" }).first(),
  ).toBeVisible();

  await page.getByRole("button", { name: "Rename document" }).click();
  const titleInput = page.getByRole("textbox", { name: "Document title" });
  await expect(titleInput).toBeVisible();
  await titleInput.fill("Fresh Title");
  await titleInput.press("Enter");

  await expect.poll(() => renameCount).toBe(1);
  expect(renamePayload).toMatchObject({
    if_base_revision: baseRevisionId,
    document: { title: "Fresh Title" },
  });
  await expect(
    page.getByRole("heading", { name: "Fresh Title" }).first(),
  ).toBeVisible();
});

test("document outline — wide viewport shows a table of contents that scrolls to a heading", async ({
  page,
}) => {
  const actorId = "actor-docs-toc-e2e";
  const doc = {
    id: "outline-doc",
    title: "Outline Doc",
    status: "active",
    head_revision_id: "rev-outline-1",
    head_revision_number: 1,
    updated_at: "2026-03-08T10:00:00Z",
    updated_by: actorId,
  };
  const revision = {
    revision_id: "rev-outline-1",
    revision_number: 1,
    created_at: "2026-03-08T10:00:00Z",
    created_by: actorId,
    content_type: "text",
    content_hash: "hash-o1",
    revision_hash: "rhash-o1",
    content:
      "# Overview\n\nIntro paragraph.\n\n## Architecture\n\nDetails about the system.\n\n## Rollout Plan\n\nPhased steps.",
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [
          { id: actorId, display_name: "Outline Tester", tags: ["human"] },
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

  await page.route(/\/docs\/outline-doc$/, async (route) => {
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

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/o/local/w/local/docs/outline-doc");
  await page.waitForLoadState("networkidle");

  const outline = page.getByRole("navigation", { name: "Document outline" });
  await expect(outline).toBeVisible();
  await expect(outline.getByText("On this page")).toBeVisible();
  await expect(
    outline.getByRole("button", { name: "Architecture" }),
  ).toBeVisible();
  await expect(
    outline.getByRole("button", { name: "Rollout Plan" }),
  ).toBeVisible();

  await outline.getByRole("button", { name: "Rollout Plan" }).click();
  // The matching heading anchor exists in the rendered body.
  await expect(
    page.locator('.js-doc-markdown-body [id="rollout-plan"]'),
  ).toBeVisible();
});

test("editor toolbar — formatting is undoable and caret-only headings do not select trailing text", async ({
  page,
}) => {
  const actorId = "actor-docs-toolbar-e2e";
  const doc = {
    id: "toolbar-doc",
    title: "Toolbar Doc",
    status: "active",
    head_revision_id: "rev-tb-1",
    head_revision_number: 1,
    updated_at: "2026-03-08T10:00:00Z",
    updated_by: actorId,
  };
  const revision = {
    revision_id: "rev-tb-1",
    revision_number: 1,
    created_at: "2026-03-08T10:00:00Z",
    created_by: actorId,
    content_type: "text",
    content_hash: "hash-tb",
    revision_hash: "rhash-tb",
    content: "Hello world",
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [
          { id: actorId, display_name: "Toolbar Tester", tags: ["human"] },
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

  await page.route(/\/docs\/toolbar-doc$/, async (route) => {
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

  await page.goto("/o/local/w/local/docs/toolbar-doc");
  await page.getByRole("button", { name: "Edit" }).click();
  const textarea = page.getByRole("textbox", { name: "Content (Markdown)" });
  await expect(textarea).toBeVisible();

  // Select the word "Hello" (offsets 0-5) and apply Bold.
  await textarea.evaluate((el) => {
    el.focus();
    el.setSelectionRange(0, 5);
  });
  await page.getByRole("button", { name: /^Bold/ }).click();
  await expect.poll(() => textarea.inputValue()).toBe("**Hello** world");

  // Cmd/Ctrl+Z reverts the toolbar insertion (native undo stack preserved).
  await textarea.focus();
  await page.keyboard.press("ControlOrMeta+z");
  await expect.poll(() => textarea.inputValue()).toBe("Hello world");

  // Caret-only H2 on a non-empty line prefixes the line without selecting
  // trailing text (regression: it used to grab characters on the next row).
  await textarea.evaluate((el) => {
    el.focus();
    el.setSelectionRange(0, 0);
  });
  await page.getByRole("button", { name: /^Heading 2/ }).click();
  await expect.poll(() => textarea.inputValue()).toBe("## Hello world");
  const collapsed = await textarea.evaluate(
    (el) => el.selectionStart === el.selectionEnd,
  );
  expect(collapsed).toBe(true);
});

test("editor — Cmd/Ctrl+S saves when dirty and is a no-op when clean", async ({
  page,
}) => {
  const actorId = "actor-docs-save-hotkey-e2e";
  const baseRevisionId = "rev-save-hk-1";
  let saveCount = 0;
  let savePayload = null;

  const doc = {
    id: "save-hotkey-doc",
    title: "Save Hotkey Doc",
    status: "active",
    head_revision_id: baseRevisionId,
    head_revision_number: 1,
    updated_at: "2026-03-08T10:00:00Z",
    updated_by: actorId,
  };
  const revision = {
    revision_id: baseRevisionId,
    revision_number: 1,
    created_at: "2026-03-08T10:00:00Z",
    created_by: actorId,
    content_type: "text",
    content_hash: "hash-shk",
    revision_hash: "rhash-shk",
    content: "Original body",
  };
  const savedRevision = {
    ...revision,
    revision_id: "rev-save-hk-2",
    revision_number: 2,
    prev_revision_id: baseRevisionId,
    content: "Edited via hotkey",
  };
  const savedDoc = {
    ...doc,
    head_revision_id: savedRevision.revision_id,
    head_revision_number: 2,
    updated_at: new Date().toISOString(),
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Save Tester", tags: ["human"] }],
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

  await page.route(/\/docs\/save-hotkey-doc\/revisions$/, async (route) => {
    const request = route.request();
    if (request.method() !== "POST") {
      await route.continue();
      return;
    }
    savePayload = JSON.parse(request.postData() ?? "{}");
    saveCount += 1;
    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({ document: savedDoc, revision: savedRevision }),
    });
  });

  await page.route(/\/docs\/save-hotkey-doc$/, async (route) => {
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
          document: saveCount === 0 ? doc : savedDoc,
          revision: saveCount === 0 ? revision : savedRevision,
        }),
      });
      return;
    }
    await route.continue();
  });

  await page.goto("/o/local/w/local/docs/save-hotkey-doc");
  await page.getByRole("button", { name: "Edit" }).click();
  const textarea = page.getByRole("textbox", { name: "Content (Markdown)" });
  await expect(textarea).toBeVisible();

  await textarea.focus();
  await page.keyboard.press("ControlOrMeta+s");
  await expect.poll(() => saveCount).toBe(0);

  await textarea.fill("Edited via hotkey");
  await textarea.focus();
  await page.keyboard.press("ControlOrMeta+s");
  await expect.poll(() => saveCount).toBe(1);
  expect(savePayload).toMatchObject({
    if_base_revision: baseRevisionId,
    content: "Edited via hotkey",
  });
});

test("doc more actions — Escape closes the kebab menu", async ({ page }) => {
  const actorId = "actor-docs-more-esc-e2e";
  const doc = {
    id: "more-actions-doc",
    title: "More Actions Doc",
    status: "active",
    head_revision_id: "rev-ma-1",
    head_revision_number: 1,
    updated_at: "2026-03-08T10:00:00Z",
    updated_by: actorId,
  };
  const revision = {
    revision_id: "rev-ma-1",
    revision_number: 1,
    created_at: "2026-03-08T10:00:00Z",
    created_by: actorId,
    content_type: "text",
    content_hash: "hash-ma",
    revision_hash: "rhash-ma",
    content: "Body",
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [
          { id: actorId, display_name: "More Actions", tags: ["human"] },
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

  await page.route(/\/docs\/more-actions-doc$/, async (route) => {
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

  await page.goto("/o/local/w/local/docs/more-actions-doc");
  await page.getByRole("button", { name: "More actions" }).click();
  await expect(page.getByRole("menuitem", { name: "Archive" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("menuitem", { name: "Archive" }),
  ).not.toBeVisible();
});

test("doc with thread — compact viewport uses bottom dock, not side rail", async ({
  page,
}) => {
  const actorId = "actor-docs-compact-shell";
  const threads = [
    {
      id: "thread-doc-threaded",
      title: "Threaded Doc Thread",
      status: "active",
      type: "process",
    },
  ];
  const doc = {
    id: "threaded-doc",
    title: "Threaded Policy",
    status: "active",
    thread_id: "thread-doc-threaded",
    head_revision_id: "rev-td-1",
    head_revision_number: 1,
    updated_at: "2026-03-08T12:00:00Z",
    updated_by: actorId,
  };
  const revision = {
    revision_id: "rev-td-1",
    revision_number: 1,
    created_at: "2026-03-08T12:00:00Z",
    created_by: actorId,
    content_type: "text",
    content_hash: "hash-td",
    revision_hash: "rhash-td",
    content: "# Threaded Policy\n\nBody.",
  };

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("anx_ui_actor_id:local", selectedActorId);
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Docs Compact Tester" }],
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

  await page.route(
    /\/threads\/thread-doc-threaded\/timeline(\?.*)?$/,
    async (route) => {
      const request = route.request();
      if (request.method() !== "GET") {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ events: [] }),
      });
    },
  );

  await page.route(/\/docs\/threaded-doc$/, async (route) => {
    const request = route.request();
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ document: doc, revision }),
    });
  });

  await page.setViewportSize({ width: 820, height: 900 });
  await page.goto("/o/local/w/local/docs/threaded-doc");
  await page.waitForLoadState("networkidle");

  await expect(
    page.locator(".shell-bottom-nav[aria-label='Primary navigation']"),
  ).toBeVisible();
  await expect(page.locator(".dd-rail")).toHaveCount(0);
  const dockFeed = page.locator(
    ".doc-detail-layout--with-rail .page-dock-feed",
  );
  await expect(dockFeed).toBeVisible();
  await expect(dockFeed.locator(".dd-surface")).toBeVisible();

  await page.setViewportSize({ width: 1280, height: 900 });
  await expect(page.locator(".shell-bottom-nav")).toBeHidden();
  await expect(page.locator(".dd-rail")).toBeVisible();
});
