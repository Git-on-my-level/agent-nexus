import { expect, test } from "@playwright/test";

const WS_HOME = "/o/local/w/local";

async function unlockShellWithActor(page, name) {
  await page.getByLabel("Display name").fill(name);
  await page.getByRole("button", { name: "Create and continue" }).click();
}

/**
 * Report a connected PM.
 *
 * The shell shows a PM surface only on a state core confirmed, and the PM
 * routes need an authenticated principal that the dev actor gate does not
 * establish — so against the e2e core `/pm/presence` answers 401 and the slot
 * stays a blank placeholder. A nav test should be about the nav, so it says
 * which state it is testing instead of inheriting the environment's.
 */
async function withConnectedPm(page) {
  await page.route("**/pm/presence", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        state: "connected",
        last_seen: new Date().toISOString(),
        runner: "Hermes",
        host: "studio",
        configured: true,
        connected: true,
      }),
    }),
  );
}

/** Report a PM that has never been onboarded, so the slot offers setup. */
async function withPmNotOnboarded(page) {
  await page.route("**/pm/presence", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ state: "not_onboarded" }),
    }),
  );
}

/** Tabs whose own label does not fit on one line at this viewport. */
async function crowdedBottomNavTabs(bottomNav) {
  return bottomNav.locator(".shell-bottom-nav-item").evaluateAll((els) =>
    els
      .map((el) => {
        const label = el.querySelector(":scope > .shell-bottom-nav-label");
        return {
          text: (label?.textContent || el.textContent || "")
            .replace(/\s+/g, " ")
            .trim(),
          itemOverflow: el.scrollWidth > el.clientWidth + 1,
          labelOverflow: label
            ? label.scrollWidth > label.clientWidth + 1
            : false,
          labelWrapped: label
            ? label.scrollHeight > label.clientHeight + 1 ||
              label.getClientRects().length > 1
            : false,
        };
      })
      .filter(
        (item) => item.itemOverflow || item.labelOverflow || item.labelWrapped,
      ),
  );
}

test("blocks shell with actor gate when no actor is selected", async ({
  page,
}) => {
  await page.goto(WS_HOME);

  await expect(
    page.getByRole("heading", { name: "Select Actor Identity" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Inbox" })).toHaveCount(0);
});

test("registers actor, unlocks shell, and opens Overview", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(WS_HOME);
  await page.getByLabel("Display name").fill("E2E User");
  await page.getByRole("button", { name: "Create and continue" }).click();

  await expect(page).toHaveURL(/\/o\/local\/w\/local\/overview/);
  await expect(
    page.getByRole("heading", { name: "Overview", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Tasks", exact: true }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Docs", exact: true }).first(),
  ).toBeVisible();
});

test("workspace root routes to Overview after activation without reloading", async ({
  page,
}) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  const documents = [];
  page.on("request", (request) => {
    if (request.resourceType() === "document") documents.push(request.url());
  });

  await page.goto(`${WS_HOME}?dashboard=weekly#overview-reports`);
  await expect(page).toHaveURL(
    `${test.info().project.use.baseURL}${WS_HOME}?dashboard=weekly#overview-reports`,
  );
  await expect(page.getByLabel("Display name")).toBeVisible();
  const historyLength = await page.evaluate(() => history.length);
  await unlockShellWithActor(page, `Inbox User ${Date.now()}`);

  await expect(page).toHaveURL(
    `${test.info().project.use.baseURL}${WS_HOME}/overview?dashboard=weekly#overview-reports`,
  );
  await expect(
    page.getByRole("heading", { name: "Overview", exact: true }),
  ).toBeVisible();
  expect(documents).toHaveLength(1);
  expect(await page.evaluate(() => history.length)).toBe(historyLength);
});

test("mobile bottom navigation switches workspace routes", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });
  await withConnectedPm(page);

  await page.goto(`${WS_HOME}/inbox`);
  await unlockShellWithActor(page, `Mobile User ${Date.now()}`);

  const bottomNav = page.getByRole("navigation", {
    name: "Primary navigation",
  });
  await bottomNav.getByRole("link", { name: "Tasks" }).click();

  await expect(page).toHaveURL(/\/o\/local\/w\/local\/tasks/);
  await expect(
    page.getByRole("heading", { name: "Tasks", exact: true }),
  ).toBeVisible();

  // Search left the bottom bar: the bar carries Overview, the product
  // surfaces, the PM slot and More. Workspace search is ⌘K plus a button in
  // each list header. The PM slot reads "Ask PM" because this test says a PM
  // is connected; with none onboarded it reads "PM setup", and until core
  // answers it holds its space with no link at all.
  await expect(
    bottomNav.getByRole("button", { name: "Search workspace" }),
  ).toHaveCount(0);
  for (const name of [
    "Overview",
    "Inbox",
    "Agents",
    "Tasks",
    "Docs",
    "Ask PM",
    "More",
  ]) {
    await expect(bottomNav.getByRole("link", { name })).toBeVisible();
  }
  expect(await crowdedBottomNavTabs(bottomNav)).toEqual([]);

  await page.getByRole("button", { name: "Search workspace" }).click();
  await expect(
    page.getByRole("dialog", { name: "Command palette" }),
  ).toBeVisible();
});

test("bottom navigation keeps one line per tab while the PM is unconfigured", async ({
  page,
}) => {
  // The narrowest phone the shell targets, and the state the connected-PM
  // test above never reaches: the PM slot reads setup copy there, which is
  // the longest label the bar ever carries.
  await page.setViewportSize({ width: 320, height: 568 });
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });
  await withPmNotOnboarded(page);

  await page.goto(`${WS_HOME}/docs`);
  await unlockShellWithActor(page, `Setup User ${Date.now()}`);

  const bottomNav = page.getByRole("navigation", {
    name: "Primary navigation",
  });
  const pmTab = bottomNav.locator('[data-pm-nav="setup"]');
  await expect(pmTab).toBeVisible();

  expect(await crowdedBottomNavTabs(bottomNav)).toEqual([]);
});

test("Docs reaches workspace search without the sidebar", async ({ page }) => {
  // Below 1024px the sidebar that holds ⌘K is hidden, so a phone can only
  // reach workspace search through a list header.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(`${WS_HOME}/docs`);
  await unlockShellWithActor(page, `Docs Search User ${Date.now()}`);

  await expect(
    page.getByRole("heading", { name: "Docs", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Search workspace" }).click();
  await expect(
    page.getByRole("dialog", { name: "Command palette" }),
  ).toBeVisible();
});

test("sidebar account menu reaches settings and sign out", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.goto(`${WS_HOME}/inbox`);
  await unlockShellWithActor(page, `Menu User ${Date.now()}`);

  const sidebar = page.getByRole("complementary", { name: "Primary" });
  // The footer is one account row; every settings destination and the identity
  // action live in the menu it opens, not in a permanent block.
  await expect(sidebar.getByRole("link", { name: "Access" })).toHaveCount(0);

  await sidebar.getByRole("button", { expanded: false }).last().click();

  const menu = page.getByRole("menu");
  for (const name of ["Access", "Secrets", "Integrations", "Audit"]) {
    await expect(menu.getByRole("menuitem", { name })).toBeVisible();
  }
  await expect(
    menu.getByRole("button", { name: /Sign out|Switch identity/ }),
  ).toBeVisible();
});

test("identity input and its original creation key survive reload and a lost confirmation", async ({
  page,
}, testInfo) => {
  const bodies = [];
  await page.route("**/actors", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const body = route.request().postDataJSON();
    bodies.push(body);
    const created = await route.fetch();
    expect(created.status()).toBe(201);
    if (bodies.length === 1)
      return route.fulfill({
        status: 503,
        json: { error: { code: "unavailable" } },
      });
    return route.fulfill({ response: created });
  });
  await page.goto(WS_HOME);
  await page.getByLabel("Display name").fill("Saved identity");
  await page.reload();
  await expect(page.getByLabel("Display name")).toHaveValue("Saved identity");
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(page.getByLabel("Display name")).toHaveAttribute("readonly", "");
  await page.screenshot({ path: testInfo.outputPath("saved-identity.png") });
  await page.reload();
  await expect(page.getByLabel("Display name")).toHaveValue("Saved identity");
  expect(bodies).toHaveLength(1); // Restoration is never an automatic resend.
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect(page).toHaveURL(/\/overview/);
  expect(bodies).toHaveLength(2);
  expect(bodies[1]).toEqual(bodies[0]);
});

test("two pre-identity tabs retain their own original requests", async ({
  context,
}) => {
  const bodies = [];
  await context.route("**/actors", (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    bodies.push(route.request().postDataJSON());
    return route.fulfill({
      status: 503,
      json: { error: { code: "unavailable" } },
    });
  });
  const a = await context.newPage(),
    b = await context.newPage();
  await a.goto(WS_HOME);
  await a.getByLabel("Display name").fill("Identity A");
  await a.getByRole("button", { name: "Create and continue" }).click();
  await expect(a.getByRole("alert")).toBeVisible();
  await b.goto(WS_HOME);
  await b.getByLabel("Display name").fill("Identity B");
  await b.getByRole("button", { name: "Create and continue" }).click();
  await expect(b.getByRole("alert")).toBeVisible();
  const first = [...bodies];
  await a.reload();
  await b.reload();
  await expect(a.getByLabel("Display name")).toHaveValue("Identity A");
  await expect(b.getByLabel("Display name")).toHaveValue("Identity B");
  expect(bodies).toHaveLength(2);
  await a.getByRole("button", { name: "Create and continue" }).click();
  await expect.poll(() => bodies.length).toBe(3);
  await b.getByRole("button", { name: "Create and continue" }).click();
  await expect.poll(() => bodies.length).toBe(4);
  expect(bodies[2]).toEqual(first[0]);
  expect(bodies[3]).toEqual(first[1]);
});

test("a definitively rejected identity remains editable after reload", async ({
  page,
}) => {
  const bodies = [],
    actors = [];
  await page.route("**/actors", (route) => {
    if (route.request().method() !== "POST")
      return route.fulfill({ json: { actors } });
    const body = route.request().postDataJSON();
    bodies.push(body);
    if (bodies.length === 1)
      return route.fulfill({
        status: 409,
        json: { error: { code: "actor_exists" } },
      });
    actors.push(body.actor);
    return route.fulfill({ status: 201, json: { actor: body.actor } });
  });
  await page.goto(WS_HOME);
  await page.getByLabel("Display name").fill("Rejected identity");
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect(
    page.getByRole("button", { name: "Edit saved identity" }),
  ).toBeVisible();
  await page.reload();
  await page.getByRole("button", { name: "Edit saved identity" }).click();
  await page.getByLabel("Display name").fill("Corrected identity");
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect(page).toHaveURL(/\/overview/);
  expect(bodies[1].request_key).not.toBe(bodies[0].request_key);
  expect(bodies[1].actor.id).not.toBe(bodies[0].actor.id);
});

test("sign-in purges closed public forms after reloading on the login route", async ({
  page,
}) => {
  await page.route("**/actors", (route) =>
    route.request().method() === "POST"
      ? route.fulfill({ status: 503, json: { error: { code: "unavailable" } } })
      : route.fallback(),
  );
  await page.goto(WS_HOME);
  await page.getByLabel("Display name").fill("Closed identity draft");
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  const entries = () =>
    page.evaluate(() =>
      Object.keys(localStorage).filter(
        (key) =>
          key.startsWith("anx:public-input:v1:") &&
          key.includes("identity:create"),
      ),
    );
  expect((await entries()).length).toBeGreaterThan(0);
  await page.goto(`${WS_HOME}/login`);
  await page.reload();
  // Exercise the actual user session boundary after a fresh login-route load.
  // No token, credential or browser challenge is stored by this fixture.
  const agent = {
    agent_id: "verified-person",
    actor_id: "verified-person",
    principal_kind: "human",
  };
  await page.route("**/auth/passkey/dev/login", (route) =>
    route.fulfill({ json: { agent } }),
  );
  await page
    .getByLabel("Principal username (optional)")
    .fill("verified-person");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/o\/local\/w\/local\/overview$/);
  await expect.poll(entries).toEqual([]);
  await page.evaluate(async () => {
    const { clearAuthSession } = await import("/src/lib/authSession.js");
    clearAuthSession("local", { organizationSlug: "local", clearActor: true });
  });
  expect(await entries()).toEqual([]);
  await page.goto(WS_HOME);
  await expect(page.getByLabel("Display name")).toHaveValue("");
});

test("identity draft-changing controls are locked while explicit resend is in flight", async ({
  page,
}) => {
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  let attempts = 0;
  await page.route("**/actors", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const body = route.request().postDataJSON();
    if (++attempts === 1)
      return route.fulfill({
        status: 403,
        json: { error: { code: "forbidden" } },
      });
    await gate;
    await route.fulfill({ status: 201, json: { actor: body.actor } });
  });
  await page.goto(WS_HOME);
  await page.getByLabel("Display name").fill("Protected request");
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect(
    page.getByRole("button", { name: "Edit saved identity" }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect.poll(() => attempts).toBe(2);
  await expect(
    page.getByRole("button", { name: "Edit saved identity" }),
  ).toHaveCount(0);
  await expect(page.getByLabel("Display name")).toHaveAttribute("readonly", "");
  release();
  await expect(page).toHaveURL(/\/overview/);
});

test("an uncertain creation stays frozen after a later permission refusal", async ({
  page,
}) => {
  let attempts = 0;
  await page.route("**/actors", (route) =>
    route.request().method() === "POST"
      ? route.fulfill({
          status: ++attempts === 1 ? 503 : 403,
          json: { error: { code: "unavailable" } },
        })
      : route.fallback(),
  );
  await page.goto(WS_HOME);
  await page.getByLabel("Display name").fill("Uncertain identity");
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect.poll(() => attempts).toBe(2);
  await expect(
    page.getByRole("button", { name: "Edit saved identity" }),
  ).toHaveCount(0);
  await page.reload();
  await expect(page.getByLabel("Display name")).toHaveValue(
    "Uncertain identity",
  );
  await expect(page.getByLabel("Display name")).toHaveAttribute("readonly", "");
  await expect(
    page.getByRole("button", { name: "Edit saved identity" }),
  ).toHaveCount(0);
});

test("a tab closed before the response keeps its original creation frozen on denial", async ({
  context,
}) => {
  let firstBody,
    attempted = false;
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  await context.route("**/actors", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const body = route.request().postDataJSON();
    if (!attempted) {
      attempted = true;
      firstBody = body;
      const committed = await route.fetch();
      expect(committed.status()).toBe(201);
      await gate;
      return route.fulfill({ response: committed }).catch(() => {});
    }
    expect(body).toEqual(firstBody);
    return route.fulfill({
      status: 403,
      json: { error: { code: "forbidden" } },
    });
  });
  const page = await context.newPage();
  await page.goto(WS_HOME);
  await page.getByLabel("Display name").fill("Unconfirmed identity");
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect.poll(() => attempted).toBe(true);
  await page.reload();
  release();
  await expect(page.getByLabel("Display name")).toHaveValue(
    "Unconfirmed identity",
  );
  await page.getByRole("button", { name: "Create and continue" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Edit saved identity" }),
  ).toHaveCount(0);
  await expect(page.getByLabel("Display name")).toHaveAttribute("readonly", "");
});

test("a rejection in one tab cannot clear another tab's uncertain attempt", async ({
  context,
}) => {
  let requests = 0,
    release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  await context.route("**/actors", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const count = ++requests;
    if (count === 1)
      return route.fulfill({
        status: 400,
        json: { error: { code: "invalid_request" } },
      });
    if (count === 2) {
      await gate;
      return route
        .fulfill({ status: 503, json: { error: { code: "unavailable" } } })
        .catch(() => {});
    }
    return route.fulfill({
      status: 403,
      json: { error: { code: "forbidden" } },
    });
  });
  const a = await context.newPage(),
    b = await context.newPage();
  await a.goto(WS_HOME);
  await a.getByLabel("Display name").fill("Shared original request");
  await a.getByRole("button", { name: "Create and continue" }).click();
  await expect(
    a.getByRole("button", { name: "Edit saved identity" }),
  ).toBeVisible();
  await b.goto(WS_HOME);
  await b
    .getByRole("button", { name: "Review Shared original request" })
    .click();
  await expect(
    b.getByRole("button", { name: "Edit saved identity" }),
  ).toBeVisible();
  await a.getByRole("button", { name: "Create and continue" }).click();
  await expect.poll(() => requests).toBe(2);
  await expect(
    b.getByRole("button", { name: "Edit saved identity" }),
  ).toHaveCount(0);
  await b.getByRole("button", { name: "Create and continue" }).click();
  await expect.poll(() => requests).toBe(3);
  await expect(b.getByRole("alert")).toBeVisible();
  await expect(
    b.getByRole("button", { name: "Edit saved identity" }),
  ).toHaveCount(0);
  await b.reload();
  await expect(
    b.getByRole("button", { name: "Edit saved identity" }),
  ).toHaveCount(0);
  await expect(b.getByLabel("Display name")).toHaveAttribute("readonly", "");
  release();
});

test("a retired request in a stale tab remains editable after resend and reload", async ({
  context,
}) => {
  let sends = 0;
  await context.route("**/actors", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    sends++;
    return route.fulfill({
      status: 400,
      json: { error: { code: "invalid_request" } },
    });
  });
  const a = await context.newPage(),
    b = await context.newPage();
  await a.goto(WS_HOME);
  await a.getByLabel("Display name").fill("Retired identity");
  await a.getByRole("button", { name: "Create and continue" }).click();
  await expect(
    a.getByRole("button", { name: "Edit saved identity" }),
  ).toBeVisible();
  await b.goto(WS_HOME);
  await b.getByRole("button", { name: "Review Retired identity" }).click();
  await a.getByRole("button", { name: "Edit saved identity" }).click();
  await expect(a.getByLabel("Display name")).not.toHaveAttribute(
    "readonly",
    "",
  );
  await b.getByRole("button", { name: "Create and continue" }).click();
  await expect(b.getByRole("alert")).toBeVisible();
  expect(sends).toBe(1);
  await b.reload();
  await expect(
    b.getByRole("button", { name: "Edit saved identity" }),
  ).toBeVisible();
  await b.getByRole("button", { name: "Edit saved identity" }).click();
  await expect(b.getByLabel("Display name")).not.toHaveAttribute(
    "readonly",
    "",
  );
});
