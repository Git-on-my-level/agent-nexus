#!/usr/bin/env node
import { spawn } from "node:child_process";
import { createServer } from "node:net";
import { mkdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

function args(argv) {
  const result = {};
  for (let index = 0; index < argv.length; index += 1) {
    const key = argv[index];
    if (!key.startsWith("--") || !argv[index + 1])
      throw new Error("usage: preview-visual-report.mjs --report <file> --observations <file> --output <png>");
    result[key.slice(2)] = argv[++index];
  }
  for (const key of ["report", "observations", "output"])
    if (!result[key]) throw new Error(`missing --${key}`);
  return result;
}

async function freePort() {
  const server = createServer();
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const port = server.address().port;
  await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
  return port;
}

async function waitUntilReady(child, url) {
  const deadline = Date.now() + 45_000;
  let lastError;
  while (Date.now() < deadline) {
    if (child.exitCode !== null)
      throw new Error(`web renderer exited early with status ${child.exitCode}`);
    try {
      const response = await fetch(url);
      if (response.ok) return;
      lastError = new Error(`web renderer returned ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`web renderer did not become ready: ${lastError?.message ?? "timeout"}`);
}

async function stop(child) {
  if (child.exitCode !== null) return;
  child.kill("SIGTERM");
  await Promise.race([
    new Promise((resolve) => child.once("exit", resolve)),
    new Promise((resolve) => setTimeout(resolve, 5_000)),
  ]);
  if (child.exitCode === null) child.kill("SIGKILL");
}

async function main() {
  const input = args(process.argv.slice(2));
  JSON.parse(await readFile(input.report, "utf8"));
  const observations = JSON.parse(await readFile(input.observations, "utf8"));
  if (!Array.isArray(observations)) throw new Error("observations must be an array");
  const output = path.resolve(input.output);
  await mkdir(path.dirname(output), { recursive: true });
  const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const port = await freePort();
  const server = spawn(
    process.execPath,
    [path.join(webRoot, "node_modules", "vite", "bin", "vite.js"), "dev", "--host", "127.0.0.1", "--port", String(port), "--strictPort"],
    {
      cwd: webRoot,
      env: {
        ...process.env,
        ANX_REPORT_PREVIEW_REPORT: path.resolve(input.report),
        ANX_REPORT_PREVIEW_OBSERVATIONS: path.resolve(input.observations),
      },
      stdio: "ignore",
    },
  );
  try {
    const url = `http://127.0.0.1:${port}/internal/report-preview`;
    await waitUntilReady(server, url);
    const browser = await chromium.launch({
      headless: true,
      chromiumSandbox: true,
    });
    try {
      const page = await browser.newPage({ viewport: { width: 1440, height: 1000 }, colorScheme: "light" });
      await page.goto(url, { waitUntil: "networkidle", timeout: 30_000 });
      const reportSurface = page.locator('[aria-label="Visual report"]');
      await reportSurface.waitFor({ state: "visible", timeout: 15_000 });
      await reportSurface.screenshot({ path: output, animations: "disabled" });
    } finally {
      await browser.close();
    }
  } finally {
    await stop(server);
  }
}

main().catch((error) => {
  process.stderr.write(`${error?.message ?? "report preview failed"}\n`);
  process.exitCode = 1;
});
