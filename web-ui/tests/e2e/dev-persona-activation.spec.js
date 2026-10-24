import { expect, test } from "@playwright/test";
import { deferred, installWorkspaceApi } from "../helpers/workspaceApiMock.js";

const PATH = "/o/local/w/local/decide";

// A dev persona sign-in that outlives the activation deadline is retired with
// it. Its own cleanup is then ignored, so the resumed activation must not
// inherit its busy state: the persona switcher stays usable without a reload.
test("a retired persona sign-in does not leave the persona switcher disabled", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const api = await installWorkspaceApi(page, {
    authenticated: false,
    devActorMode: true,
  });
  const persona = {
    persona_id: "persona-operator",
    agent_id: api.self.agent_id,
    actor_id: api.self.actor_id,
    principal_kind: "human",
    display_label: "Sample operator",
    default: true,
  };
  const signIn = deferred();
  let signIns = 0;
  await page.route("**/auth/dev/identities", (route) =>
    route.fulfill({ json: { personas: [persona] } }),
  );
  await page.route("**/auth/dev/default-persona", (route) =>
    route.fulfill({ json: { persona } }),
  );
  await page.route("**/auth/dev/session", async (route) => {
    signIns++;
    await signIn.promise;
    api.authenticated = true;
    return route.fulfill({ json: { ok: true } });
  });
  await page.clock.install({ time: new Date("2030-01-01T00:00:00Z") });
  await page.clock.pauseAt(new Date("2030-01-01T00:00:01Z"));
  try {
    await page.goto(PATH);
    await expect.poll(() => signIns).toBe(1);
    // The activation deadline retires the stalled sign-in.
    await page.clock.runFor(15_001);
    // The session now exists, so the resumed activation signs in without a
    // persona step and never touches the busy state again.
    signIn.resolve();
    await expect.poll(() => api.authenticated).toBe(true);
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await page.getByRole("button", { name: "Account menu" }).click();
    await expect(
      page.getByRole("button", { name: "Switch fixture persona" }),
    ).toBeEnabled();
    expect(signIns).toBe(1);
  } finally {
    signIn.resolve();
  }
});
