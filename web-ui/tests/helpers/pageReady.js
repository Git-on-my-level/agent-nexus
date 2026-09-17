/**
 * Waits until the SvelteKit client has hydrated the page.
 *
 * Use this instead of `page.waitForLoadState("networkidle")`: workspace pages
 * hold a live event stream open, so the network never goes idle and that wait
 * times out (or hangs teardown) no matter how long it is given.
 *
 * @param {import("@playwright/test").Page} page
 */
export async function waitForAppReady(page) {
  await page.waitForLoadState("load");
  // Svelte's client runtime registers itself on window when it boots.
  await page.waitForFunction(() => "__svelte" in window, null, {
    timeout: 20_000,
  });
  // One frame for the effects scheduled by hydration to run.
  await page.evaluate(
    () => new Promise((resolve) => requestAnimationFrame(() => resolve())),
  );
}
