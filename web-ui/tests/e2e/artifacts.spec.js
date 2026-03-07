import { expect, test } from "@playwright/test";

test("navigates from thread timeline artifact ref to artifact detail", async ({
  page,
}) => {
  const actorId = "actor-ops-ai";

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.goto("/threads/thread-lemon-shortage");

  await expect(
    page.getByRole("heading", { name: "Thread Detail: thread-lemon-shortage" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Timeline", exact: true }).click();
  const artifactRef = page
    .getByRole("link", { name: "artifact:artifact-supplier-sla" })
    .first();
  await expect(artifactRef).toBeVisible();
  await artifactRef.click();

  await expect(
    page.getByRole("heading", {
      name: "Artifact Detail: artifact-supplier-sla",
    }),
  ).toBeVisible();

  const textContentHeading = page.getByRole("heading", {
    name: "Text Content",
  });
  await expect(textContentHeading).toBeVisible();
  const textContentPanel = page
    .locator("div", { has: textContentHeading })
    .first();
  await expect(textContentPanel).toContainText("CitrusBot Farm Supplier SLA");

  await expect(
    page.getByText("Artifact", { exact: false }).first(),
  ).toBeVisible();
  await expect(page.getByText("ID: artifact-supplier-sla")).toBeVisible();
});
