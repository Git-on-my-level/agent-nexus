import { createServer } from "node:http";
import { test } from "@playwright/test";
import { warmWorkspacePages } from "../../playwright.identity.js";

for (const basePath of ["", "/anx"]) {
  test(`server preparation waits for client hydration under ${basePath || "/"}`, async ({
    browser,
  }) => {
    let release;
    const compile = new Promise((resolve) => {
      release = resolve;
    });
    let requested;
    const clientRequested = new Promise((resolve) => {
      requested = resolve;
    });
    const server = createServer(async (req, res) => {
      if (req.url === `${basePath}/cold-shell.js`) {
        requested();
        await compile;
        res.setHeader("content-type", "text/javascript");
        res.end('document.body.innerHTML = "<h1>Agents</h1>";');
        return;
      }
      res.setHeader("content-type", "text/html");
      res.end(
        `<p>Loading workspace…</p><script type="module" src="${basePath}/cold-shell.js"></script>`,
      );
    });
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    const ready = warmWorkspacePages(
      [{ port: server.address().port, basePath }],
      {
        launchBrowser: async () => ({
          newPage: () => browser.newPage(),
          close: async () => {},
        }),
      },
    );
    try {
      // No timing guess: hold the client compile until it has been requested.
      // A TCP/HTTP-only readiness check resolves early and fails this race.
      await Promise.race([
        clientRequested,
        ready.then(() => {
          throw new Error("Startup completed before the client hydrated");
        }),
      ]);
      release();
      await ready;
    } finally {
      release();
      await ready.catch(() => {});
      server.closeAllConnections();
      await new Promise((resolve) => server.close(resolve));
    }
  });
}
