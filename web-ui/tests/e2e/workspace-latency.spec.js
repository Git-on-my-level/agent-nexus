import { expect, test } from "@playwright/test";

// Opt-in real-core measurement. Use an isolated workspace and production UI:
// ANX_MEASURE_LATENCY=1 PLAYWRIGHT_PREVIEW=1 pnpm exec playwright test
// workspace-latency.spec.js --project=default --workers=1
test("real small workspace request waterfall and interactive paint", async ({
  page,
  request,
}, testInfo) => {
  test.skip(
    process.env.ANX_MEASURE_LATENCY !== "1",
    "opt-in measurement fixture",
  );
  test.setTimeout(180000);
  const core =
    process.env.PLAYWRIGHT_CORE_BASE_URL ||
    `http://127.0.0.1:${process.env.PLAYWRIGHT_CORE_PORT || 8000}`;
  const post = async (path, data) => {
    const response = await request.post(`${core}${path}`, { data });
    expect(response.ok(), `${path}: ${await response.text()}`).toBe(true);
    return response.json();
  };
  const human = await post("/auth/passkey/dev/register", {
    bootstrap_token:
      process.env.ANX_BOOTSTRAP_TOKEN || "playwright-local-bootstrap-token",
    display_name: "Latency fixture reader",
  });
  const actor_id = human.agent.actor_id;
  const { board } = await post("/boards", {
    actor_id,
    board: { title: "Latency portfolio", role: "initiatives" },
  });
  for (let i = 0; i < 10; i++) {
    await post("/work", {
      actor_id,
      board_ref: `board:${board.id}`,
      title: `Latency initiative ${i}`,
      summary: "A useful outcome\n- [x] Design\n- [ ] Verify",
      next_actor: "human",
      next_action: "Approve verification",
    });
  }
  await page.context().addCookies([
    {
      name: "anx_ui_session_local__local",
      value: human.tokens.refresh_token,
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
      sameSite: "Lax",
    },
  ]);
  await page.addInitScript(() => {
    localStorage.setItem("workspaceTourSeen.local", "1");
    localStorage.setItem("workspaceTourSeen.local:local", "1");
    // Record readiness in the browser, independent of Playwright's polling
    // backoff. A rendered row is the point at which the view can be used.
    const observer = new MutationObserver(() => {
      const overview =
        location.pathname.endsWith("/overview") &&
        [...document.querySelectorAll("summary")].some(
          (node) => node.textContent.trim() === "No plan (10)",
        );
      const inbox =
        location.pathname.endsWith("/inbox") &&
        document.querySelector("[data-inbox-row]") &&
        !document.body.textContent.includes("Loading inbox…");
      if (overview || inbox) {
        performance.mark("anx.interactive");
        observer.disconnect();
      }
    });
    observer.observe(document, {
      childList: true,
      subtree: true,
      characterData: true,
    });
  });
  const measurements = [];
  // Keep process/module startup separate from browser navigation latency.
  // The server is warm, while the first browser still has an empty HTTP cache.
  const warm = await request.get("/o/local/w/local/overview");
  expect(warm.ok()).toBe(true);
  for (const route of ["overview", "inbox"]) {
    for (let sample = 0; sample < 3; sample++) {
      await page.goto(`/o/local/w/local/${route}`, {
        waitUntil: "domcontentloaded",
      });
      if (route === "overview") {
        await expect(
          page.getByText("No plan (10)", { exact: true }),
        ).toBeVisible();
      } else {
        await expect(page.locator("[data-inbox-row]").first()).toBeVisible();
        await expect(
          page.getByText("Loading inbox…", { exact: true }),
        ).toHaveCount(0);
      }
      const metric = await page.evaluate(() => ({
        interactive:
          performance.getEntriesByName("anx.interactive")[0]?.startTime,
        paints: performance
          .getEntriesByType("paint")
          .map(({ name, startTime }) => ({ name, startTime })),
        resources: performance
          .getEntriesByType("resource")
          .filter((entry) =>
            ["fetch", "xmlhttprequest"].includes(entry.initiatorType),
          )
          .map(
            ({
              name,
              startTime,
              responseStart,
              duration,
              transferSize,
              serverTiming,
            }) => ({
              path: new URL(name).pathname,
              startTime,
              responseStart,
              duration,
              transferSize,
              serverTiming: serverTiming.map(({ name, duration }) => ({
                name,
                duration,
              })),
            }),
          ),
      }));
      measurements.push({ route, sample, ...metric });
      console.log(JSON.stringify({ route, sample, ...metric }));
    }
  }
  await testInfo.attach("workspace-latency.json", {
    body: JSON.stringify(measurements, null, 2),
    contentType: "application/json",
  });
});
