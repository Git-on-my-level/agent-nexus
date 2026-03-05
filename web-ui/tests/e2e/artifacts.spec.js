import { expect, test } from "@playwright/test";

test("navigates from thread timeline artifact ref to artifact detail", async ({
  page,
}) => {
  const actorId = "actor-artifact-nav-e2e";

  await page.addInitScript((selectedActorId) => {
    window.localStorage.setItem("oar_ui_actor_id", selectedActorId);
  }, actorId);

  await page.goto("/threads/thread-onboarding");

  await expect(
    page.getByRole("heading", { name: "Thread Detail: thread-onboarding" }),
  ).toBeVisible();

  const eventRow = page.locator("article", {
    hasText: "Waiting on legal review confirmation.",
  });
  await expect(eventRow).toBeVisible();

  await eventRow
    .getByRole("link", { name: "artifact:artifact-policy-draft" })
    .click();

  await expect(
    page.getByRole("heading", {
      name: "Artifact Detail: artifact-policy-draft",
    }),
  ).toBeVisible();

  const textContentHeading = page.getByRole("heading", {
    name: "Text Content",
  });
  await expect(textContentHeading).toBeVisible();
  const textContentPanel = textContentHeading.locator("xpath=..");
  await expect(textContentPanel).toContainText("Onboarding Policy Draft");
});
