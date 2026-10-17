import { expect, test } from "@playwright/test";
import { deferred } from "../helpers/workspaceApiMock.js";
import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

const WORKSPACE = "/o/local/w/local";
const PATH = `${WORKSPACE}/initiatives/card%3Areturn-test`;
async function visibility(page, state) {
  await page.evaluate((state) => {
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => state,
    });
    Object.defineProperty(document, "hidden", {
      configurable: true,
      get: () => state === "hidden",
    });
    document.dispatchEvent(new Event("visibilitychange"));
  }, state);
}

async function installPage(page) {
  const api = await installWorkspaceApi(page);
  await page.route("**/work/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    const body = path.endsWith("/plan")
      ? { plan: { steps: [] } }
      : path.endsWith("/observations")
        ? { observations: [] }
        : path.endsWith("/participants")
          ? { participants: [] }
          : {
              work: {
                ref: "card:return-test",
                title: "Return test initiative",
                phase: "in_progress",
                source: { authority: "nexus" },
              },
            };
    return route.fulfill({ json: body });
  });
  return api;
}

for (const scenario of [
  "same principal",
  "metadata change",
  "principal change",
  "network failure",
  "401",
  "refreshed cookie",
]) {
  test(`return after hours hidden: ${scenario}`, async ({ page }) => {
    test.setTimeout(90_000);
    const api = await installPage(page);
    let authModuleUrl;
    page.on("request", (request) => {
      if (new URL(request.url()).pathname === "/src/lib/authSession.js")
        authModuleUrl = request.url();
    });
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto(PATH);
    await expect(
      page.getByRole("heading", {
        name: "Return test initiative",
        exact: true,
      }),
    ).toBeVisible();
    await page.clock.install();
    // Local mode does not install the optional session-maintenance driver.
    // Start the same generic driver used by an adapter-managed shell.
    await page.evaluate(async (url) => {
      const { startWorkspaceSessionMaintenance } = await import(url);
      window.stopTestMaintenance = startWorkspaceSessionMaintenance();
    }, authModuleUrl);
    await visibility(page, "hidden");
    await page.clock.fastForward(6 * 60 * 60 * 1000);
    let requests = 0;
    if (scenario === "metadata change")
      api.self = { ...api.self, username: "renamed@example.com" };
    if (scenario === "principal change")
      api.self = {
        ...api.self,
        agent_id: "another-human",
        actor_id: "another-actor",
      };
    await page.route("**/auth/session", async (route) => {
      requests++;
      if (scenario === "network failure" && requests === 1)
        return route.abort("failed");
      if (scenario === "401")
        return route.fulfill({
          status: 401,
          json: { error: { code: "unauthorized" } },
        });
      return route.fulfill({
        json: { authenticated: true, agent: api.self },
        headers:
          scenario === "refreshed cookie"
            ? {
                "set-cookie":
                  "anx_ui_access_local__local=refreshed-token; Path=/; HttpOnly",
              }
            : {},
      });
    });
    if (scenario === "refreshed cookie") {
      // Expire access while retaining the refresh cookie. The mocked session
      // response models the server refreshing it; no tokens enter page JS.
      await page.context().addCookies([
        {
          name: "anx_ui_access_local__local",
          value: "expired-access-token",
          domain: "127.0.0.1",
          path: "/",
          httpOnly: true,
          expires: Math.floor(Date.now() / 1000) - 1,
        },
      ]);
    }
    await visibility(page, "visible");
    await expect.poll(() => requests).toBeGreaterThan(0);
    await page.clock.runFor(1000);
    if (scenario === "401") {
      await expect(page).toHaveURL(/\/login/);
      await expect(page.getByRole("heading").first()).toBeVisible();
    } else {
      await expect(
        page.getByRole("link", { name: "← Initiatives", exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("heading", {
          name: "Return test initiative",
          exact: true,
        }),
      ).toBeVisible();
    }
    if (scenario === "network failure") {
      await page.clock.runFor(60_000);
      await expect.poll(() => requests).toBeGreaterThan(1);
      await expect(
        page.getByRole("heading", {
          name: "Return test initiative",
          exact: true,
        }),
      ).toBeVisible();
    }
    if (scenario === "refreshed cookie") {
      const cookies = await page.context().cookies();
      expect(
        cookies.find((cookie) => cookie.name === "anx_ui_access_local__local")
          ?.value,
      ).toBe("refreshed-token");
    }
    await expect(page.locator("[data-workspace-empty-error]")).toHaveCount(0);
    expect(errors).toEqual([]);
  });
}

test("a missing route chunk recovers after an earlier deploy in the same tab", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await installPage(page);
  await page.goto(PATH);
  await expect(
    page.getByRole("heading", { name: "Return test initiative", exact: true }),
  ).toBeVisible();
  await page.evaluate(() =>
    sessionStorage.setItem("anx.chunk-recovery.v1", "1"),
  );
  await visibility(page, "hidden");
  let missing = 0;
  let documents = 0;
  page.on("request", (request) => {
    if (request.isNavigationRequest()) documents++;
  });
  await page.route("**/src/routes/**/docs/+page.svelte*", (route) => {
    missing++;
    return route.fulfill({
      status: 404,
      contentType: "text/plain",
      body: "Not found",
    });
  });
  await visibility(page, "visible");
  await page.evaluate((href) => {
    const link = document.createElement("a");
    link.href = href;
    link.textContent = "Test navigation to docs";
    document.querySelector("main").append(link);
    link.click();
    link.remove();
  }, `${WORKSPACE}/docs`);
  await expect.poll(() => missing).toBeGreaterThan(0);
  await expect.poll(() => documents).toBe(1);
  await expect(
    page.getByRole("heading", { name: "Return test initiative", exact: true }),
  ).toBeVisible();
  // The reload served the same broken build: a second failure must stop
  // automatic reloads and leave a user-controlled retry.
  await page.evaluate((href) => {
    const link = document.createElement("a");
    link.href = href;
    document.body.append(link);
    link.click();
    link.remove();
  }, `${WORKSPACE}/docs`);
  await expect(
    page.getByRole("heading", { name: "Something went wrong", exact: true }),
  ).toBeVisible();
  expect(documents).toBe(1);
  await page.unroute("**/src/routes/**/docs/+page.svelte*");
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Docs", exact: true }),
  ).toBeVisible();
});

test("an empty child route shows Retry on tab return (fault injection)", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await installPage(page);
  await page.goto(PATH);
  await expect(
    page.getByRole("heading", { name: "Return test initiative", exact: true }),
  ).toBeVisible();
  await visibility(page, "hidden");
  // A valid route module that renders no nodes does not throw. This exercises
  // the shell's missing-content invariant, not a proposed incident trigger.
  await page.route("**/src/routes/**/docs/+page.svelte*", (route) =>
    route.fulfill({
      contentType: "text/javascript",
      body: "export default function EmptyRoute() {}",
    }),
  );
  await page.evaluate((href) => {
    const link = document.createElement("a");
    link.href = href;
    document.querySelector("main").append(link);
    link.click();
    link.remove();
  }, `${WORKSPACE}/docs`);
  await expect(page).toHaveURL(`${WORKSPACE}/docs`);
  await visibility(page, "visible");
  await expect(
    page.getByRole("navigation", { name: "Primary", exact: true }),
  ).toBeVisible();
  const error = page.locator("[data-workspace-empty-error]");
  await expect(error).toContainText("This page could not be displayed.");
  await page.unroute("**/src/routes/**/docs/+page.svelte*");
  await error.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Docs", exact: true }),
  ).toBeVisible();
});

for (const resume of ["tab return", "overdue tab return", "online", "Retry"]) {
  test(`stalled activation recovers on ${resume} without a reload`, async ({
    page,
  }, testInfo) => {
    test.setTimeout(90_000);
    await installPage(page);
    await page.clock.install({ time: new Date("2030-01-01T00:00:00Z") });
    await page.clock.pauseAt(new Date("2030-01-01T00:00:01Z"));
    const stalled = deferred();
    let handshakes = 0;
    let documents = 0;
    page.on("request", (request) => {
      if (request.isNavigationRequest()) documents++;
    });
    await page.route("**/meta/handshake", async (route) => {
      handshakes++;
      if (handshakes === 1) await stalled.promise;
      return route.fallback();
    });
    try {
      await page.goto(PATH);
      await expect.poll(() => handshakes).toBe(1);
      const loading = page.getByLabel("Loading workspace content");
      await expect(loading).toBeVisible();
      await expect(loading.locator("p.text-meta")).toBeVisible();
      if (resume === "Retry") {
        const path = testInfo.outputPath("workspace-loading.png");
        await page.screenshot({ path });
        await testInfo.attach("workspace-loading", {
          path,
          contentType: "image/png",
        });
      }
      await expect(loading.getByRole("status")).toContainText(
        "Loading workspace",
      );
      if (resume.includes("tab return")) await visibility(page, "hidden");
      await page.clock.runFor(14_999);
      const error = page.locator("[data-workspace-activation-error]");
      await expect(error).toHaveCount(0);
      if (resume === "overdue tab return") {
        // Advance wall time without firing timers: a frozen background tab
        // must retire an overdue flight in its visibility handler itself.
        await page.clock.setSystemTime(new Date("2030-01-01T06:00:01Z"));
        await visibility(page, "visible");
      } else {
        await page.clock.runFor(2);
        await expect(error).toContainText("took too long");
        await expect(
          error.getByRole("button", { name: "Retry", exact: true }),
        ).toBeEnabled();
        if (resume === "Retry") {
          const path = testInfo.outputPath("workspace-timeout.png");
          await page.screenshot({ path });
          await testInfo.attach("workspace-timeout", {
            path,
            contentType: "image/png",
          });
        }
      }
      if (resume === "tab return") await visibility(page, "visible");
      else if (resume === "online")
        await page.evaluate(() => window.dispatchEvent(new Event("online")));
      else if (resume === "Retry")
        await error.getByRole("button", { name: "Retry", exact: true }).click();
      await expect(
        page.getByRole("heading", {
          name: "Return test initiative",
          exact: true,
        }),
      ).toBeVisible();
      expect(handshakes).toBe(2);
      expect(documents).toBe(1);
      await expect(error).toHaveCount(0);
      await expect(page.locator("[data-workspace-empty-error]")).toHaveCount(0);
      // A retired response cannot undo the successful retry. Event bursts on
      // an already hydrated workspace must not start extra activations.
      stalled.resolve();
      await page.evaluate(() => {
        for (let index = 0; index < 10; index++) {
          document.dispatchEvent(new Event("visibilitychange"));
          window.dispatchEvent(new Event("online"));
        }
      });
      await page.clock.runFor(100);
      expect(handshakes).toBe(2);
      await expect(
        page.getByRole("heading", {
          name: "Return test initiative",
          exact: true,
        }),
      ).toBeVisible();
    } finally {
      stalled.resolve();
    }
  });
}
