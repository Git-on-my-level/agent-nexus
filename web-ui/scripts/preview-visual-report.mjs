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
      throw new Error(
        "usage: preview-visual-report.mjs --report <file> --observations <file> --output <png> [--expect-text <text>] [--chromium-sandbox <true|false>]",
      );
    const name = key.slice(2);
    const value = argv[++index];
    if (name === "expect-text") result[name] = [...(result[name] ?? []), value];
    else result[name] = value;
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
  await new Promise((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
  return port;
}

async function waitUntilReady(child, url) {
  const deadline = Date.now() + 45_000;
  let lastError;
  while (Date.now() < deadline) {
    if (child.exitCode !== null)
      throw new Error(
        `web renderer exited early with status ${child.exitCode}`,
      );
    try {
      const response = await fetch(url);
      if (response.ok) return;
      lastError = new Error(`web renderer returned ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(
    `web renderer did not become ready: ${lastError?.message ?? "timeout"}`,
  );
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

export function isSandboxLaunchFailure(error) {
  return /chromium sandboxing failed/i.test(
    error instanceof Error ? error.message : String(error),
  );
}

export async function main(
  argv = process.argv.slice(2),
  { launchBrowser = (options) => chromium.launch(options) } = {},
) {
  const input = args(argv);
  const sandbox = input["chromium-sandbox"] ?? "true";
  if (sandbox !== "true" && sandbox !== "false")
    throw new Error("--chromium-sandbox must be true or false");
  JSON.parse(await readFile(input.report, "utf8"));
  const observations = JSON.parse(await readFile(input.observations, "utf8"));
  if (!Array.isArray(observations))
    throw new Error("observations must be an array");
  const expectedTexts = (input["expect-text"] ?? [])
    .map((value) => value.trim())
    .filter(Boolean);
  const output = path.resolve(input.output);
  await mkdir(path.dirname(output), { recursive: true });
  const webRoot = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "..",
  );
  let browser;
  try {
    browser = await launchBrowser({
      headless: true,
      // Use full Chromium's new headless mode. The default headless shell
      // can segfault during sandboxed startup on Linux runners.
      channel: "chromium",
      // Keep sandboxing by default. Trusted test containers running as root
      // may explicitly opt out through the command line.
      chromiumSandbox: sandbox === "true",
    });
  } catch (error) {
    if (isSandboxLaunchFailure(error)) {
      process.stderr.write(`${error?.message ?? String(error)}\n`);
      return { rendered: false, reason: "sandbox_unavailable" };
    }
    throw error;
  }

  let server;
  try {
    const port = await freePort();
    server = spawn(
      process.execPath,
      [
        path.join(webRoot, "node_modules", "vite", "bin", "vite.js"),
        "dev",
        "--host",
        "127.0.0.1",
        "--port",
        String(port),
        "--strictPort",
      ],
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
    const url = `http://127.0.0.1:${port}/internal/report-preview`;
    await waitUntilReady(server, url);
    const page = await browser.newPage({
      viewport: { width: 1440, height: 1000 },
      colorScheme: "light",
    });
    const pageErrors = [];
    page.on("pageerror", (error) => pageErrors.push(error.message));
    await page.goto(url, { waitUntil: "networkidle", timeout: 30_000 });
    if (pageErrors.length)
      throw new Error(`report preview page error: ${pageErrors.join("; ")}`);
    const reportSurface = page.locator('[aria-label="Visual report"]');
    await reportSurface.waitFor({ state: "visible", timeout: 15_000 });
    await page.waitForFunction(
      () => {
        const surface = document.querySelector('[aria-label="Visual report"]');
        return surface && !surface.innerText.includes("Reading workspace…");
      },
      null,
      { timeout: 15_000 },
    );
    const renderedText = await reportSurface.innerText();
    if (pageErrors.length)
      throw new Error(`report preview page error: ${pageErrors.join("; ")}`);
    if (renderedText.includes("Reading workspace…"))
      throw new Error("report preview retained a loading placeholder");
    for (const expectedText of expectedTexts) {
      if (!renderedText.includes(expectedText))
        throw new Error(
          `report preview did not render fixture content: ${expectedText}`,
        );
    }
    await reportSurface.screenshot({ path: output, animations: "disabled" });
    return { rendered: true };
  } finally {
    if (server) await stop(server);
    await browser.close();
  }
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : "";
export async function run(argv = process.argv.slice(2), dependencies = {}) {
  try {
    const result = await main(argv, dependencies);
    if (result.reason) process.stdout.write(`${JSON.stringify(result)}\n`);
    return 0;
  } catch (error) {
    process.stderr.write(`${error?.message ?? "report preview failed"}\n`);
    return 1;
  }
}

if (invokedPath === fileURLToPath(import.meta.url)) {
  run().then((exitCode) => {
    process.exitCode = exitCode;
  });
}
