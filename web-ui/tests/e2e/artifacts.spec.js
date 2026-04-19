import { expect, test } from "@playwright/test";

function filterArtifacts(items, url) {
  const kind = String(url.searchParams.get("kind") ?? "").trim();
  const threadId = String(url.searchParams.get("thread_id") ?? "").trim();
  const createdAfter = String(
    url.searchParams.get("created_after") ?? "",
  ).trim();
  const createdBefore = String(
    url.searchParams.get("created_before") ?? "",
  ).trim();

  return items.filter((artifact) => {
    if (kind && artifact.kind !== kind) {
      return false;
    }
    if (threadId && artifact.thread_id !== threadId) {
      return false;
    }
    if (
      createdAfter &&
      Date.parse(String(artifact.created_at)) < Date.parse(createdAfter)
    ) {
      return false;
    }
    if (
      createdBefore &&
      Date.parse(String(artifact.created_at)) > Date.parse(createdBefore)
    ) {
      return false;
    }
    return true;
  });
}

test("artifact filters are URL-backed and survive refresh", async ({
  page,
}) => {
  const actorId = "actor-artifacts-e2e";
  let artifactRequestCount = 0;
  const artifacts = [
    {
      id: "artifact-review-onboarding-1",
      kind: "review",
      thread_id: "thread-onboarding",
      summary: "Prepare onboarding plan",
      refs: ["thread:thread-onboarding"],
      created_at: "2026-03-04T08:00:00.000Z",
      created_by: actorId,
    },
    {
      id: "artifact-receipt-1",
      kind: "receipt",
      thread_id: "thread-onboarding",
      summary: "Collected onboarding evidence",
      refs: ["thread:thread-onboarding"],
      created_at: "2026-03-04T11:00:00.000Z",
      created_by: actorId,
    },
    {
      id: "artifact-receipt-2",
      kind: "receipt",
      thread_id: "thread-incident-42",
      summary: "Incident recovery evidence",
      refs: ["thread:thread-incident-42"],
      created_at: "2026-03-04T12:00:00.000Z",
      created_by: actorId,
    },
  ];

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.route(/\/actors(\?.*)?$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: actorId, display_name: "Artifacts Tester" }],
      }),
    });
  });

  await page.route(/\/artifacts(?:\?.*)?$/, async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") {
      await route.continue();
      return;
    }

    artifactRequestCount += 1;
    const url = new URL(request.url());
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        artifacts: filterArtifacts(artifacts, url),
      }),
    });
  });

  await page.goto("/o/local/w/local/artifacts");
  await expect.poll(() => artifactRequestCount).toBeGreaterThan(0);

  await expect(page.getByText("Prepare onboarding plan")).toBeVisible();
  await expect(page.getByText("Collected onboarding evidence")).toBeVisible();
  await expect(page.getByText("Incident recovery evidence")).toBeVisible();

  await page.getByRole("button", { name: "Filter" }).click();
  await page.getByLabel("Kind").selectOption("receipt");
  await page.getByLabel("Thread ID").fill("thread-onboarding");
  await page.getByRole("button", { name: "Apply" }).click();

  await expect(page).toHaveURL(
    /\/o\/local\/w\/local\/artifacts\?kind=receipt&thread_id=thread-onboarding$/,
  );
  await expect(page.getByText("Collected onboarding evidence")).toBeVisible();
  await expect(page.getByText("Prepare onboarding plan")).toHaveCount(0);
  await expect(page.getByText("Incident recovery evidence")).toHaveCount(0);

  await page.reload();

  await expect(page).toHaveURL(
    /\/o\/local\/w\/local\/artifacts\?kind=receipt&thread_id=thread-onboarding$/,
  );
  await expect(page.getByLabel("Kind")).toHaveValue("receipt");
  await expect(page.getByLabel("Thread ID")).toHaveValue("thread-onboarding");
  await expect(page.getByText("Collected onboarding evidence")).toBeVisible();
  await expect(page.getByText("Prepare onboarding plan")).toHaveCount(0);
  await expect(page.getByText("Incident recovery evidence")).toHaveCount(0);
});
