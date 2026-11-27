import { expect, test } from "@playwright/test";
import { BRIEF, LIST, setup } from "./helpers/durable-decide.js";

const id = "R-20261127-01";
const ref = `ruling:${id}`;
const title = "Use the new host routing rule";
const ruling = {
  ruling_id: id,
  status: "proposed",
  scope: "account",
  topic: "routing",
  question: title,
  source_quote: "Use the enrolled host.",
  relations: [{ kind: "supersedes", ruling_id: "R-20261126-08" }],
};

const json = (route, body) =>
  route.fulfill({
    contentType: "application/json",
    body: JSON.stringify(body),
  });

test("one tap confirms a ruling and retries the saved key after a lost receipt", async ({
  page,
  context,
}, testInfo) => {
  await setup(context, (route) => route.abort());
  await page.setViewportSize({ width: 1280, height: 900 });
  const requests = [];
  let confirmed = false;
  await context.route(
    (url) => url.pathname === "/brief",
    (route) =>
      json(route, {
        ...BRIEF,
        generated_at: new Date().toISOString(),
        decisions: confirmed
          ? []
          : [
              {
                ...BRIEF.decisions[0],
                ref,
                inbox_item_id: ref,
                kind: "ruling_proposal",
                title,
                body: "Send work to the host enrolled for this workspace.",
                proposals: ["Confirm ruling"],
                ruling_proposal: ruling,
              },
            ],
      }),
  );
  await context.route(`**/rulings/${id}/confirm`, (route) => {
    requests.push(route.request().postDataJSON());
    if (requests.length === 1) return route.abort("timedout");
    confirmed = true;
    return json(route, { ruling: { ...ruling, status: "active" } });
  });
  await page.goto(LIST);
  const button = page.getByRole("button", { name: "Confirm ruling" });
  await expect(button).toBeVisible();
  await expect(page.getByRole("blockquote")).toContainText(ruling.source_quote);
  await expect(page.locator("[data-decide-pick]")).toHaveCount(0);
  await testInfo.attach("ruling-decision", {
    body: await page.screenshot(),
    contentType: "image/png",
  });
  await button.click();
  await expect.poll(() => requests.length).toBe(1);
  expect(requests[0].idempotency_key).toBeTruthy();
  await page.reload();
  await expect.poll(() => requests.length, { timeout: 15_000 }).toBe(2);
  expect(requests[1]).toEqual(requests[0]);
  await expect(page.locator("[data-decide-row]")).toHaveCount(0);
});
