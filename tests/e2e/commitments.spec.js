import { expect, test } from "@playwright/test";

test("create commitment and enforce status evidence for done transition", async ({
  page,
}) => {
  const actorId = "actor-commitment-e2e";
  let createCount = 0;
  const patchPayloads = [];
  let snapshot = {
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
    open_commitments: [],
    next_check_in_at: "2026-03-05T00:00:00.000Z",
    updated_at: "2026-03-04T00:00:00.000Z",
    updated_by: actorId,
    provenance: { sources: ["actor_statement:event-1001"] },
  };
  const commitments = {};

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [
          { id: actorId, display_name: "Commitment Tester" },
          { id: "actor-policy-owner", display_name: "Policy Owner" },
        ],
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
      body: JSON.stringify({ thread: snapshot }),
    });
  });

  await page.route(/\/threads\/thread-onboarding\/timeline$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ events: [] }),
    });
  });

  await page.route(/\/commitments$/, async (route) => {
    const request = route.request();
    if (request.method() !== "POST") {
      await route.fulfill({
        status: 405,
        contentType: "application/json",
        body: JSON.stringify({ error: "Method not allowed" }),
      });
      return;
    }

    createCount += 1;
    const payload = JSON.parse(request.postData() ?? "{}");
    const commitmentId = `commitment-new-${createCount}`;
    const created = {
      id: commitmentId,
      ...payload.commitment,
      status: payload.commitment.status ?? "open",
      updated_at: "2026-03-04T01:00:00.000Z",
      updated_by: payload.actor_id,
      provenance: payload.commitment.provenance ?? {
        sources: ["actor_statement:ui"],
      },
    };

    commitments[commitmentId] = created;
    snapshot = {
      ...snapshot,
      open_commitments: [...snapshot.open_commitments, commitmentId],
      updated_at: "2026-03-04T01:00:00.000Z",
      updated_by: payload.actor_id,
    };

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ commitment: created }),
    });
  });

  await page.route(/\/commitments\/[^/?]+$/, async (route) => {
    const request = route.request();
    const commitmentId = request.url().split("/").at(-1) ?? "";
    const commitment = commitments[commitmentId];

    if (!commitment) {
      await route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: "Commitment not found" }),
      });
      return;
    }

    if (request.method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ commitment }),
      });
      return;
    }

    if (request.method() === "PATCH") {
      const payload = JSON.parse(request.postData() ?? "{}");
      patchPayloads.push(payload);

      const nextStatus = payload.patch?.status;
      if (
        nextStatus === "done" &&
        !(
          Array.isArray(payload.refs) &&
          payload.refs.some(
            (ref) => ref.startsWith("artifact:") || ref.startsWith("event:"),
          )
        )
      ) {
        await route.fulfill({
          status: 400,
          contentType: "application/json",
          body: JSON.stringify({
            error:
              "status=done requires artifact:<receipt_id> or event:<decision_event_id> in refs.",
          }),
        });
        return;
      }

      const updated = {
        ...commitment,
        ...payload.patch,
        updated_at: "2026-03-04T02:00:00.000Z",
        updated_by: payload.actor_id,
      };

      if (nextStatus === "done") {
        updated.provenance = {
          ...(updated.provenance ?? { sources: [] }),
          by_field: {
            ...((updated.provenance ?? {}).by_field ?? {}),
            status: payload.refs ?? [],
          },
        };
        snapshot = {
          ...snapshot,
          open_commitments: snapshot.open_commitments.filter(
            (id) => id !== commitmentId,
          ),
          updated_at: "2026-03-04T02:00:00.000Z",
          updated_by: payload.actor_id,
        };
      }

      commitments[commitmentId] = updated;
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ commitment: updated }),
      });
      return;
    }

    await route.continue();
  });

  await page.goto("/threads/thread-onboarding");

  await page.getByLabel("Commitment title").fill("Ship policy fix");
  await page.getByLabel("Owner").selectOption(actorId);
  await page
    .getByLabel("Due at (ISO timestamp)")
    .fill("2026-03-12T00:00:00.000Z");
  await page
    .getByLabel("Definition of done (comma/newline separated)")
    .fill("Merged\nReviewed");
  await page
    .getByLabel("Links (typed refs, comma/newline separated)")
    .fill("thread:thread-onboarding\nartifact:artifact-policy-draft");
  await page.getByRole("button", { name: "Create commitment" }).click();

  await expect.poll(() => createCount).toBe(1);
  await expect(
    page.getByRole("heading", { name: "Ship policy fix" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Edit commitment" }).click();
  await page.getByLabel("Commitment status").selectOption("done");
  await page.getByRole("button", { name: "Save commitment" }).click();

  await expect(
    page.getByText("Status done requires", { exact: false }),
  ).toBeVisible();
  await expect.poll(() => patchPayloads.length).toBe(0);

  await page
    .getByLabel("Status evidence ref (typed ref)")
    .fill("artifact:artifact-receipt-1");
  await page.getByRole("button", { name: "Save commitment" }).click();

  await expect.poll(() => patchPayloads.length).toBe(1);
  expect(patchPayloads[0]).toEqual({
    actor_id: actorId,
    patch: {
      status: "done",
    },
    refs: ["artifact:artifact-receipt-1"],
    if_updated_at: "2026-03-04T01:00:00.000Z",
  });

  await expect(
    page.getByText("No open commitments.", { exact: true }),
  ).toBeVisible();
});
