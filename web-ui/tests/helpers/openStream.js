/**
 * Leave a mocked SSE request unanswered until the browser drops it.
 *
 * Fulfilling a one-line keepalive completes the response. The client treats
 * that as a dropped stream and reconnects, which under load exhausts the
 * per-host connection pool and races the next list re-read. The handler
 * returns only after the request fails or the page closes, then fulfills so
 * teardown is not left with an unhandled route.
 *
 * @param {import("@playwright/test").Page} page
 * @param {import("@playwright/test").Route} route
 */
export async function holdOpenStream(page, route) {
  const request = route.request();
  await new Promise((resolve) => {
    if (page.isClosed()) {
      resolve();
      return;
    }
    const finish = () => {
      page.off("close", finish);
      page.off("requestfailed", onFailed);
      resolve();
    };
    const onFailed = (failed) => {
      if (failed === request) finish();
    };
    page.on("close", finish);
    page.on("requestfailed", onFailed);
  });
  await route
    .fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: ": keepalive\n\n",
    })
    .catch(() => {});
}
