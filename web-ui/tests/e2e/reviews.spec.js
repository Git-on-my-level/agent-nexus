import { expect, test } from "@playwright/test";

test("receipt artifact detail shows heading, JSON preview, and topic link", async ({
  page,
}) => {
  const actorId = "actor-review-e2e";
  const receiptId = "artifact-receipt-review-e2e";
  const cardRef = "card:e2e-onboarding-card";

  const receiptArtifact = {
    id: receiptId,
    kind: "attachment",
    thread_id: "thread-onboarding",
    summary: "Receipt for review flow test",
    refs: [cardRef, "thread:thread-onboarding"],
    created_at: "2026-03-04T06:00:00.000Z",
    created_by: actorId,
    provenance: { sources: ["event:ui"] },
  };

  const receiptPacket = {
    receipt_id: receiptId,
    subject_ref: cardRef,
    outputs: ["artifact:artifact-output-1"],
    verification_evidence: ["artifact:artifact-evidence-1"],
    changes_summary: "Receipt for review flow test",
    known_gaps: [],
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
        actors: [{ id: actorId, display_name: "Review Tester" }],
      }),
    });
  });

  await page.route(/\/artifacts\/[^/?]+$/, async (route) => {
    const request = route.request();
    const artifactId = request.url().split("/").at(-1) ?? "";
    if (request.method() === "GET" && request.resourceType() === "document") {
      await route.continue();
      return;
    }
    if (request.method() === "GET" && artifactId === receiptId) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ artifact: receiptArtifact }),
      });
      return;
    }
    await route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({ error: "Artifact not found" }),
    });
  });

  await page.route(/\/artifacts\/[^/?]+\/content$/, async (route) => {
    const artifactId = route.request().url().split("/").at(-2) ?? "";
    if (artifactId === receiptId) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(receiptPacket),
      });
      return;
    }
    await route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({ error: "Content not found" }),
    });
  });

  await page.goto(`/o/local/w/local/artifacts/${receiptId}`);
  await expect(
    page.getByRole("heading", { name: receiptArtifact.summary }),
  ).toBeVisible();

  await expect(
    page.getByRole("link", { name: /Topic thread-onboarding/ }),
  ).toBeVisible();

  await expect(
    page
      .locator("pre")
      .filter({ hasText: `"subject_ref"` })
      .filter({ hasText: "e2e-onboarding-card" })
      .first(),
  ).toBeVisible();
});
