#!/usr/bin/env node

/**
 * Headless QA capture tool for Agent Nexus web-ui.
 *
 * Launches Playwright Chromium against a running dev server, captures
 * full-page screenshots of every major route, runs axe-core accessibility
 * audits, collects console errors, and writes a structured JSON manifest
 * that downstream agents can programmatically consume.
 *
 * Prerequisites:
 *   - `make serve` (or equivalent) running so core + web-ui are live.
 *   - Playwright browsers installed (`pnpm exec playwright install chromium`).
 *
 * Usage:
 *   node scripts/qa-capture.mjs [options]
 *
 * Options:
 *   --base-url <url>      Web-UI base URL              (default: http://127.0.0.1:5173)
 *   --organization <slug> Organization slug            (default: local)
 *   --workspace <slug>    Workspace slug                (default: local)
 *   --persona <id>        Dev persona to auth as        (default: jordan)
 *   --viewport <WxH>      Viewport size                 (default: 1440x900)
 *   --out <dir>           Output directory              (default: .qa-captures)
 *   --routes <list>       Comma-separated route names   (default: all known routes)
 *   --full-page / --no-full-page   Full-page screenshots (default: true)
 *   --wait <ms>           Extra settle time after load  (default: 800)
 *   --compare <dir>       Compare against a previous capture run directory
 *   --json                Print manifest JSON to stdout on completion
 *   --no-axe              Skip accessibility audits
 *   --no-detail           Skip detail page discovery
 *
 * Exit codes:
 *   0  All captures succeeded
 *   1  Fatal error (server unreachable, browser failed, etc.)
 *   2  Some captures had errors (screenshots still produced where possible)
 */

import { chromium } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { runQaVisualCommand } from "./qa-visual.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(__dirname, "..");

// ── CLI arg parsing ─────────────────────────────────────────────────────────

function parseArgs(argv) {
  const args = argv.slice(2);
  const opts = {
    baseUrl: "http://127.0.0.1:5173",
    organization: "local",
    workspace: "local",
    persona: "jordan",
    viewport: "1440x900",
    outDir: path.join(projectRoot, ".qa-captures"),
    routes: null,
    fullPage: true,
    waitMs: 800,
    compareDir: null,
    json: false,
    axe: true,
    discoverDetail: true,
    baseline: false,
  };

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    switch (arg) {
      case "--base-url":
        opts.baseUrl = args[++i];
        break;
      case "--organization":
        opts.organization = args[++i];
        break;
      case "--workspace":
        opts.workspace = args[++i];
        break;
      case "--persona":
        opts.persona = args[++i];
        break;
      case "--viewport":
        opts.viewport = args[++i];
        break;
      case "--out":
        opts.outDir = path.resolve(args[++i]);
        break;
      case "--routes":
        opts.routes = args[++i].split(",").map((r) => r.trim());
        break;
      case "--full-page":
        opts.fullPage = true;
        break;
      case "--no-full-page":
        opts.fullPage = false;
        break;
      case "--wait":
        opts.waitMs = Number(args[++i]);
        break;
      case "--compare":
        opts.compareDir = path.resolve(args[++i]);
        break;
      case "--json":
        opts.json = true;
        break;
      case "--baseline":
        opts.baseline = true;
        break;
      case "--no-axe":
        opts.axe = false;
        break;
      case "--no-detail":
        opts.discoverDetail = false;
        break;
      case "--help":
      case "-h":
        printUsage();
        process.exit(0);
        break;
      default:
        console.error(`Unknown option: ${arg}. Use --help for usage.`);
        process.exit(1);
    }
  }

  const [w, h] = opts.viewport.split("x").map(Number);
  opts.viewportWidth = w || 1440;
  opts.viewportHeight = h || 900;

  return opts;
}

function printUsage() {
  console.log(
    `
QA Capture — Headless screenshot + accessibility tool for Agent Nexus web-ui

Usage: node scripts/qa-capture.mjs [options]

Options:
  --base-url <url>      Web-UI base URL              (default: http://127.0.0.1:5173)
  --organization <slug> Organization slug            (default: local)
  --workspace <slug>    Workspace slug                (default: local)
  --persona <id>        Dev persona to auth as        (default: jordan)
  --viewport <WxH>      Viewport size                 (default: 1440x900)
  --out <dir>           Output directory              (default: .qa-captures)
  --routes <list>       Comma-separated route names   (default: all)
                        Available: home,inbox,topics,threads,boards,docs,artifacts,trash,access
  --full-page           Capture full scrollable page  (default)
  --no-full-page        Capture viewport only
  --wait <ms>           Extra settle time after load  (default: 800)
  --compare <dir>       Compare against a previous run directory
  --baseline            Capture the committed QA baseline into tests/fixtures/qa-visual-baseline/
  --json                Print manifest JSON to stdout on completion
  --no-axe              Skip accessibility audits (faster)
  --no-detail           Skip detail page auto-discovery
  --help                Show this help

Examples:
  node scripts/qa-capture.mjs
  node scripts/qa-capture.mjs --baseline
  node scripts/qa-capture.mjs --routes inbox,topics --viewport 768x1024
  node scripts/qa-capture.mjs --compare .qa-captures/2026-04-09_04-00-54_1440x900
  node scripts/qa-capture.mjs --json --no-axe --no-detail
`.trim(),
  );
}

// ── Known routes ────────────────────────────────────────────────────────────

function workspaceShellPrefix(opts) {
  return `/o/${opts.organization}/w/${opts.workspace}`;
}

function getDefaultRoutes(opts) {
  const shell = workspaceShellPrefix(opts);
  return [
    { name: "home", path: "/", description: "Workspace dashboard / chooser" },
    { name: "inbox", path: `${shell}/inbox`, description: "Inbox triage view" },
    { name: "topics", path: `${shell}/topics`, description: "Topic list" },
    { name: "threads", path: `${shell}/threads`, description: "Thread list" },
    { name: "boards", path: `${shell}/boards`, description: "Board list" },
    { name: "docs", path: `${shell}/docs`, description: "Documents list" },
    {
      name: "artifacts",
      path: `${shell}/artifacts`,
      description: "Artifacts list",
    },
    { name: "trash", path: `${shell}/trash`, description: "Trash" },
    {
      name: "access",
      path: `${shell}/access`,
      description: "Access / principals",
    },
  ];
}

function resolveRoutes(opts) {
  const defaults = getDefaultRoutes(opts);
  if (!opts.routes) return defaults;
  return defaults.filter((r) => opts.routes.includes(r.name));
}

// ── Detail page discovery ───────────────────────────────────────────────────

async function discoverDetailPages(page, opts) {
  const details = [];
  const seenHrefs = new Set();
  const shell = workspaceShellPrefix(opts);

  const collections = [
    {
      listPath: `${shell}/topics`,
      detailPrefix: `${shell}/topics/`,
      name: "topic-detail",
    },
    {
      listPath: `${shell}/boards`,
      detailPrefix: `${shell}/boards/`,
      name: "board-detail",
      linkSelector: "a[href]",
    },
    {
      listPath: `${shell}/docs`,
      detailPrefix: `${shell}/docs/`,
      name: "doc-detail",
      excludePattern: /\/revisions\//,
    },
    {
      listPath: `${shell}/artifacts`,
      detailPrefix: `${shell}/artifacts/`,
      name: "artifact-detail",
    },
  ];

  for (const collection of collections) {
    try {
      await page.goto(`${opts.baseUrl}${collection.listPath}`, {
        waitUntil: "load",
        timeout: 15000,
      });
      await page.waitForTimeout(600);

      const selector = collection.linkSelector || "a[href]";
      const excludeRe = collection.excludePattern;

      const links = await page.$$eval(
        selector,
        (anchors, prefix) =>
          anchors
            .map((a) => a.getAttribute("href"))
            .filter((href) => href && href.startsWith(prefix)),
        collection.detailPrefix,
      );

      const filtered = excludeRe
        ? links.filter((href) => !excludeRe.test(href))
        : links;

      for (const href of filtered.slice(0, 2)) {
        if (seenHrefs.has(href)) continue;
        seenHrefs.add(href);

        const id = href
          .replace(collection.detailPrefix, "")
          .split("/")[0]
          .split("?")[0];
        details.push({
          name: `${collection.name}--${id}`,
          path: href,
          absolute: true,
          description: `${collection.name} for ${id}`,
        });
      }
    } catch {
      // list page may be empty or fail; skip
    }
  }

  return details;
}

// ── Console + error collection ──────────────────────────────────────────────

function createPageCollector(page) {
  const consoleMessages = [];
  const pageErrors = [];
  const networkErrors = [];

  page.on("console", (msg) => {
    const type = msg.type();
    if (type === "error" || type === "warning") {
      consoleMessages.push({
        type,
        text: msg.text(),
        location: msg.location(),
      });
    }
  });

  page.on("pageerror", (error) => {
    pageErrors.push({
      message: error.message,
      stack: error.stack?.split("\n").slice(0, 5).join("\n"),
    });
  });

  page.on("requestfailed", (request) => {
    networkErrors.push({
      url: request.url(),
      method: request.method(),
      failure: request.failure()?.errorText ?? "unknown",
    });
  });

  return {
    flush() {
      const snapshot = {
        consoleErrors: consoleMessages.filter((m) => m.type === "error").length,
        consoleWarnings: consoleMessages.filter((m) => m.type === "warning")
          .length,
        console: [...consoleMessages],
        pageErrors: [...pageErrors],
        networkErrors: [...networkErrors],
      };
      consoleMessages.length = 0;
      pageErrors.length = 0;
      networkErrors.length = 0;
      return snapshot;
    },
  };
}

// ── Accessibility audit ─────────────────────────────────────────────────────

async function runAxeAudit(page) {
  try {
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "best-practice"])
      .analyze();

    return {
      violations: results.violations.map((v) => ({
        id: v.id,
        impact: v.impact,
        description: v.description,
        helpUrl: v.helpUrl,
        nodes: v.nodes.length,
      })),
      violationCount: results.violations.length,
      passes: results.passes.length,
      incomplete: results.incomplete.length,
    };
  } catch (err) {
    return {
      error: err.message,
      violations: [],
      violationCount: 0,
      passes: 0,
      incomplete: 0,
    };
  }
}

// ── Screenshot capture ──────────────────────────────────────────────────────

async function captureScreenshot(page, route, opts, runDir, collector) {
  const shell = workspaceShellPrefix(opts);
  const routePath =
    route.absolute || route.path === "/" || route.path.startsWith("/o/")
      ? route.path
      : `${shell}${route.path.startsWith("/") ? route.path : `/${route.path}`}`;
  const url = `${opts.baseUrl}${routePath}`;
  const filename = `${route.name}.png`;
  const filepath = path.join(runDir, filename);

  collector.flush();
  const loadStart = Date.now();

  const result = {
    route: route.name,
    description: route.description || "",
    path: route.path,
    url,
    filename,
    viewport: `${opts.viewportWidth}x${opts.viewportHeight}`,
    status: "ok",
    error: null,
    timestamp: new Date().toISOString(),
    pageTitle: "",
    loadTimeMs: 0,
    httpStatus: null,
    diagnostics: null,
    accessibility: null,
    fileHash: null,
    fileSizeBytes: null,
  };

  try {
    const response = await page.goto(url, {
      waitUntil: "load",
      timeout: 20000,
    });

    await page.waitForTimeout(opts.waitMs + 600);

    result.loadTimeMs = Date.now() - loadStart;
    result.httpStatus = response?.status() ?? null;
    result.pageTitle = await page.title();

    if (response && !response.ok()) {
      result.status = "http_error";
      result.error = `HTTP ${response.status()}`;
    }

    await page.screenshot({
      path: filepath,
      fullPage: opts.fullPage,
    });

    const buf = await readFile(filepath);
    result.fileHash = createHash("sha256")
      .update(buf)
      .digest("hex")
      .slice(0, 16);
    result.fileSizeBytes = buf.length;

    if (opts.axe) {
      result.accessibility = await runAxeAudit(page);
    }
  } catch (err) {
    result.status = "error";
    result.error = err.message;
    result.loadTimeMs = Date.now() - loadStart;
    try {
      await page.screenshot({ path: filepath, fullPage: false });
    } catch {
      // fallback screenshot failed; leave file absent
    }
  }

  result.diagnostics = collector.flush();
  return result;
}

// ── Comparison ──────────────────────────────────────────────────────────────

async function loadBaselineManifest(compareDir) {
  const manifestPath = path.join(compareDir, "manifest.json");
  try {
    const raw = await readFile(manifestPath, "utf-8");
    return JSON.parse(raw);
  } catch (err) {
    console.error(
      `Cannot read baseline manifest at ${manifestPath}: ${err.message}`,
    );
    return null;
  }
}

function compareCaptures(currentResults, baseline) {
  if (!baseline?.captures) return null;

  const baselineByRoute = new Map();
  for (const cap of baseline.captures) {
    baselineByRoute.set(cap.route, cap);
  }

  const comparison = {
    baselineRun: baseline.run,
    baselineTimestamp: baseline.timestamp,
    routesAdded: [],
    routesRemoved: [],
    routesChanged: [],
    routesUnchanged: [],
  };

  const currentRouteNames = new Set();
  for (const current of currentResults) {
    currentRouteNames.add(current.route);
    const prev = baselineByRoute.get(current.route);
    if (!prev) {
      comparison.routesAdded.push(current.route);
      continue;
    }

    const changed =
      current.fileHash !== prev.fileHash ||
      current.status !== prev.status ||
      current.fileSizeBytes !== prev.fileSizeBytes;

    if (changed) {
      const delta = {
        route: current.route,
        statusChange:
          current.status !== prev.status
            ? { from: prev.status, to: current.status }
            : null,
        sizeChange:
          current.fileSizeBytes !== prev.fileSizeBytes
            ? {
                from: prev.fileSizeBytes,
                to: current.fileSizeBytes,
                deltaBytes:
                  (current.fileSizeBytes ?? 0) - (prev.fileSizeBytes ?? 0),
              }
            : null,
        hashChanged: current.fileHash !== prev.fileHash,
      };
      comparison.routesChanged.push(delta);
    } else {
      comparison.routesUnchanged.push(current.route);
    }
  }

  for (const route of baselineByRoute.keys()) {
    if (!currentRouteNames.has(route)) {
      comparison.routesRemoved.push(route);
    }
  }

  return comparison;
}

// ── Auth ────────────────────────────────────────────────────────────────────

const PERSONA_ACTOR_MAP = {
  jordan: "actor-dev-human-operator",
  zara: "actor-ops-ai",
  squeeze: "actor-squeeze-bot",
  flavor: "actor-flavor-ai",
  supply: "actor-supply-rover",
  cashier: "actor-cashier-bot",
};

async function authenticateDevPersona(page, opts) {
  const actorId = PERSONA_ACTOR_MAP[opts.persona] || opts.persona;

  await page.addInitScript(
    ({ actorId, workspace }) => {
      localStorage.setItem("anx_ui_actor_id", actorId);
      localStorage.setItem(`anx_ui_actor_id:${workspace}`, actorId);
    },
    { actorId, workspace: opts.workspace },
  );

  const inboxUrl = `${opts.baseUrl}${workspaceShellPrefix(opts)}/inbox`;
  await page.goto(inboxUrl, {
    waitUntil: "load",
    timeout: 20000,
  });
  await page.waitForTimeout(1200);

  const identitiesRes = await page.request.get(
    `${opts.baseUrl}/auth/dev/identities`,
    {
      headers: {
        "x-anx-workspace-slug": opts.workspace,
        "x-anx-organization-slug": opts.organization,
      },
    },
  );

  let personaAuthOk = false;
  if (identitiesRes.ok()) {
    const body = await identitiesRes.json().catch(() => ({}));
    const personas = Array.isArray(body.personas) ? body.personas : [];
    const hasPersona = personas.some((p) => p.persona_id === opts.persona);

    if (hasPersona) {
      const response = await page.request.post(
        `${opts.baseUrl}/auth/dev/session`,
        {
          headers: {
            "content-type": "application/json",
            "x-anx-workspace-slug": opts.workspace,
            "x-anx-organization-slug": opts.organization,
          },
          data: { persona_id: opts.persona },
        },
      );
      personaAuthOk = response.ok();
    }
  }

  if (personaAuthOk) {
    await page.goto(inboxUrl, {
      waitUntil: "load",
      timeout: 20000,
    });
    await page.waitForTimeout(800);
  }

  return { actorId, personaAuthOk };
}

// ── Server health check ─────────────────────────────────────────────────────

async function waitForServer(baseUrl, timeoutMs = 10000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(baseUrl, { signal: AbortSignal.timeout(2000) });
      if (res.ok || res.status < 500) return true;
    } catch {
      // retry
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  return false;
}

// ── Summary formatting ──────────────────────────────────────────────────────

function formatSummary(manifest) {
  const lines = [];
  lines.push(`\n${"─".repeat(60)}`);
  lines.push(`QA CAPTURE SUMMARY: ${manifest.run}`);
  lines.push(`${"─".repeat(60)}`);

  const s = manifest.summary;
  lines.push(`Routes captured: ${s.ok}/${s.total} ok`);
  if (s.errors > 0) lines.push(`Errors: ${s.errors}`);
  if (s.totalConsoleErrors > 0)
    lines.push(
      `Console errors:  ${s.totalConsoleErrors} across ${s.routesWithConsoleErrors} routes`,
    );
  if (s.totalA11yViolations > 0)
    lines.push(
      `A11y violations: ${s.totalA11yViolations} across ${s.routesWithA11yViolations} routes`,
    );

  if (s.errors > 0) {
    lines.push(`\nFailed routes:`);
    for (const cap of manifest.captures) {
      if (cap.status !== "ok") {
        lines.push(`  ✗ ${cap.route}: ${cap.error}`);
      }
    }
  }

  const consoleErrorRoutes = manifest.captures.filter(
    (c) => c.diagnostics?.consoleErrors > 0,
  );
  if (consoleErrorRoutes.length > 0) {
    lines.push(`\nRoutes with console errors:`);
    for (const cap of consoleErrorRoutes) {
      lines.push(`  ! ${cap.route}: ${cap.diagnostics.consoleErrors} errors`);
      for (const msg of cap.diagnostics.console
        .filter((m) => m.type === "error")
        .slice(0, 3)) {
        lines.push(`      ${msg.text.slice(0, 120)}`);
      }
    }
  }

  const a11yRoutes = manifest.captures.filter(
    (c) => c.accessibility?.violationCount > 0,
  );
  if (a11yRoutes.length > 0) {
    lines.push(`\nAccessibility violations:`);
    for (const cap of a11yRoutes) {
      lines.push(
        `  ${cap.route}: ${cap.accessibility.violationCount} violations`,
      );
      for (const v of cap.accessibility.violations.slice(0, 5)) {
        lines.push(
          `    [${v.impact}] ${v.id}: ${v.description} (${v.nodes} nodes)`,
        );
      }
    }
  }

  if (manifest.comparison) {
    const cmp = manifest.comparison;
    lines.push(`\nComparison vs ${cmp.baselineRun}:`);
    if (
      cmp.routesChanged.length === 0 &&
      cmp.routesAdded.length === 0 &&
      cmp.routesRemoved.length === 0
    ) {
      lines.push(`  No changes detected.`);
    } else {
      if (cmp.routesChanged.length > 0) {
        lines.push(`  Changed (${cmp.routesChanged.length}):`);
        for (const c of cmp.routesChanged) {
          const parts = [];
          if (c.statusChange)
            parts.push(`status: ${c.statusChange.from} → ${c.statusChange.to}`);
          if (c.sizeChange) {
            const sign = c.sizeChange.deltaBytes >= 0 ? "+" : "";
            parts.push(`size: ${sign}${c.sizeChange.deltaBytes}B`);
          }
          if (c.hashChanged && !c.sizeChange) parts.push("pixels changed");
          lines.push(`    ~ ${c.route}: ${parts.join(", ")}`);
        }
      }
      if (cmp.routesAdded.length > 0) {
        lines.push(`  Added: ${cmp.routesAdded.join(", ")}`);
      }
      if (cmp.routesRemoved.length > 0) {
        lines.push(`  Removed: ${cmp.routesRemoved.join(", ")}`);
      }
      lines.push(`  Unchanged: ${cmp.routesUnchanged.length} routes`);
    }
  }

  lines.push(`\nManifest: ${manifest.manifestPath}`);
  lines.push(`Output:   ${manifest.outputDir}`);
  lines.push("");

  return lines.join("\n");
}

// ── Main ────────────────────────────────────────────────────────────────────

async function main() {
  const opts = parseArgs(process.argv);

  if (opts.baseline) {
    const exitCode = await runQaVisualCommand({
      mode: "baseline",
      outDir: path.join(projectRoot, "tests", "fixtures", "qa-visual-baseline"),
      port: Number(process.env.QA_VISUAL_PORT ?? 4273),
      thresholdRatio: 0.001,
      json: opts.json,
    });
    process.exit(exitCode);
  }

  const timestamp = new Date()
    .toISOString()
    .replace(/[:.]/g, "-")
    .replace("T", "_")
    .slice(0, 19);
  const runLabel = `${timestamp}_${opts.viewportWidth}x${opts.viewportHeight}`;
  const runDir = path.join(opts.outDir, runLabel);
  await mkdir(runDir, { recursive: true });

  if (!opts.json) {
    console.log(`QA Capture: ${runLabel}`);
    console.log(`  base-url : ${opts.baseUrl}`);
    console.log(`  workspace: ${opts.workspace}`);
    console.log(`  persona  : ${opts.persona}`);
    console.log(`  viewport : ${opts.viewportWidth}x${opts.viewportHeight}`);
    console.log(`  axe      : ${opts.axe ? "on" : "off"}`);
    console.log(`  output   : ${runDir}`);
    if (opts.compareDir) console.log(`  compare  : ${opts.compareDir}`);
    console.log();
  }

  if (!opts.json) process.stdout.write("Checking server... ");
  const serverOk = await waitForServer(opts.baseUrl, 8000);
  if (!serverOk) {
    console.error(
      `\nServer not reachable at ${opts.baseUrl}. Is \`make serve\` running?`,
    );
    process.exit(1);
  }
  if (!opts.json) console.log("ok");

  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: {
      width: opts.viewportWidth,
      height: opts.viewportHeight,
    },
    colorScheme: "dark",
    ignoreHTTPSErrors: true,
  });
  const page = await context.newPage();
  const collector = createPageCollector(page);

  let exitCode = 0;

  try {
    if (!opts.json) process.stdout.write("Authenticating... ");
    const authResult = await authenticateDevPersona(page, opts);
    if (!opts.json) {
      console.log(
        authResult.personaAuthOk
          ? `ok (${authResult.actorId})`
          : `dev_actor_mode (${authResult.actorId})`,
      );
    }

    const listRoutes = resolveRoutes(opts);

    let detailRoutes = [];
    if (opts.discoverDetail) {
      if (!opts.json) process.stdout.write("Discovering detail pages... ");
      detailRoutes = await discoverDetailPages(page, opts);
      if (!opts.json) console.log(`${detailRoutes.length} found`);
    }

    const allRoutes = [...listRoutes, ...detailRoutes];

    if (!opts.json) console.log(`\nCapturing ${allRoutes.length} routes...\n`);

    const results = [];
    for (const route of allRoutes) {
      if (!opts.json) process.stdout.write(`  ${route.name.padEnd(40)} `);
      const result = await captureScreenshot(
        page,
        route,
        opts,
        runDir,
        collector,
      );
      results.push(result);

      if (!opts.json) {
        const parts = [];
        if (result.status === "ok") {
          parts.push("ok");
          parts.push(`${result.loadTimeMs}ms`);
          if (result.accessibility?.violationCount > 0) {
            parts.push(`a11y:${result.accessibility.violationCount}`);
          }
          if (result.diagnostics?.consoleErrors > 0) {
            parts.push(`errors:${result.diagnostics.consoleErrors}`);
          }
        } else {
          parts.push(`${result.status}: ${result.error}`);
        }
        console.log(parts.join("  "));
      }
    }

    let comparison = null;
    if (opts.compareDir) {
      const baseline = await loadBaselineManifest(opts.compareDir);
      if (baseline) {
        comparison = compareCaptures(results, baseline);
      }
    }

    const totalConsoleErrors = results.reduce(
      (sum, r) => sum + (r.diagnostics?.consoleErrors ?? 0),
      0,
    );
    const totalA11yViolations = results.reduce(
      (sum, r) => sum + (r.accessibility?.violationCount ?? 0),
      0,
    );

    const manifest = {
      run: runLabel,
      timestamp: new Date().toISOString(),
      options: {
        baseUrl: opts.baseUrl,
        workspace: opts.workspace,
        persona: opts.persona,
        viewport: `${opts.viewportWidth}x${opts.viewportHeight}`,
        fullPage: opts.fullPage,
        axe: opts.axe,
      },
      captures: results,
      summary: {
        total: results.length,
        ok: results.filter((r) => r.status === "ok").length,
        errors: results.filter((r) => r.status !== "ok").length,
        totalConsoleErrors,
        routesWithConsoleErrors: results.filter(
          (r) => (r.diagnostics?.consoleErrors ?? 0) > 0,
        ).length,
        totalA11yViolations,
        routesWithA11yViolations: results.filter(
          (r) => (r.accessibility?.violationCount ?? 0) > 0,
        ).length,
        avgLoadTimeMs: Math.round(
          results.reduce((sum, r) => sum + (r.loadTimeMs ?? 0), 0) /
            (results.length || 1),
        ),
      },
      comparison,
      manifestPath: path.join(runDir, "manifest.json"),
      outputDir: runDir,
    };

    await writeFile(manifest.manifestPath, JSON.stringify(manifest, null, 2));

    // Write a lightweight "latest" symlink-equivalent for agents to find easily
    const latestPath = path.join(opts.outDir, "latest.json");
    await writeFile(
      latestPath,
      JSON.stringify(
        { run: runLabel, dir: runDir, manifest: manifest.manifestPath },
        null,
        2,
      ),
    );

    if (opts.json) {
      console.log(JSON.stringify(manifest, null, 2));
    } else {
      console.log(formatSummary(manifest));
    }

    if (manifest.summary.errors > 0) exitCode = 2;
  } finally {
    await context.close();
    await browser.close();
  }

  process.exit(exitCode);
}

main().catch((err) => {
  console.error("qa-capture failed:", err.message);
  process.exit(1);
});
